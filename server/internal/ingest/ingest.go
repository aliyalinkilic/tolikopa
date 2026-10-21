// Package ingest implements POST /api/v1/ingest: the agent -> server batch
// pipeline. It validates the envelope, auto-registers the machine, computes NIC
// deltas (reset-corrected via boot_time), resolves process names to categories,
// and bulk-writes to the hypertables with idempotent ON CONFLICT DO NOTHING.
package ingest

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/config"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// Validation caps (see docs/API.md §1).
const (
	maxDecompressed   = 1 << 20 // 1 MiB decompressed
	maxSamples        = 240
	maxDisksPerSample = 20
	maxProcessRows    = 5000
	maxAppFocusRows   = 200
	maxEvents         = 512
	maxDiskHealthRows = 64
	maxTextLen        = 255 // hostnames, user names, mounts, OS strings …
	maxAppNameLen     = 128
	acquireTimeout    = 2 * time.Second

	// Endpoint hygiene. The catalog is 57 items today; 512 leaves generous room
	// for growth while keeping one machine's report bounded. maxItemIDLen matches
	// the catalog's own id rule ([a-z0-9][a-z0-9._-]{0,63}); maxErrorCodeLen is
	// sized for an HRESULT string ("0x80073cfa") so no message text can fit.
	maxRemediationItems    = 512
	maxRemediationAttempts = 512
	maxItemIDLen           = 64
	maxErrorCodeLen        = 32
	maxVersionLen          = 64
)

var errTooLarge = errors.New("payload too large")

// validSessionState reports whether s is one of the known session states
// (empty allowed — old agents omit it). Unknown text is dropped at ingest so
// arbitrary strings never reach metric_sample/machine_live.
func validSessionState(s string) bool {
	switch s {
	case "", "active", "idle", "locked", "disconnected", "none":
		return true
	}
	return false
}

// Ingester holds the dependencies for the ingest handler.
type Ingester struct {
	pool      *pgxpool.Pool
	cats      *Categories
	pols      *Policies
	tokenHash [32]byte
}

// NewIngester builds the handler and precomputes the ingest-token digest so the
// bearer token is compared in constant time against a hash (never stored plain).
func NewIngester(pool *pgxpool.Pool, cats *Categories, pols *Policies, cfg *config.Config) *Ingester {
	return &Ingester{
		pool:      pool,
		cats:      cats,
		pols:      pols,
		tokenHash: sha256.Sum256([]byte(cfg.IngestToken)),
	}
}

// RequireIngest is Echo middleware enforcing the shared bearer ingest token.
func (ig *Ingester) RequireIngest(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		const prefix = "Bearer "
		h := c.Request().Header.Get("Authorization")
		if !strings.HasPrefix(h, prefix) {
			return apierr.New(http.StatusUnauthorized, "ingest_token_required", "bearer ingest token required")
		}
		got := sha256.Sum256([]byte(strings.TrimPrefix(h, prefix)))
		if subtle.ConstantTimeCompare(got[:], ig.tokenHash[:]) != 1 {
			return apierr.New(http.StatusUnauthorized, "ingest_token_invalid", "invalid ingest token")
		}
		return next(c)
	}
}

// tooLarge is a 413 payload_too_large.
// clip cuts s to at most maxTextLen runes. Agents never send longer values; a
// longer one is either a bug or someone filling the tables, and neither should
// reach the database at full length.
func clip(s string) string {
	if len(s) <= maxTextLen {
		return s
	}
	r := []rune(s)
	if len(r) <= maxTextLen {
		return s
	}
	return string(r[:maxTextLen])
}

// baseName reduces a process name to its base name by dropping everything up to
// the last '/' or '\'. The agent already sends only base names (docs/AGENT.md);
// this is defence in depth so a faulting-module path can never be stored.
func baseName(s string) string {
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		return s[i+1:]
	}
	return s
}

// clipStrings bounds every free-text field of a batch.
func clipStrings(env *model.Envelope) {
	m := &env.Machine
	for _, p := range []*string{&m.Hostname, &m.OS, &m.OSBuild, &m.Domain, &m.Kind, &m.CPUModel} {
		*p = clip(*p)
	}
	for i := range env.Samples {
		s := &env.Samples[i]
		s.LogonUser, s.SessionState = clip(s.LogonUser), clip(s.SessionState)
		for j := range s.Disks {
			s.Disks[j].Mount = clip(s.Disks[j].Mount)
		}
		for j := range s.Processes {
			s.Processes[j].Name = clip(s.Processes[j].Name)
		}
	}
	for i := range env.Events {
		e := &env.Events[i]
		e.Kind, e.ProcessName, e.Severity = clip(e.Kind), clip(baseName(e.ProcessName)), clip(e.Severity)
		for k, v := range e.Detail {
			if len(k) > 64 {
				delete(e.Detail, k)
				continue
			}
			e.Detail[k] = clip(v)
		}
	}
	for i := range env.DiskHealth {
		env.DiskHealth[i].Disk = clip(env.DiskHealth[i].Disk)
	}
	for i := range env.AppFocus {
		env.AppFocus[i].Name = clip(env.AppFocus[i].Name)
	}
}

func tooLarge(message string) error {
	return apierr.New(http.StatusRequestEntityTooLarge, apierr.PayloadTooLarge, message)
}

// Handle processes one machine's batch.
func (ig *Ingester) Handle(c echo.Context) error {
	ctx := c.Request().Context()

	// Back-pressure: if the pool is saturated, acquiring a connection blocks and
	// then times out -> answer 429 with Retry-After instead of piling up.
	acqCtx, cancel := context.WithTimeout(ctx, acquireTimeout)
	conn, err := ig.pool.Acquire(acqCtx)
	cancel()
	if err != nil {
		c.Response().Header().Set("Retry-After", "5")
		return apierr.New(http.StatusTooManyRequests, apierr.ServerBusy, "server busy; retry later")
	}
	defer conn.Release()

	body, err := readBody(c.Request(), maxDecompressed)
	if errors.Is(err, errTooLarge) {
		return tooLarge("body too large")
	}
	if err != nil {
		return apierr.BadRequest("unreadable request body")
	}

	var env model.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return apierr.BadRequest("invalid JSON")
	}
	if strings.TrimSpace(env.Machine.MachineID) == "" {
		return apierr.BadRequest("machine_id is required")
	}

	// Hard caps -> 413.
	if len(env.Samples) > maxSamples {
		return tooLarge("too many samples")
	}
	totalProc := 0
	for _, s := range env.Samples {
		if len(s.Disks) > maxDisksPerSample {
			return tooLarge("too many disks in a sample")
		}
		totalProc += len(s.Processes)
	}
	if totalProc > maxProcessRows {
		return tooLarge("too many process rows")
	}
	if len(env.Events) > maxEvents {
		return tooLarge("too many events")
	}
	if len(env.DiskHealth) > maxDiskHealthRows {
		return tooLarge("too many disk_health rows")
	}
	// app_focus is fed by a helper that runs in the signed-in user's session, so a
	// user can inflate it. Failing the batch would let them blank their PC's
	// telemetry; keeping the busiest rows only costs them their own noise.
	if len(env.AppFocus) > maxAppFocusRows {
		sort.Slice(env.AppFocus, func(i, j int) bool { return env.AppFocus[i].Seconds > env.AppFocus[j].Seconds })
		env.AppFocus = env.AppFocus[:maxAppFocusRows]
	}
	clipStrings(&env)
	if r := env.Remediation; r != nil {
		if len(r.Present) > maxRemediationItems {
			return tooLarge("too many remediation items")
		}
		if len(r.Attempts) > maxRemediationAttempts {
			return tooLarge("too many remediation attempts")
		}
	}

	// Per-sample validation (ts window + cpu clamp).
	now := time.Now()
	maxFuture := now.Add(5 * time.Minute)
	minPast := now.Add(-7 * 24 * time.Hour)

	result := model.IngestResponse{}
	accepted := make([]model.Sample, 0, len(env.Samples))
	for _, s := range env.Samples {
		if s.TS.After(maxFuture) {
			result.Rejected = append(result.Rejected, model.RejectedSample{TS: s.TS, Reason: "ts_in_future"})
			continue
		}
		if s.TS.Before(minPast) {
			result.Rejected = append(result.Rejected, model.RejectedSample{TS: s.TS, Reason: "ts_too_old"})
			continue
		}
		if s.CPUPct < 0 {
			s.CPUPct = 0
		} else if s.CPUPct > 100 {
			s.CPUPct = 100
		}
		if !validSessionState(s.SessionState) {
			s.SessionState = ""
		}
		accepted = append(accepted, s)
	}
	sort.Slice(accepted, func(i, j int) bool { return accepted[i].TS.Before(accepted[j].TS) })

	tags, err := ig.persist(ctx, conn, &env, accepted, c.RealIP())
	if err != nil {
		// Log the real cause; the client only ever sees the generic error
		// (raw driver/SQL text would leak table and constraint names).
		log.Printf("ingest: persist %s: %v", env.Machine.MachineID, err)
		return apierr.New(http.StatusInternalServerError, apierr.ServerError, "internal server error")
	}

	result.Accepted = len(accepted)
	// The revision handshake. The agent reports the policy revision it has actually
	// applied; when the resolved revision already matches we omit `policy` entirely
	// so the steady-state 202 body stays a few dozen bytes. An agent that sends no
	// remediation block at all (older build) reports an empty revision, which never
	// matches, so it simply receives the policy and ignores it — no lockstep deploy.
	if ig.pols != nil {
		if pol := ig.pols.Resolve(tags); pol.Revision != reportedRevision(&env) {
			result.Policy = &pol
		}
	}
	return c.JSON(http.StatusAccepted, result)
}

// reportedRevision is the revision the agent claims to have applied ("" when the
// agent predates the feature or has never applied one).
func reportedRevision(env *model.Envelope) string {
	if env.Remediation == nil {
		return ""
	}
	return strings.TrimSpace(env.Remediation.PolicyRevision)
}

// persist writes the batch inside a single transaction on the acquired conn and
// returns the machine's tags (read back from the upsert) so the caller can
// resolve the applicable policy without a second query.
func (ig *Ingester) persist(ctx context.Context, conn *pgxpool.Conn, env *model.Envelope, accepted []model.Sample, remoteIP string) ([]string, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	m := env.Machine
	kind := m.Kind
	if kind == "" {
		kind = "windows"
	}
	lastTS := time.Now()
	if len(accepted) > 0 {
		lastTS = accepted[len(accepted)-1].TS
	}
	ipAddrs := cleanIPv4s(m.IPAddrs)
	lastIP := cleanIP(remoteIP)

	// 1) Upsert machine (auto-register on first contact).
	//
	// 1a) An archived machine that reports again is restored (0013). Archiving is
	// a visibility change, so the agent talking again is the strongest evidence
	// the PC is not retired after all; the audit row makes the return visible.
	// A plain PK update that matches nothing for every active machine.
	var restoredHost string
	if err := tx.QueryRow(ctx, `
		UPDATE machine SET archived_at = NULL, archived_by = NULL
		WHERE machine_id = $1 AND archived_at IS NOT NULL
		RETURNING hostname`, m.MachineID).Scan(&restoredHost); err == nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO machine_archive_audit (machine_id, hostname, action, actor_ip)
			VALUES ($1, $2, 'auto_restore', $3)`, m.MachineID, restoredHost, nullStr(remoteIP)); err != nil {
			return nil, err
		}
		log.Printf("ingest: archived machine %s (%s) reported again; restored", m.MachineID, restoredHost)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	// RETURNING tags: the remediation policy is fleet-wide minus tag exclusions, so
	// resolving it needs this machine's tags. The upsert always touches the row, so
	// reading them back here costs nothing and saves a query on a path that runs
	// for 100+ machines every 60 s.
	var tags []string
	if err := tx.QueryRow(ctx, `
		INSERT INTO machine (machine_id, hostname, os, os_build, agent_version, domain, kind, cpu_model, cpu_cores, last_seen,
		                     ip_addrs, last_ip, ip_updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::inet,now())
		ON CONFLICT (machine_id) DO UPDATE SET
			hostname=EXCLUDED.hostname, os=EXCLUDED.os, os_build=EXCLUDED.os_build,
			agent_version=EXCLUDED.agent_version, domain=EXCLUDED.domain, kind=EXCLUDED.kind,
			cpu_model=COALESCE(EXCLUDED.cpu_model, machine.cpu_model),
			cpu_cores=COALESCE(EXCLUDED.cpu_cores, machine.cpu_cores),
			last_seen=GREATEST(machine.last_seen, EXCLUDED.last_seen),
			-- An agent too old to report addresses must not erase what a newer one wrote.
			ip_addrs=CASE WHEN cardinality(EXCLUDED.ip_addrs) > 0 THEN EXCLUDED.ip_addrs ELSE machine.ip_addrs END,
			last_ip=COALESCE(EXCLUDED.last_ip, machine.last_ip),
			ip_updated_at=now()
		RETURNING tags`,
		m.MachineID, m.Hostname, nullStr(m.OS), nullStr(m.OSBuild), nullStr(m.AgentVersion),
		nullStr(m.Domain), kind, nullStr(m.CPUModel), nullInt(m.CPUCores), lastTS,
		ipAddrs, lastIP).Scan(&tags); err != nil {
		return nil, err
	}

	// 1b) Windows Update posture (low cadence; present only on some batches).
	if u := env.UpdateStatus; u != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE machine SET
				pending_updates=$2, reboot_pending=$3,
				updates_last_search=$4, updates_last_install=$5, updates_checked_at=$6
			WHERE machine_id=$1`,
			m.MachineID, nullNonNeg(u.PendingCount), u.RebootPending,
			nullTime(u.LastSearch), nullTime(u.LastInstall), nullTime(u.CheckedAt)); err != nil {
			return nil, err
		}
	}

	// 1c) Battery/power state (low cadence; present only on some batches).
	if bt := env.Battery; bt != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE machine SET
				battery_present=$2, battery_percent=$3, battery_ac=$4, battery_charging=$5,
				battery_health_pct=$6, battery_design_mwh=$7, battery_full_mwh=$8,
				battery_cycles=$9, battery_updated_at=$10
			WHERE machine_id=$1`,
			m.MachineID, bt.Present, nullNonNeg(bt.Percent), bt.ACConnected, bt.Charging,
			nullNonNeg(bt.HealthPct), nullInt(bt.DesignCapacity), nullInt(bt.FullChargeCapacity),
			nullNonNeg(bt.CycleCount), nullTime(bt.CheckedAt)); err != nil {
			return nil, err
		}
	}

	// 2) Previous cumulative counters + boot (for NIC delta).
	var prevSent, prevRecv *int64
	var prevBoot *time.Time
	err = tx.QueryRow(ctx,
		`SELECT net_sent_bytes, net_recv_bytes, boot_time FROM machine_live WHERE machine_id=$1`,
		m.MachineID).Scan(&prevSent, &prevRecv, &prevBoot)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	// 3) Build metric + disk + process rows, computing deltas in chronological order.
	var metricRows [][]any
	var diskRows [][]any
	procDedup := make(map[string]int) // (ts|name) -> index into procRows
	var procRows [][]any

	// Processes belong to the interactive user's session, but the periodic "full"
	// sample that carries processes can momentarily lack logon_user while the vitals
	// samples in the same batch have it. Attribute process rows to the most recent
	// non-empty logon_user in the batch so per-user category views populate.
	effectiveUser := ""
	for _, s := range accepted {
		if s.LogonUser != "" {
			effectiveUser = s.LogonUser
		}
	}

	for _, s := range accepted {
		// A NIC byte delta is meaningful ONLY between two same-boot samples whose
		// cumulative counter did not go backwards. A decrease means the summed NIC
		// set shrank (a virtual/VPN/VM adapter dropped out) or the machine rebooted
		// — NOT a real interval of that much traffic. In those cases we emit NULL
		// (skip the interval) rather than attributing the entire cumulative counter
		// as one interval's delta, which previously produced multi-GB/TB spikes.
		// Sent and recv are evaluated independently.
		var sentDelta, recvDelta *int64
		reboot := prevBoot == nil || !m.BootTime.Equal(*prevBoot)
		if prevSent != nil && !reboot && s.NetSentBytes >= *prevSent {
			sentDelta = i64p(s.NetSentBytes - *prevSent)
		}
		if prevRecv != nil && !reboot && s.NetRecvBytes >= *prevRecv {
			recvDelta = i64p(s.NetRecvBytes - *prevRecv)
		}
		prevSent = i64p(s.NetSentBytes)
		prevRecv = i64p(s.NetRecvBytes)
		b := m.BootTime
		prevBoot = &b

		metricRows = append(metricRows, []any{
			s.TS, m.MachineID, float32(s.CPUPct), s.MemUsed, s.MemTotal,
			s.NetSentBytes, s.NetRecvBytes, sentDelta, recvDelta,
			m.BootTime, nullStr(s.LogonUser), nullStr(s.SessionState),
		})

		for _, d := range s.Disks {
			diskRows = append(diskRows, []any{s.TS, m.MachineID, d.Mount, d.Used, d.Total})
		}

		for _, p := range s.Processes {
			name := NormalizeName(p.Name)
			if name == "" {
				continue
			}
			var catID *int16
			if id, _, ok := ig.cats.Resolve(name); ok {
				catID = &id
			}
			key := s.TS.Format(time.RFC3339Nano) + "|" + name
			puser := s.LogonUser
			if puser == "" {
				puser = effectiveUser
			}
			row := []any{s.TS, m.MachineID, name, catID, nullStr(puser), float32(p.CPUPct), p.MemBytes}
			if idx, exists := procDedup[key]; exists {
				// Keep the row with higher cpu to stay deterministic under duplicates.
				if float32(p.CPUPct) > procRows[idx][5].(float32) {
					procRows[idx] = row
				}
				continue
			}
			procDedup[key] = len(procRows)
			procRows = append(procRows, row)
		}
	}

	// 4) metric_sample — multi-row INSERT ON CONFLICT DO NOTHING.
	if err := execMultiRow(ctx, tx,
		`INSERT INTO metric_sample (ts, machine_id, cpu_pct, mem_used, mem_total, net_sent_bytes, net_recv_bytes, net_sent_delta, net_recv_delta, boot_time, logon_user, session_state) VALUES `,
		12, metricRows,
		` ON CONFLICT (machine_id, ts) DO NOTHING`); err != nil {
		return nil, err
	}

	// 5) disk_usage_sample — multi-row INSERT ON CONFLICT DO NOTHING.
	if err := execMultiRow(ctx, tx,
		`INSERT INTO disk_usage_sample (ts, machine_id, mount, used_bytes, total_bytes) VALUES `,
		5, diskRows,
		` ON CONFLICT (machine_id, mount, ts) DO NOTHING`); err != nil {
		return nil, err
	}

	// 6) process_sample — COPY into a temp table, then INSERT..SELECT ON CONFLICT.
	if len(procRows) > 0 {
		if _, err := tx.Exec(ctx,
			`CREATE TEMP TABLE _proc_in (LIKE process_sample INCLUDING DEFAULTS) ON COMMIT DROP`); err != nil {
			return nil, err
		}
		cols := []string{"ts", "machine_id", "process_name", "category_id", "logon_user", "cpu_pct", "mem_bytes"}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"_proc_in"}, cols, pgx.CopyFromRows(procRows)); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO process_sample (ts, machine_id, process_name, category_id, logon_user, cpu_pct, mem_bytes)
			SELECT ts, machine_id, process_name, category_id, logon_user, cpu_pct, mem_bytes FROM _proc_in
			ON CONFLICT (machine_id, ts, process_name) DO NOTHING`); err != nil {
			return nil, err
		}
	}

	// 7) events.
	if len(env.Events) > 0 {
		var eventRows [][]any
		for _, e := range env.Events {
			if e.TS.IsZero() || e.Kind == "" {
				continue
			}
			detail, _ := json.Marshal(e.Detail)
			if len(detail) == 0 {
				detail = []byte("{}")
			}
			eventRows = append(eventRows, []any{
				e.TS, m.MachineID, e.Kind, nullStr(e.ProcessName), nullStr(e.Severity), detail,
			})
		}
		if err := execMultiRow(ctx, tx,
			`INSERT INTO event (ts, machine_id, kind, process_name, severity, detail) VALUES `,
			6, eventRows, ``); err != nil {
			return nil, err
		}
	}

	// 8) disk_health upsert.
	for _, dh := range env.DiskHealth {
		if _, err := tx.Exec(ctx, `
			INSERT INTO disk_health (machine_id, disk, smart_ok, predict_fail, updated_at)
			VALUES ($1,$2,$3,$4, now())
			ON CONFLICT (machine_id, disk) DO UPDATE SET
				smart_ok=EXCLUDED.smart_ok, predict_fail=EXCLUDED.predict_fail, updated_at=now()`,
			m.MachineID, dh.Disk, dh.SMARTOk, dh.PredictFail); err != nil {
			return nil, err
		}
	}

	// 8b) Foreground app-usage (NAME ONLY), attributed to the batch's current user.
	if len(env.AppFocus) > 0 {
		var afRows [][]any
		for _, af := range env.AppFocus {
			name := NormalizeName(af.Name)
			if name == "" || len(name) > maxAppNameLen || af.Seconds <= 0 {
				continue
			}
			afRows = append(afRows, []any{lastTS, m.MachineID, nullStr(effectiveUser), name, af.Seconds})
		}
		if err := execMultiRow(ctx, tx,
			`INSERT INTO app_focus_sample (ts, machine_id, logon_user, app_name, seconds) VALUES `,
			5, afRows,
			` ON CONFLICT (machine_id, ts, app_name) DO NOTHING`); err != nil {
			return nil, err
		}
	}

	// 9) machine_live upsert with the latest sample (never regress on ts).
	if len(accepted) > 0 {
		last := accepted[len(accepted)-1]
		// Disks (and processes) ride only on the periodic "full" sample, so the
		// trailing vitals-only sample usually carries none. Use the most recent
		// sample in the batch that actually has disk data; the COALESCE below then
		// preserves the last known value across batches that contain no full sample.
		latestDisks := last.Disks
		for i := len(accepted) - 1; i >= 0; i-- {
			if len(accepted[i].Disks) > 0 {
				latestDisks = accepted[i].Disks
				break
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO machine_live (machine_id, ts, cpu_pct, mem_used, mem_total, disk_min_free_pct, net_sent_bytes, net_recv_bytes, boot_time, logon_user, session_state)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (machine_id) DO UPDATE SET
				ts=EXCLUDED.ts, cpu_pct=EXCLUDED.cpu_pct, mem_used=EXCLUDED.mem_used,
				mem_total=EXCLUDED.mem_total,
				disk_min_free_pct=COALESCE(EXCLUDED.disk_min_free_pct, machine_live.disk_min_free_pct),
				net_sent_bytes=EXCLUDED.net_sent_bytes, net_recv_bytes=EXCLUDED.net_recv_bytes,
				boot_time=EXCLUDED.boot_time, logon_user=EXCLUDED.logon_user, session_state=EXCLUDED.session_state
			WHERE machine_live.ts <= EXCLUDED.ts`,
			m.MachineID, last.TS, float32(last.CPUPct), last.MemUsed, last.MemTotal,
			minFreePct(latestDisks), last.NetSentBytes, last.NetRecvBytes, m.BootTime,
			nullStr(last.LogonUser), nullStr(last.SessionState)); err != nil {
			return nil, err
		}
	}

	// 10) Endpoint-hygiene report (low cadence; present only on some batches, and
	// never at all from an agent older than the feature).
	if r := env.Remediation; r != nil {
		if err := persistRemediation(ctx, tx, m.MachineID, r); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return tags, nil
}

// execMultiRow builds and runs a single multi-row INSERT with $N placeholders.
func execMultiRow(ctx context.Context, tx pgx.Tx, prefix string, cols int, rows [][]any, suffix string) error {
	if len(rows) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString(prefix)
	args := make([]any, 0, len(rows)*cols)
	p := 1
	for i, row := range rows {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('(')
		for j := 0; j < cols; j++ {
			if j > 0 {
				sb.WriteByte(',')
			}
			sb.WriteByte('$')
			sb.WriteString(strconv.Itoa(p))
			p++
			args = append(args, row[j])
		}
		sb.WriteByte(')')
	}
	sb.WriteString(suffix)
	_, err := tx.Exec(ctx, sb.String(), args...)
	return err
}

// readBody reads the request body, transparently gunzipping when the request is
// gzip-encoded, and enforces the decompressed size limit.
func readBody(r *http.Request, limit int64) ([]byte, error) {
	var reader io.Reader = r.Body
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errTooLarge
	}
	return data, nil
}

// minFreePct returns the worst mount's free percentage, or nil when unknown.
func minFreePct(disks []model.Disk) *float64 {
	var min *float64
	for _, d := range disks {
		if d.Total <= 0 {
			continue
		}
		free := float64(d.Total-d.Used) / float64(d.Total) * 100
		if min == nil || free < *min {
			v := free
			min = &v
		}
	}
	return min
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullInt(n int) *int {
	if n <= 0 {
		return nil
	}
	return &n
}

// nullNonNeg maps a negative count (agent's "unknown" sentinel) to SQL NULL and
// keeps 0 or positive counts as-is.
func nullNonNeg(n int) *int {
	if n < 0 {
		return nil
	}
	return &n
}

// nullTime maps the zero time to SQL NULL.
func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func i64p(v int64) *int64 { return &v }

// maxIPAddrs bounds the agent-reported address list (a workstation has one or
// two; anything beyond a handful is a virtualisation host or a hostile payload).
const maxIPAddrs = 8

// cleanIPv4s keeps only well-formed, unicast, non-loopback, non-link-local IPv4
// addresses in canonical dotted form, de-duplicated and capped. The stored
// value is what the panel renders, so agent-supplied text never reaches it raw.
func cleanIPv4s(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, s := range in {
		ip := net.ParseIP(strings.TrimSpace(s))
		ip4 := ip.To4()
		if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() || ip4.IsUnspecified() || ip4.IsMulticast() {
			continue
		}
		c := ip4.String()
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
		if len(out) == maxIPAddrs {
			break
		}
	}
	return out
}

// cleanIP validates the request source address for the inet column; an
// unparsable value (or an empty one) becomes NULL rather than a failed insert.
func cleanIP(s string) *string {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil {
		return nil
	}
	c := ip.String()
	return &c
}
