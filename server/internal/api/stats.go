package api

import (
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// overviewLimit caps the top/critical lists on the dashboard.
const overviewLimit = 8

// FleetOverview handles GET /api/fleet/overview — the one-shot homepage
// dashboard payload. Live data comes exclusively from machine_live (plus the
// machine inventory row); the 24 h history comes from metric_5m; the critical
// per-mount disk list uses the latest disk_usage_sample row per mount within a
// bounded 6 h window (machine_live only stores the worst mount's free %).
func (a *API) FleetOverview(c echo.Context) error {
	ctx := c.Request().Context()
	now := time.Now()
	warn := a.cfg.OfflineWarn()

	ov := model.FleetOverview{
		GeneratedAt:  now.UTC(),
		Health:       model.HealthSummary{Issues: []model.HealthIssue{}},
		History:      []model.FleetHistoryPoint{},
		TopCPU:       []model.TopCPUMachine{},
		CriticalMem:  []model.CriticalMemMachine{},
		CriticalDisk: []model.CriticalDiskMount{},
		RecentEvents: []model.OverviewEvent{},
		RecentAlerts: []model.AlertDTO{},
	}

	// ── 1) Single pass over the hot machine_live table ─────────────────────────
	type liveRow struct {
		id, host, user     string
		cpu, memRatio      float64
		memUsed, memTotal  int64
	}
	var (
		online                                []liveRow
		cpuSum, memSum                        float64
		highCPU, highMem, lowDisk, rebootPend int
	)
	rows, err := a.pool.Query(ctx, `
		SELECT m.machine_id, m.hostname, ml.ts, ml.cpu_pct, ml.mem_used, ml.mem_total,
		       ml.disk_min_free_pct, ml.logon_user,
		       -- A Linux host is a headless server: its "active" only means a
		       -- logind/SSH session exists, so it never counts as a PC in use.
		       CASE WHEN m.kind = 'linux' THEN NULL ELSE ml.session_state END,
		       m.reboot_pending
		FROM machine m JOIN machine_live ml ON ml.machine_id = m.machine_id
		WHERE m.archived_at IS NULL`)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, host          string
			ts                time.Time
			cpu, diskFree     *float64
			memUsed, memTotal *int64
			user, state       *string
			reboot            bool
		)
		if err := rows.Scan(&id, &host, &ts, &cpu, &memUsed, &memTotal,
			&diskFree, &user, &state, &reboot); err != nil {
			return serverError(err)
		}
		ov.Totals.Total++
		if reboot {
			rebootPend++
		}
		if now.Sub(ts) >= warn {
			ov.Totals.Offline++
			continue
		}
		ov.Totals.Online++
		switch sv(state) {
		case "active":
			ov.Totals.InUse++
		case "idle":
			ov.Totals.Idle++
		case "locked":
			ov.Totals.Locked++
		}
		r := liveRow{id: id, host: host, user: sv(user), cpu: fv(cpu)}
		if memUsed != nil && memTotal != nil && *memTotal > 0 {
			r.memUsed, r.memTotal = *memUsed, *memTotal
			r.memRatio = float64(*memUsed) / float64(*memTotal)
		}
		cpuSum += r.cpu
		memSum += r.memRatio
		if r.cpu > 90 {
			highCPU++
		}
		if r.memRatio >= 0.90 {
			highMem++
		}
		if diskFree != nil && *diskFree <= 10 { // <=10% free == >=90% used
			lowDisk++
		}
		online = append(online, r)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	if ov.Totals.Online > 0 {
		ov.AvgCPU = cpuSum / float64(ov.Totals.Online) // 0..100
		ov.AvgMem = memSum / float64(ov.Totals.Online) // ratio 0..1
	}

	// top_cpu: online machines by current CPU desc.
	byCPU := append([]liveRow(nil), online...)
	sort.Slice(byCPU, func(i, j int) bool { return byCPU[i].cpu > byCPU[j].cpu })
	for i, r := range byCPU {
		if i >= overviewLimit {
			break
		}
		ov.TopCPU = append(ov.TopCPU, model.TopCPUMachine{
			MachineID: r.id, Hostname: r.host, LogonUser: r.user,
			CPUPct: r.cpu, MemRatio: r.memRatio,
		})
	}

	// critical_mem: online machines with mem_ratio >= 0.90, desc.
	byMem := append([]liveRow(nil), online...)
	sort.Slice(byMem, func(i, j int) bool { return byMem[i].memRatio > byMem[j].memRatio })
	for _, r := range byMem {
		if r.memRatio < 0.90 || len(ov.CriticalMem) >= overviewLimit {
			break
		}
		ov.CriticalMem = append(ov.CriticalMem, model.CriticalMemMachine{
			MachineID: r.id, Hostname: r.host, LogonUser: r.user,
			MemRatio: r.memRatio, MemUsed: r.memUsed, MemTotal: r.memTotal,
		})
	}

	// ── 2) critical_disk: latest usage per mount (bounded 6 h window) ──────────
	drows, err := a.pool.Query(ctx, `
		SELECT d.machine_id, m.hostname, d.mount,
		       d.used_bytes::float8 / d.total_bytes * 100 AS used_pct,
		       d.total_bytes - d.used_bytes               AS free_bytes
		FROM (
			SELECT DISTINCT ON (machine_id, mount) machine_id, mount, used_bytes, total_bytes
			FROM disk_usage_sample
			WHERE ts > now() - interval '6 hours'
			ORDER BY machine_id, mount, ts DESC
		) d
		JOIN machine m ON m.machine_id = d.machine_id
		WHERE d.total_bytes > 0 AND d.used_bytes::float8 / d.total_bytes >= 0.90 AND m.archived_at IS NULL
		ORDER BY used_pct DESC
		LIMIT $1`, overviewLimit)
	if err != nil {
		return serverError(err)
	}
	defer drows.Close()
	for drows.Next() {
		var cd model.CriticalDiskMount
		if err := drows.Scan(&cd.MachineID, &cd.Hostname, &cd.Mount, &cd.UsedPct, &cd.FreeBytes); err != nil {
			return serverError(err)
		}
		ov.CriticalDisk = append(ov.CriticalDisk, cd)
	}
	if err := drows.Err(); err != nil {
		return serverError(err)
	}

	// ── 3) history: last 24 h fleet-wide averages per 5-min bucket ─────────────
	hrows, err := a.pool.Query(ctx, `
		SELECT bucket, avg(cpu_avg)::float8, avg(mem_ratio_avg)::float8
		FROM metric_5m
		WHERE bucket >= now() - interval '24 hours'
		GROUP BY bucket ORDER BY bucket`)
	if err != nil {
		return serverError(err)
	}
	defer hrows.Close()
	for hrows.Next() {
		var p model.FleetHistoryPoint
		var cpuAvg, memAvg *float64
		if err := hrows.Scan(&p.TS, &cpuAvg, &memAvg); err != nil {
			return serverError(err)
		}
		p.CPUAvg = fv(cpuAvg)
		p.MemRatioAvg = fv(memAvg)
		ov.History = append(ov.History, p)
	}
	if err := hrows.Err(); err != nil {
		return serverError(err)
	}

	// ── 4) recent_events: last 24 h, newest first ──────────────────────────────
	erows, err := a.pool.Query(ctx, `
		SELECT e.ts, e.machine_id, COALESCE(m.hostname, ''), e.kind, e.process_name, e.severity
		FROM event e LEFT JOIN machine m ON m.machine_id = e.machine_id
		WHERE e.ts > now() - interval '24 hours' AND m.archived_at IS NULL
		ORDER BY e.ts DESC LIMIT 10`)
	if err != nil {
		return serverError(err)
	}
	defer erows.Close()
	for erows.Next() {
		var ev model.OverviewEvent
		var pname, sev *string
		if err := erows.Scan(&ev.TS, &ev.MachineID, &ev.Hostname, &ev.Kind, &pname, &sev); err != nil {
			return serverError(err)
		}
		ev.ProcessName = sv(pname)
		ev.Severity = sv(sev)
		ov.RecentEvents = append(ov.RecentEvents, ev)
	}
	if err := erows.Err(); err != nil {
		return serverError(err)
	}

	// ── 5) recent_alerts: open (non-resolved) alerts, newest first ─────────────
	arows, err := a.pool.Query(ctx,
		alertSelect+` WHERE a.status <> 'resolved' ORDER BY a.opened_at DESC LIMIT 10`)
	if err != nil {
		return serverError(err)
	}
	defer arows.Close()
	ov.RecentAlerts, err = scanAlerts(arows)
	if err != nil {
		return serverError(err)
	}

	// ── 6) health inputs: SMART failures + open critical alerts ────────────────
	var smartBad, critAlerts int
	if err := a.pool.QueryRow(ctx, `
		SELECT count(DISTINCT machine_id) FROM disk_health
		WHERE smart_ok = false OR predict_fail = true`).Scan(&smartBad); err != nil {
		return serverError(err)
	}
	if err := a.pool.QueryRow(ctx, `
		SELECT count(*) FROM alert
		WHERE status <> 'resolved' AND severity = 'critical'`).Scan(&critAlerts); err != nil {
		return serverError(err)
	}

	// ── 7) health score: 100 minus capped per-issue penalties (docs/API.md §3) ──
	totalPenalty := 0
	addIssue := func(key string, count, penalty int) {
		if count <= 0 {
			return
		}
		if penalty < 1 {
			penalty = 1 // any present issue costs at least one point
		}
		ov.Health.Issues = append(ov.Health.Issues, model.HealthIssue{Key: key, Count: count, Penalty: penalty})
		totalPenalty += penalty
	}
	// share scales weight by the affected fraction of base, capped at weight.
	share := func(count, base, weight int) int {
		if base <= 0 {
			return 0
		}
		return min(weight, int(math.Round(float64(weight)*float64(count)/float64(base))))
	}
	addIssue("offline", ov.Totals.Offline, share(ov.Totals.Offline, ov.Totals.Total, 30))
	addIssue("high_cpu", highCPU, share(highCPU, ov.Totals.Online, 15))
	addIssue("high_mem", highMem, share(highMem, ov.Totals.Online, 15))
	addIssue("low_disk", lowDisk, share(lowDisk, ov.Totals.Total, 20))
	addIssue("smart", smartBad, min(25, smartBad*10))
	addIssue("reboot_pending", rebootPend, min(10, rebootPend*2))
	addIssue("crit_alerts", critAlerts, min(20, critAlerts*5))
	ov.Health.Score = max(0, 100-totalPenalty)

	return c.JSON(http.StatusOK, ov)
}

// StatsHourlyActivity handles GET /api/stats/hourly-activity?days=7 — the
// fleet-wide busy-hours heatmap from session_user_1h (all rows, including the
// '' machine-level user). active_seconds = active_samples × 15 s (nominal
// vitals cadence); dow/hour are Europe/Istanbul local (ISO dow, 1=Mon..7=Sun).
func (a *API) StatsHourlyActivity(c echo.Context) error {
	ctx := c.Request().Context()
	days := queryInt(c, "days", 7)
	if days < 1 {
		days = 1
	} else if days > 30 {
		days = 30
	}

	rows, err := a.pool.Query(ctx, `
		SELECT extract(isodow FROM bucket AT TIME ZONE 'Europe/Istanbul')::int AS dow,
		       extract(hour   FROM bucket AT TIME ZONE 'Europe/Istanbul')::int AS hour,
		       sum(active_samples)::bigint,
		       sum(total_samples)::bigint
		FROM session_user_1h
		WHERE bucket >= now() - make_interval(days => $1)
		  -- PC usage only: a Linux server's logind/SSH session is not a person at a desk.
		  AND machine_id NOT IN (SELECT machine_id FROM machine WHERE kind = 'linux')
		GROUP BY 1, 2 ORDER BY 1, 2`, days)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()

	resp := model.HourlyActivityResponse{Days: days, Cells: []model.HourlyActivityCell{}}
	for rows.Next() {
		var cell model.HourlyActivityCell
		var active, total int64
		if err := rows.Scan(&cell.Dow, &cell.Hour, &active, &total); err != nil {
			return serverError(err)
		}
		cell.ActiveSeconds = active * 15 // nominal vitals cadence (docs/DATABASE.md)
		cell.Samples = total
		resp.Cells = append(resp.Cells, cell)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, resp)
}

// StatsTopApps handles GET /api/stats/top-apps?range=24h&by=cpu — fleet-wide
// application consumption from proc_name_1h, LEFT-joined with the app-focus
// rollup for foreground seconds. Categories are resolved from the same
// in-memory process_category cache ingest uses ("" when unclassified).
// PRIVACY: names are process base names only — never titles, paths or URLs.
func (a *API) StatsTopApps(c echo.Context) error {
	ctx := c.Request().Context()

	interval := "24 hours"
	switch c.QueryParam("range") {
	case "7d":
		interval = "7 days"
	case "30d":
		interval = "30 days"
	}
	orderExpr := "cpu_avg" // by=cpu (default)
	switch c.QueryParam("by") {
	case "mem":
		orderExpr = "mem_avg"
	case "focus":
		orderExpr = "focus_seconds"
	}

	// cpu_avg/mem_avg are sample-weighted means across hour buckets; machines is
	// the peak concurrent distinct-machine count of any single hour (distinct
	// counts cannot be summed across buckets); active_hours counts the distinct
	// hour buckets the app appeared in. Focus seconds come from the per-user
	// app-focus cagg (same normalized names as process_sample).
	rows, err := a.pool.Query(ctx, `
		WITH p AS (
			SELECT process_name,
			       sum(cpu_sum)::float8 / NULLIF(sum(samples), 0)::float8           AS cpu_avg,
			       max(cpu_max)::float8                                             AS cpu_max,
			       sum(mem_avg * samples)::float8 / NULLIF(sum(samples), 0)::float8 AS mem_avg,
			       max(machines)::int                                               AS machines,
			       count(*)::float8                                                 AS active_hours
			FROM proc_name_1h
			WHERE bucket >= now() - $1::interval
			GROUP BY process_name
		), f AS (
			SELECT app_name, sum(seconds)::bigint AS focus_seconds
			FROM app_focus_user_1h
			WHERE bucket >= now() - $1::interval
			GROUP BY app_name
		)
		SELECT p.process_name, p.cpu_avg, p.cpu_max, p.mem_avg, p.machines, p.active_hours,
		       COALESCE(f.focus_seconds, 0) AS focus_seconds
		FROM p LEFT JOIN f ON f.app_name = p.process_name
		ORDER BY `+orderExpr+` DESC NULLS LAST
		LIMIT 15`, interval)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()

	resp := model.TopAppsResponse{Apps: []model.TopApp{}}
	for rows.Next() {
		var app model.TopApp
		var cpuAvg, cpuMax, memAvg *float64
		var machines *int
		if err := rows.Scan(&app.Name, &cpuAvg, &cpuMax, &memAvg, &machines,
			&app.ActiveHours, &app.FocusSeconds); err != nil {
			return serverError(err)
		}
		app.CPUAvg = fv(cpuAvg)
		app.CPUMax = fv(cpuMax)
		app.MemAvg = int64(fv(memAvg))
		if machines != nil {
			app.Machines = *machines
		}
		if _, name, ok := a.cats.Resolve(app.Name); ok {
			app.Category = name
		}
		resp.Apps = append(resp.Apps, app)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, resp)
}

// sv dereferences a nullable text column to "" for NULL.
func sv(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
