// Package alerts implements the background evaluator: it opens/refreshes/resolves
// alert instances from alert_rule against the hot machine_live table + rollups,
// derives usage_session transitions from recent metric_sample rows, keeps
// machine_live.open_alerts in sync, and pushes lifecycle events to the WS hub.
package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aliyalinkilic/tolikopa/server/internal/config"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
	"github.com/aliyalinkilic/tolikopa/server/internal/workhours"
)

const (
	evalInterval = 30 * time.Second
	sessionGap   = 5 * time.Minute
	// resolveGrace damps flapping: an open alert is resolved only after its
	// condition has been continuously clear for this long. A machine that keeps
	// going briefly online then offline stays under ONE open alert instead of
	// re-firing a new one (with a new notification) on every cycle.
	resolveGrace = 10 * time.Minute
	// crashResolveGrace is resolveGrace for crash_storm (see resolveGraceFor).
	crashResolveGrace = time.Hour
	// usageHysteresis / diskHysteresis: percentage points an open cpu/mem (resp.
	// disk_low) alert must clear its threshold by before it counts as clear.
	usageHysteresis = 5.0
	diskHysteresis  = 2.0
	kindActive      = "active"
	kindLoggedOn    = "logged_on"
)

// Publisher pushes alert lifecycle events to subscribers (implemented by ws.Hub).
type Publisher interface {
	PublishAlert(kind string, payload any)
}

// Engine runs the periodic evaluation + derivation loop.
type Engine struct {
	pool *pgxpool.Pool
	cfg  *config.Config
	pub  Publisher

	lastProcessed time.Time // usage_session derivation cursor

	// lang is the instance default language (app_setting.default_lang), re-read
	// at the start of every evaluation. Only the evaluator goroutine touches it.
	lang string

	// sched is the work-hours schedule, re-read every evaluation (evaluator
	// goroutine only). A failed read keeps the previous one.
	sched workhours.Schedule
}

// NewEngine constructs the engine. pub may be nil (no WS push).
func NewEngine(pool *pgxpool.Pool, cfg *config.Config, pub Publisher) *Engine {
	return &Engine{pool: pool, cfg: cfg, pub: pub, lastProcessed: time.Now().Add(-15 * time.Minute), lang: LangTR,
		sched: workhours.Compile(workhours.Default())}
}

// Run evaluates rules + derives sessions every evalInterval until ctx is done.
func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(evalInterval)
	defer t.Stop()
	// Kick once immediately so state is fresh shortly after boot.
	e.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.tick(ctx)
		}
	}
}

func (e *Engine) tick(ctx context.Context) {
	if err := e.evaluate(ctx); err != nil {
		log.Printf("alerts: evaluate: %v", err)
	}
	if err := e.deriveSessions(ctx); err != nil {
		log.Printf("alerts: deriveSessions: %v", err)
	}
	if _, err := e.pool.Exec(ctx, `
		UPDATE machine_live ml SET open_alerts = COALESCE((
			SELECT count(*) FROM alert a WHERE a.machine_id = ml.machine_id AND a.status <> 'resolved'
		), 0)`); err != nil {
		log.Printf("alerts: open_alerts sync: %v", err)
	}
}

type rule struct {
	enabled    bool
	warn, crit *float64
	forSeconds int
	params     map[string]any
}

type activeAlert struct {
	machineID string
	hostname  string
	rule      string
	dedupKey  string
	severity  string
	value     *float64
	// detail is the alert's params object: every value the message interpolates
	// (docs/API.md §6). It is stored in alert.detail and served as AlertDTO.Params;
	// the text in alert.message is rendered from it and is only a fallback.
	detail Params
}

func (e *Engine) loadRules(ctx context.Context) (map[string]rule, error) {
	rows, err := e.pool.Query(ctx,
		`SELECT rule, enabled, warn_threshold, crit_threshold, for_seconds, params FROM alert_rule`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]rule)
	for rows.Next() {
		var name string
		var r rule
		var params []byte
		if err := rows.Scan(&name, &r.enabled, &r.warn, &r.crit, &r.forSeconds, &params); err != nil {
			return nil, err
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &r.params)
		}
		out[name] = r
	}
	return out, rows.Err()
}

func (e *Engine) evaluate(ctx context.Context) error {
	rules, err := e.loadRules(ctx)
	if err != nil {
		return err
	}
	e.lang = e.loadLang(ctx)
	e.loadSchedule(ctx)
	now := time.Now()

	evaluators := map[string]evaluator{
		"offline":     e.evalOffline,
		"cpu_high":    e.evalCPU,
		"mem_high":    e.evalMem,
		"disk_low":    e.evalDiskLow,
		"smart_fail":  e.evalSmart,
		"crash_storm": e.evalCrashStorm,
	}

	for name, fn := range evaluators {
		r, ok := rules[name]
		var res evalResult
		if ok && r.enabled {
			open, err := e.loadOpen(ctx, name)
			if err != nil {
				log.Printf("alerts: rule %s: open alerts: %v", name, err)
				continue
			}
			res, err = fn(ctx, r, open, now)
			if err != nil {
				log.Printf("alerts: rule %s: %v", name, err)
				continue
			}
		}
		if err := e.reconcile(ctx, name, res); err != nil {
			log.Printf("alerts: reconcile %s: %v", name, err)
		}
	}
	return nil
}

// evaluator computes one rule. open maps the rule's non-resolved dedup keys to
// their machine id (for hysteresis and "hold" decisions).
type evaluator func(ctx context.Context, r rule, open map[string]string, now time.Time) (evalResult, error)

// evalResult is what an evaluator decided this round.
type evalResult struct {
	// actives: the condition holds — open or refresh.
	actives []activeAlert
	// hold: open alerts whose condition cannot be judged now (no fresh data, e.g.
	// the machine of a disk_low alert is offline). Neither refreshed nor resolved:
	// a full disk does not become "fixed" because the PC went to sleep.
	hold []string
	// clear: open alerts to resolve at once (bypassing the resolve grace), keyed
	// by dedup key, valued by the reason stored in detail.resolved_reason.
	clear map[string]string
}

// loadOpen returns the rule's non-resolved alerts: dedup_key → machine_id.
func (e *Engine) loadOpen(ctx context.Context, ruleName string) (map[string]string, error) {
	rows, err := e.pool.Query(ctx,
		`SELECT dedup_key, COALESCE(machine_id, '') FROM alert WHERE rule=$1 AND status <> 'resolved'`, ruleName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, mid string
		if err := rows.Scan(&k, &mid); err != nil {
			return nil, err
		}
		out[k] = mid
	}
	return out, rows.Err()
}

// loadSchedule re-reads the work-hours schedule. A failure keeps the previous one.
func (e *Engine) loadSchedule(ctx context.Context) {
	w, err := workhours.Load(ctx, e.pool)
	if err != nil {
		log.Printf("alerts: work hours: %v", err)
		return
	}
	e.sched = workhours.Compile(w)
}

// reconcile upserts the active alerts, resolves the ones to clear at once, and
// resolves any other open alert of this rule whose condition no longer holds.
func (e *Engine) reconcile(ctx context.Context, ruleName string, res evalResult) error {
	keep := make([]string, 0, len(res.actives)+len(res.hold)) // never nil: ANY(NULL) matches nothing
	for _, a := range res.actives {
		keep = append(keep, a.dedupKey)
		if err := e.upsert(ctx, a); err != nil {
			return err
		}
	}
	keep = append(keep, res.hold...)

	var resolvedIDs []int64
	for key, reason := range res.clear {
		var id int64
		err := e.pool.QueryRow(ctx, `
			UPDATE alert SET status='resolved', resolved_at=now(),
				detail = detail || jsonb_build_object('resolved_reason', $3::text)
			WHERE rule=$1 AND dedup_key=$2 AND status<>'resolved'
			RETURNING id`, ruleName, key, reason).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		resolvedIDs = append(resolvedIDs, id)
	}

	// Resolve stale alerts — but only once the condition has been clear for
	// the rule's resolve grace (last_seen_at is refreshed each eval the condition
	// holds), so brief blips don't resolve+reopen an alert on a loop.
	rows, err := e.pool.Query(ctx, `
		UPDATE alert SET status='resolved', resolved_at=now()
		WHERE rule=$1 AND status<>'resolved' AND NOT (dedup_key = ANY($2))
		  AND last_seen_at < $3
		RETURNING id`, ruleName, keep, time.Now().Add(-resolveGraceFor(ruleName)))
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		resolvedIDs = append(resolvedIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range resolvedIDs {
		if e.pub != nil {
			e.pub.PublishAlert("alert.resolved", map[string]any{"id": id})
		}
	}
	return nil
}

// resolveGraceFor is how long a rule's condition must stay clear before its
// alert resolves. Crash storms get an hour: in the previous installation's data
// almost every storm was ONE background service crash-looping for days, which
// at 10 minutes re-opened (and re-notified) the same storm hundreds of times.
func resolveGraceFor(ruleName string) time.Duration {
	if ruleName == "crash_storm" {
		return crashResolveGrace
	}
	return resolveGrace
}

// loadLang reads the instance default language. Any failure keeps the previous
// value: a transient DB error must not flip the language of stored messages.
func (e *Engine) loadLang(ctx context.Context) string {
	var v *string
	err := e.pool.QueryRow(ctx, `SELECT value #>> '{}' FROM app_setting WHERE key='default_lang'`).Scan(&v)
	if err != nil || v == nil {
		return e.lang
	}
	return NormalizeLang(*v)
}

func (e *Engine) upsert(ctx context.Context, a activeAlert) error {
	if a.detail == nil {
		a.detail = Params{}
	}
	a.detail["hostname"] = a.hostname
	message := RenderMessage(e.lang, a.rule, a.detail)
	detail, _ := json.Marshal(a.detail)
	if len(detail) == 0 {
		detail = []byte("{}")
	}
	var (
		id         int64
		openedAt   time.Time
		lastSeenAt time.Time
		inserted   bool
	)
	err := e.pool.QueryRow(ctx, `
		INSERT INTO alert (machine_id, rule, dedup_key, severity, status, value, message, opened_at, last_seen_at, detail)
		VALUES ($1,$2,$3,$4,'open',$5,$6, now(), now(), $7)
		ON CONFLICT (dedup_key) WHERE status <> 'resolved'
		DO UPDATE SET last_seen_at=now(), severity=EXCLUDED.severity, value=EXCLUDED.value,
			message=EXCLUDED.message, detail=EXCLUDED.detail
		RETURNING id, opened_at, last_seen_at, (xmax = 0) AS inserted`,
		nullStr(a.machineID), a.rule, a.dedupKey, a.severity, a.value, message, detail,
	).Scan(&id, &openedAt, &lastSeenAt, &inserted)
	if err != nil {
		return err
	}
	if inserted {
		dto := model.AlertDTO{
			ID: id, MachineID: a.machineID, Hostname: a.hostname, Rule: a.rule,
			Severity: a.severity, Status: "open", Value: a.value, Message: message,
			OpenedAt: openedAt, LastSeenAt: lastSeenAt, Params: a.detail, Detail: a.detail,
		}
		if e.pub != nil {
			e.pub.PublishAlert("alert.opened", dto)
		}
		// Only critical alerts e-mail; info and warning (every work-hours offline
		// kind included) never do.
		if a.severity == "critical" {
			e.sendCriticalEmail(dto, e.lang)
		}
	}
	return nil
}

// ── Rule evaluators ──────────────────────────────────────────────────────────

// Offline alert kinds (params.kind) for work-hours devices. Servers' offline
// alerts carry no kind.
const (
	offlineNotStarted  = "not_started"  // not seen at all since today's start (info)
	offlineWentOffline = "went_offline" // seen today, then went offline (warning)
	offlineStale       = "stale"        // missed stale_after_days whole work days (info)
)

// offlineVerdict is the pure offline decision for one machine.
type offlineVerdict struct {
	sev       string  // "" = no alert this round
	thr       float64 // threshold (seconds) that fired; 0 for stale
	kind      string  // params.kind for work-hours devices
	workHours bool    // the machine is a work-hours-only device
	clear     bool    // resolve an open alert now (reason below)
	reason    string  // detail.resolved_reason when clear
}

// decideOffline applies the offline rule to one machine last seen at lastSeen.
// isOpen/openKind describe the machine's currently open offline alert, if any
// (openKind is its params.kind, "" for alerts written before kinds existed).
//
// Machines without a work-hours tag (servers) keep the 24/7 behaviour: warn/crit
// on seconds since last seen. Work-hours devices (docs/API.md §6.1):
//   - a device that has missed stale_after_days whole work days is "stale": one
//     long-lived info alert (kind stale), kept around the clock — it is neither
//     cleared at the end of the day nor re-opened every morning — until the
//     device reports again (or is archived);
//   - otherwise, outside the schedule nothing fires and an open alert is cleared;
//   - inside it the offline time counts from max(lastSeen, today's start), so a
//     PC shut down last evening is "offline for 0 min" at 09:00;
//   - the threshold is max(rule warn, schedule offline_after_minutes) — staff
//     PCs sleep over lunch; a 5-minute nap is not an incident;
//   - a NEW alert opens only outside the morning/evening grace (an open one is
//     kept until the day ends);
//   - a device not switched on at all today is info (kind not_started), one that
//     went offline during the day is warning (kind went_offline); never critical:
//     a staff PC that is off usually means its user is away, which is no reason
//     for a critical e-mail.
func decideOffline(warn, crit *float64, s workhours.Schedule, tags []string, now, lastSeen time.Time, isOpen bool, openKind string) offlineVerdict {
	if !s.Applies(tags) {
		sev, thr := sevHigh(now.Sub(lastSeen).Seconds(), warn, crit)
		return offlineVerdict{sev: sev, thr: thr}
	}
	v := offlineVerdict{workHours: true}
	if s.Stale(now, lastSeen) {
		v.sev, v.kind = "info", offlineStale
		return v
	}
	p := s.OfflinePresence(now, lastSeen)
	if !p.InHours {
		v.clear = true
		v.reason = "work_hours_end"
		if isOpen && openKind == offlineStale {
			// No longer stale outside work hours: the device reported again (or
			// the cutoff was raised) — not a work-hours-end closure.
			v.reason = "not_stale"
		}
		return v
	}
	var thr float64
	switch {
	case warn != nil:
		thr = *warn
	case crit != nil:
		thr = *crit
	default:
		return v // rule has no thresholds at all
	}
	if floor := s.OfflineAfter().Seconds(); floor > thr {
		thr = floor
	}
	if p.Age.Seconds() < thr || (!isOpen && !p.MayOpen) {
		return v
	}
	v.thr = thr
	if p.NotStarted {
		v.sev, v.kind = "info", offlineNotStarted
	} else {
		v.sev, v.kind = "warning", offlineWentOffline
	}
	return v
}

func (e *Engine) evalOffline(ctx context.Context, r rule, open map[string]string, now time.Time) (evalResult, error) {
	res := evalResult{clear: map[string]string{}}
	kinds, err := e.openOfflineKinds(ctx)
	if err != nil {
		return res, err
	}
	rows, err := e.pool.Query(ctx, `
		SELECT ml.machine_id, m.hostname, ml.ts, m.tags
		FROM machine_live ml JOIN machine m ON m.machine_id=ml.machine_id
		WHERE m.archived_at IS NULL`)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	for rows.Next() {
		var mid, host string
		var seen time.Time
		var tags []string
		if err := rows.Scan(&mid, &host, &seen, &tags); err != nil {
			return res, err
		}
		key := "offline:" + mid
		_, isOpen := open[key]
		d := decideOffline(r.warn, r.crit, e.sched, tags, now, seen, isOpen, kinds[key])
		if d.clear {
			if isOpen {
				res.clear[key] = d.reason
			}
			continue
		}
		if d.sev == "" {
			continue
		}
		age := now.Sub(seen).Seconds()
		if age < 0 {
			age = 0
		}
		v := age
		detail := Params{"age_seconds": int(age), "minutes": int(age / 60), "unit": "s"}
		if d.kind == offlineStale {
			detail["days"] = int(age / 86400)
			detail["stale_after_days"] = e.sched.StaleAfterDays()
			detail["last_seen"] = seen.UTC().Format(time.RFC3339)
		} else {
			detail["threshold"] = d.thr
		}
		if d.workHours {
			detail["work_hours"] = true
		}
		if d.kind != "" {
			detail["kind"] = d.kind
		}
		res.actives = append(res.actives, activeAlert{
			machineID: mid, hostname: host, rule: "offline", dedupKey: key,
			severity: d.sev, value: &v, detail: detail,
		})
	}
	return res, rows.Err()
}

// openOfflineKinds maps the dedup key of every open offline alert to its
// params.kind ("" when unset).
func (e *Engine) openOfflineKinds(ctx context.Context) (map[string]string, error) {
	rows, err := e.pool.Query(ctx, `
		SELECT dedup_key, COALESCE(detail->>'kind', '') FROM alert
		WHERE rule='offline' AND status <> 'resolved'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, kind string
		if err := rows.Scan(&k, &kind); err != nil {
			return nil, err
		}
		out[k] = kind
	}
	return out, rows.Err()
}

func (e *Engine) evalCPU(ctx context.Context, r rule, open map[string]string, _ time.Time) (evalResult, error) {
	return e.evalSustained(ctx, r, open, "cpu_high", "avg(ms.cpu_avg)")
}

func (e *Engine) evalMem(ctx context.Context, r rule, open map[string]string, _ time.Time) (evalResult, error) {
	return e.evalSustained(ctx, r, open, "mem_high", "avg(ms.mem_ratio_avg)*100")
}

// evalSustained evaluates a metric_5m-based threshold over the rule's for_seconds
// window (real-time aggregation keeps the live edge fresh). An open alert stays
// open until the value falls usageHysteresis points below the threshold: in the
// previous installation's data most mem_high alerts sat at 90–91 % against a
// 90 % threshold and re-opened within 30 minutes.
func (e *Engine) evalSustained(ctx context.Context, r rule, open map[string]string, ruleName, expr string) (evalResult, error) {
	var res evalResult
	win := float64(r.forSeconds)
	if win <= 0 {
		win = 300
	}
	q := fmt.Sprintf(`
		SELECT ms.machine_id, m.hostname, %s AS v, count(*)
		FROM metric_5m ms JOIN machine m ON m.machine_id=ms.machine_id
		WHERE ms.bucket >= now() - make_interval(secs => $1) AND m.archived_at IS NULL
		GROUP BY ms.machine_id, m.hostname`, expr)
	rows, err := e.pool.Query(ctx, q, win)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	for rows.Next() {
		var mid, host string
		var v *float64
		var cnt int
		if err := rows.Scan(&mid, &host, &v, &cnt); err != nil {
			return res, err
		}
		if cnt < 1 || v == nil {
			continue
		}
		key := ruleName + ":" + mid
		_, isOpen := open[key]
		sev, thr := sevHighHyst(*v, r.warn, r.crit, isOpen, usageHysteresis)
		if sev == "" {
			continue
		}
		val := *v
		res.actives = append(res.actives, activeAlert{
			machineID: mid, hostname: host, rule: ruleName, dedupKey: key,
			severity: sev, value: &val,
			detail: Params{"value": val, "threshold": thr, "unit": "%", "window_seconds": int(win)},
		})
	}
	return res, rows.Err()
}

func (e *Engine) evalDiskLow(ctx context.Context, r rule, open map[string]string, _ time.Time) (evalResult, error) {
	var res evalResult
	// Latest usage per machine+mount; time-bounded so only recent chunks scan.
	rows, err := e.pool.Query(ctx, `
		SELECT DISTINCT ON (dus.machine_id, dus.mount)
			dus.machine_id, m.hostname, dus.mount,
			CASE WHEN dus.total_bytes > 0
				THEN (dus.total_bytes - dus.used_bytes)::float8 / dus.total_bytes * 100
				ELSE NULL END AS free_pct
		FROM disk_usage_sample dus JOIN machine m ON m.machine_id=dus.machine_id
		WHERE dus.ts >= now() - interval '2 hours' AND m.archived_at IS NULL
		ORDER BY dus.machine_id, dus.mount, dus.ts DESC`)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	reporting := map[string]bool{}
	for rows.Next() {
		var mid, host, mount string
		var free *float64
		if err := rows.Scan(&mid, &host, &mount, &free); err != nil {
			return res, err
		}
		reporting[mid] = true
		if free == nil {
			continue
		}
		key := "disk_low:" + mid + ":" + mount
		_, isOpen := open[key]
		sev, thr := sevLowHyst(*free, r.warn, r.crit, isOpen, diskHysteresis)
		if sev == "" {
			continue
		}
		v := *free
		res.actives = append(res.actives, activeAlert{
			machineID: mid, hostname: host, rule: "disk_low",
			dedupKey: key, severity: sev, value: &v,
			detail: Params{"mount": mount, "free_pct": *free, "threshold": thr, "unit": "%"},
		})
	}
	if err := rows.Err(); err != nil {
		return res, err
	}
	// A machine that sent no disk sample for 2 h (switched off, asleep) has not
	// freed any space: keep its alerts as they are. Without this every evening
	// shutdown resolved the alert and the next morning re-opened it — one disk
	// in the old data had 137 alerts for the same nearly full C:.
	res.hold = holdSilent(open, reporting)
	return res, nil
}

// holdSilent returns the open alert keys whose machine did not report.
func holdSilent(open map[string]string, reporting map[string]bool) []string {
	var out []string
	for key, mid := range open {
		if mid != "" && !reporting[mid] {
			out = append(out, key)
		}
	}
	return out
}

func (e *Engine) evalSmart(ctx context.Context, r rule, _ map[string]string, _ time.Time) (evalResult, error) {
	var res evalResult
	rows, err := e.pool.Query(ctx, `
		SELECT dh.machine_id, m.hostname, dh.disk, dh.smart_ok, dh.predict_fail
		FROM disk_health dh JOIN machine m ON m.machine_id=dh.machine_id
		WHERE (dh.predict_fail = true OR dh.smart_ok = false) AND m.archived_at IS NULL`)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	for rows.Next() {
		var mid, host, disk string
		var smartOK, predictFail *bool
		if err := rows.Scan(&mid, &host, &disk, &smartOK, &predictFail); err != nil {
			return res, err
		}
		res.actives = append(res.actives, activeAlert{
			machineID: mid, hostname: host, rule: "smart_fail",
			dedupKey: "smart_fail:" + mid + ":" + disk, severity: "critical",
			detail: Params{"disk": disk, "smart_ok": smartOK, "predict_fail": predictFail},
		})
	}
	return res, rows.Err()
}

// ignoredApps reads crash_storm params.ignore_apps: process base names (case-
// insensitive, ".exe" optional) whose crashes never count toward a storm.
func ignoredApps(params map[string]any) []string {
	out := []string{}
	list, _ := params["ignore_apps"].([]any)
	for _, v := range list {
		s, ok := v.(string)
		if !ok {
			continue
		}
		s = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".exe")
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (e *Engine) evalCrashStorm(ctx context.Context, r rule, _ map[string]string, _ time.Time) (evalResult, error) {
	var res evalResult
	win := 600.0
	if r.params != nil {
		if w, ok := r.params["window_seconds"].(float64); ok && w > 0 {
			win = w
		}
	}
	// app is the most frequent faulting process base name in the window, apps
	// the number of distinct ones — almost every storm is one crash-looping app.
	rows, err := e.pool.Query(ctx, `
		SELECT e.machine_id, m.hostname, count(*)::float8,
		       mode() WITHIN GROUP (ORDER BY lower(e.process_name)),
		       count(DISTINCT lower(e.process_name))::int
		FROM event e JOIN machine m ON m.machine_id=e.machine_id
		WHERE e.kind IN ('app_crash','app_hang') AND e.ts >= now() - make_interval(secs => $1)
		  AND m.archived_at IS NULL
		  AND NOT (regexp_replace(lower(COALESCE(e.process_name, '')), '\.exe$', '') = ANY($2))
		GROUP BY e.machine_id, m.hostname`, win, ignoredApps(r.params))
	if err != nil {
		return res, err
	}
	defer rows.Close()
	for rows.Next() {
		var mid, host string
		var cnt float64
		var app *string
		var apps int
		if err := rows.Scan(&mid, &host, &cnt, &app, &apps); err != nil {
			return res, err
		}
		sev, thr := sevHigh(cnt, r.warn, r.crit)
		if sev == "" {
			continue
		}
		v := cnt
		detail := Params{"count": int(cnt), "window_seconds": int(win), "window_minutes": int(win / 60), "threshold": thr, "apps": apps}
		if app != nil && *app != "" {
			detail["app"] = *app
		}
		res.actives = append(res.actives, activeAlert{
			machineID: mid, hostname: host, rule: "crash_storm", dedupKey: "crash_storm:" + mid,
			severity: sev, value: &v, detail: detail,
		})
	}
	return res, rows.Err()
}

// ── usage_session derivation ─────────────────────────────────────────────────

// deriveSessions scans metric_sample rows since the last cursor and opens/closes
// active/logged_on usage_session rows on state transitions and heartbeat gaps.
func (e *Engine) deriveSessions(ctx context.Context) error {
	until := time.Now()

	// Load currently-open sessions so we only write on real transitions.
	openMap, err := e.loadOpenSessions(ctx)
	if err != nil {
		return err
	}

	rows, err := e.pool.Query(ctx, `
		SELECT machine_id, ts, logon_user, session_state
		FROM metric_sample
		WHERE ts > $1 AND ts <= $2
		ORDER BY machine_id, ts`, e.lastProcessed, until)
	if err != nil {
		return err
	}
	defer rows.Close()

	var curMachine string
	var prevTS time.Time
	hasPrev := false

	for rows.Next() {
		var mid string
		var ts time.Time
		var user, state *string
		if err := rows.Scan(&mid, &ts, &user, &state); err != nil {
			return err
		}
		if mid != curMachine {
			curMachine = mid
			hasPrev = false
		}
		u := ""
		if user != nil {
			u = *user
		}
		st := ""
		if state != nil {
			st = *state
		}
		// "idle" (logged on, unlocked, no input >= 5 min) intentionally does NOT
		// count as active — only as logged_on (like locked/disconnected).
		active := st == "active"
		loggedOn := u != "" && st != "none"

		if hasPrev && ts.Sub(prevTS) > sessionGap {
			e.closeIfOpen(ctx, openMap, mid, kindActive, prevTS)
			e.closeIfOpen(ctx, openMap, mid, kindLoggedOn, prevTS)
		}
		if active {
			e.openIfClosed(ctx, openMap, mid, kindActive, u, ts)
		} else {
			e.closeIfOpen(ctx, openMap, mid, kindActive, ts)
		}
		if loggedOn {
			e.openIfClosed(ctx, openMap, mid, kindLoggedOn, u, ts)
		} else {
			e.closeIfOpen(ctx, openMap, mid, kindLoggedOn, ts)
		}
		prevTS = ts
		hasPrev = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	e.lastProcessed = until

	// Close sessions for machines that have gone silent (offline) so they don't
	// stay open forever.
	if _, err := e.pool.Exec(ctx, `
		UPDATE usage_session us SET ended_at = ml.ts, is_open = false
		FROM machine_live ml
		WHERE us.machine_id = ml.machine_id AND us.is_open = true
		  AND now() - ml.ts > make_interval(secs => $1) AND ml.ts >= us.started_at`,
		sessionGap.Seconds()); err != nil {
		return err
	}
	return nil
}

func (e *Engine) loadOpenSessions(ctx context.Context) (map[string]map[string]bool, error) {
	rows, err := e.pool.Query(ctx, `SELECT machine_id, kind FROM usage_session WHERE is_open = true`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]map[string]bool)
	for rows.Next() {
		var mid, kind string
		if err := rows.Scan(&mid, &kind); err != nil {
			return nil, err
		}
		if m[mid] == nil {
			m[mid] = make(map[string]bool)
		}
		m[mid][kind] = true
	}
	return m, rows.Err()
}

func (e *Engine) openIfClosed(ctx context.Context, openMap map[string]map[string]bool, mid, kind, user string, ts time.Time) {
	if openMap[mid][kind] {
		return
	}
	if _, err := e.pool.Exec(ctx, `
		INSERT INTO usage_session (machine_id, logon_user, kind, started_at, is_open)
		VALUES ($1,$2,$3,$4,true)
		ON CONFLICT (machine_id, kind, started_at) DO NOTHING`,
		mid, nullStr(user), kind, ts); err != nil {
		log.Printf("alerts: open session: %v", err)
		return
	}
	if openMap[mid] == nil {
		openMap[mid] = make(map[string]bool)
	}
	openMap[mid][kind] = true
}

func (e *Engine) closeIfOpen(ctx context.Context, openMap map[string]map[string]bool, mid, kind string, ts time.Time) {
	if !openMap[mid][kind] {
		return
	}
	if _, err := e.pool.Exec(ctx, `
		UPDATE usage_session SET ended_at=$3, is_open=false
		WHERE machine_id=$1 AND kind=$2 AND is_open=true`, mid, kind, ts); err != nil {
		log.Printf("alerts: close session: %v", err)
		return
	}
	openMap[mid][kind] = false
}

// ── helpers ──────────────────────────────────────────────────────────────────

// sevHigh classifies a "higher is worse" value and returns the threshold that
// fired, so the alert can say what limit was crossed.
func sevHigh(v float64, warn, crit *float64) (string, float64) {
	if crit != nil && v >= *crit {
		return "critical", *crit
	}
	if warn != nil && v >= *warn {
		return "warning", *warn
	}
	return "", 0
}

// sevHighHyst is sevHigh with hysteresis: an alert that is already open keeps
// (at least) warning until v drops margin below the threshold it would need.
func sevHighHyst(v float64, warn, crit *float64, open bool, margin float64) (string, float64) {
	sev, thr := sevHigh(v, warn, crit)
	if sev != "" || !open {
		return sev, thr
	}
	if warn != nil {
		if v >= *warn-margin {
			return "warning", *warn
		}
		return "", 0
	}
	if crit != nil && v >= *crit-margin {
		return "critical", *crit
	}
	return "", 0
}

// sevLowHyst is sevHighHyst for "lower is worse" values.
func sevLowHyst(v float64, warn, crit *float64, open bool, margin float64) (string, float64) {
	sev, thr := sevLow(v, warn, crit)
	if sev != "" || !open {
		return sev, thr
	}
	if warn != nil {
		if v <= *warn+margin {
			return "warning", *warn
		}
		return "", 0
	}
	if crit != nil && v <= *crit+margin {
		return "critical", *crit
	}
	return "", 0
}

// sevLow is sevHigh for "lower is worse" values (free disk space).
func sevLow(v float64, warn, crit *float64) (string, float64) {
	if crit != nil && v <= *crit {
		return "critical", *crit
	}
	if warn != nil && v <= *warn {
		return "warning", *warn
	}
	return "", 0
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
