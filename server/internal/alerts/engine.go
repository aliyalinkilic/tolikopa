// Package alerts implements the background evaluator: it opens/refreshes/resolves
// alert instances from alert_rule against the hot machine_live table + rollups,
// derives usage_session transitions from recent metric_sample rows, keeps
// machine_live.open_alerts in sync, and pushes lifecycle events to the WS hub.
package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aliyalinkilic/tolikopa/server/internal/config"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

const (
	evalInterval = 30 * time.Second
	sessionGap   = 5 * time.Minute
	// resolveGrace damps flapping: an open alert is resolved only after its
	// condition has been continuously clear for this long. A machine that keeps
	// going briefly online then offline stays under ONE open alert instead of
	// re-firing a new one (with a new notification) on every cycle.
	resolveGrace = 10 * time.Minute
	kindActive   = "active"
	kindLoggedOn = "logged_on"
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
}

// NewEngine constructs the engine. pub may be nil (no WS push).
func NewEngine(pool *pgxpool.Pool, cfg *config.Config, pub Publisher) *Engine {
	return &Engine{pool: pool, cfg: cfg, pub: pub, lastProcessed: time.Now().Add(-15 * time.Minute), lang: LangTR}
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

	evaluators := map[string]func(context.Context, rule) ([]activeAlert, error){
		"offline":     e.evalOffline,
		"cpu_high":    e.evalCPU,
		"mem_high":    e.evalMem,
		"disk_low":    e.evalDiskLow,
		"smart_fail":  e.evalSmart,
		"crash_storm": e.evalCrashStorm,
	}

	for name, fn := range evaluators {
		r, ok := rules[name]
		var actives []activeAlert
		if ok && r.enabled {
			actives, err = fn(ctx, r)
			if err != nil {
				log.Printf("alerts: rule %s: %v", name, err)
				continue
			}
		}
		if err := e.reconcile(ctx, name, actives); err != nil {
			log.Printf("alerts: reconcile %s: %v", name, err)
		}
	}
	return nil
}

// reconcile upserts the active alerts and resolves any open alert of this rule
// whose condition no longer holds.
func (e *Engine) reconcile(ctx context.Context, ruleName string, actives []activeAlert) error {
	keys := make([]string, 0, len(actives))
	for _, a := range actives {
		keys = append(keys, a.dedupKey)
		if err := e.upsert(ctx, a); err != nil {
			return err
		}
	}
	// Resolve stale alerts — but only once the condition has been clear for
	// resolveGrace (last_seen_at is refreshed each eval the condition holds), so
	// brief online blips don't resolve+reopen an offline alert on a loop.
	rows, err := e.pool.Query(ctx, `
		UPDATE alert SET status='resolved', resolved_at=now()
		WHERE rule=$1 AND status<>'resolved' AND NOT (dedup_key = ANY($2))
		  AND last_seen_at < $3
		RETURNING id`, ruleName, keys, time.Now().Add(-resolveGrace))
	if err != nil {
		return err
	}
	var resolvedIDs []int64
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
		if a.severity == "critical" {
			e.sendCriticalEmail(dto, e.lang)
		}
	}
	return nil
}

// ── Rule evaluators ──────────────────────────────────────────────────────────

func (e *Engine) evalOffline(ctx context.Context, r rule) ([]activeAlert, error) {
	rows, err := e.pool.Query(ctx, `
		SELECT ml.machine_id, m.hostname, EXTRACT(EPOCH FROM (now()-ml.ts))::float8
		FROM machine_live ml JOIN machine m ON m.machine_id=ml.machine_id
		WHERE m.archived_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []activeAlert
	for rows.Next() {
		var mid, host string
		var age float64
		if err := rows.Scan(&mid, &host, &age); err != nil {
			return nil, err
		}
		sev, thr := sevHigh(age, r.warn, r.crit)
		if sev == "" {
			continue
		}
		v := age
		out = append(out, activeAlert{
			machineID: mid, hostname: host, rule: "offline", dedupKey: "offline:" + mid,
			severity: sev, value: &v,
			detail: Params{"age_seconds": int(age), "minutes": int(age / 60), "threshold": thr, "unit": "s"},
		})
	}
	return out, rows.Err()
}

func (e *Engine) evalCPU(ctx context.Context, r rule) ([]activeAlert, error) {
	return e.evalSustained(ctx, r, "cpu_high", "avg(ms.cpu_avg)")
}

func (e *Engine) evalMem(ctx context.Context, r rule) ([]activeAlert, error) {
	return e.evalSustained(ctx, r, "mem_high", "avg(ms.mem_ratio_avg)*100")
}

// evalSustained evaluates a metric_5m-based threshold over the rule's for_seconds
// window (real-time aggregation keeps the live edge fresh).
func (e *Engine) evalSustained(ctx context.Context, r rule, ruleName, expr string) ([]activeAlert, error) {
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
		return nil, err
	}
	defer rows.Close()
	var out []activeAlert
	for rows.Next() {
		var mid, host string
		var v float64
		var cnt int
		if err := rows.Scan(&mid, &host, &v, &cnt); err != nil {
			return nil, err
		}
		if cnt < 1 {
			continue
		}
		sev, thr := sevHigh(v, r.warn, r.crit)
		if sev == "" {
			continue
		}
		val := v
		out = append(out, activeAlert{
			machineID: mid, hostname: host, rule: ruleName, dedupKey: ruleName + ":" + mid,
			severity: sev, value: &val,
			detail: Params{"value": v, "threshold": thr, "unit": "%", "window_seconds": int(win)},
		})
	}
	return out, rows.Err()
}

func (e *Engine) evalDiskLow(ctx context.Context, r rule) ([]activeAlert, error) {
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
		return nil, err
	}
	defer rows.Close()
	var out []activeAlert
	for rows.Next() {
		var mid, host, mount string
		var free *float64
		if err := rows.Scan(&mid, &host, &mount, &free); err != nil {
			return nil, err
		}
		if free == nil {
			continue
		}
		sev, thr := sevLow(*free, r.warn, r.crit)
		if sev == "" {
			continue
		}
		v := *free
		out = append(out, activeAlert{
			machineID: mid, hostname: host, rule: "disk_low",
			dedupKey: "disk_low:" + mid + ":" + mount, severity: sev, value: &v,
			detail: Params{"mount": mount, "free_pct": *free, "threshold": thr, "unit": "%"},
		})
	}
	return out, rows.Err()
}

func (e *Engine) evalSmart(ctx context.Context, r rule) ([]activeAlert, error) {
	rows, err := e.pool.Query(ctx, `
		SELECT dh.machine_id, m.hostname, dh.disk, dh.smart_ok, dh.predict_fail
		FROM disk_health dh JOIN machine m ON m.machine_id=dh.machine_id
		WHERE (dh.predict_fail = true OR dh.smart_ok = false) AND m.archived_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []activeAlert
	for rows.Next() {
		var mid, host, disk string
		var smartOK, predictFail *bool
		if err := rows.Scan(&mid, &host, &disk, &smartOK, &predictFail); err != nil {
			return nil, err
		}
		out = append(out, activeAlert{
			machineID: mid, hostname: host, rule: "smart_fail",
			dedupKey: "smart_fail:" + mid + ":" + disk, severity: "critical",
			detail: Params{"disk": disk, "smart_ok": smartOK, "predict_fail": predictFail},
		})
	}
	return out, rows.Err()
}

func (e *Engine) evalCrashStorm(ctx context.Context, r rule) ([]activeAlert, error) {
	win := 600.0
	if r.params != nil {
		if w, ok := r.params["window_seconds"].(float64); ok && w > 0 {
			win = w
		}
	}
	rows, err := e.pool.Query(ctx, `
		SELECT e.machine_id, m.hostname, count(*)::float8
		FROM event e JOIN machine m ON m.machine_id=e.machine_id
		WHERE e.kind IN ('app_crash','app_hang') AND e.ts >= now() - make_interval(secs => $1) AND m.archived_at IS NULL
		GROUP BY e.machine_id, m.hostname`, win)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []activeAlert
	for rows.Next() {
		var mid, host string
		var cnt float64
		if err := rows.Scan(&mid, &host, &cnt); err != nil {
			return nil, err
		}
		sev, thr := sevHigh(cnt, r.warn, r.crit)
		if sev == "" {
			continue
		}
		v := cnt
		out = append(out, activeAlert{
			machineID: mid, hostname: host, rule: "crash_storm", dedupKey: "crash_storm:" + mid,
			severity: sev, value: &v,
			detail: Params{"count": int(cnt), "window_seconds": int(win), "window_minutes": int(win / 60), "threshold": thr},
		})
	}
	return out, rows.Err()
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
