package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// MachineDetail handles GET /api/machines/{id}.
func (a *API) MachineDetail(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	var (
		d         model.MachineDetail
		domain    *string
		os, osB   *string
		agentVer  *string
		labels    []byte
		ts        *time.Time
		cpu       *float64
		memUsed   *int64
		memTotal  *int64
		diskFree  *float64
		logonUser *string
		sessState *string
		openAlert *int
		cpuModel  *string
		cpuCores  *int
		pendUpd   *int
		upSearch  *time.Time
		upInstall *time.Time
		upChecked *time.Time
		bat       model.BatteryCols
		lastIP    *string
		archBy    *string
	)
	err := a.pool.QueryRow(ctx, `
		SELECT m.machine_id, m.hostname, m.kind, m.domain, m.os, m.os_build, m.first_seen,
		       m.agent_version, m.labels, m.tags, m.cpu_model, m.cpu_cores,
		       m.ip_addrs, host(m.last_ip), m.ip_updated_at,
		       m.pending_updates, m.reboot_pending, m.updates_last_search, m.updates_last_install, m.updates_checked_at,
		       m.battery_present, m.battery_percent, m.battery_ac, m.battery_charging,
		       m.battery_health_pct, m.battery_design_mwh, m.battery_full_mwh, m.battery_cycles, m.battery_updated_at,
		       ml.ts, ml.cpu_pct, ml.mem_used, ml.mem_total, ml.disk_min_free_pct,
		       ml.logon_user, ml.session_state, ml.open_alerts, m.archived_at, m.archived_by
		FROM machine m LEFT JOIN machine_live ml ON ml.machine_id = m.machine_id
		WHERE m.machine_id = $1`, id).
		Scan(&d.MachineID, &d.Hostname, &d.Kind, &domain, &os, &osB, &d.FirstSeen,
			&agentVer, &labels, &d.Tags, &cpuModel, &cpuCores,
			&d.IPAddrs, &lastIP, &d.IPUpdatedAt,
			&pendUpd, &d.RebootPending, &upSearch, &upInstall, &upChecked,
			&bat.Present, &bat.Percent, &bat.AC, &bat.Charging,
			&bat.Health, &bat.Design, &bat.Full, &bat.Cycles, &bat.UpdatedAt,
			&ts, &cpu, &memUsed, &memTotal, &diskFree, &logonUser, &sessState, &openAlert,
			&d.ArchivedAt, &archBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFoundf("machine not found")
	}
	if err != nil {
		return serverError(err)
	}

	if domain != nil {
		d.Domain = *domain
	}
	if os != nil {
		d.OS = *os
	}
	if osB != nil {
		d.OSBuild = *osB
	}
	if agentVer != nil {
		d.AgentVersion = *agentVer
	}
	if cpuModel != nil {
		d.CPUModel = *cpuModel
	}
	if cpuCores != nil {
		d.CPUCores = *cpuCores
	}
	if lastIP != nil {
		d.LastIP = *lastIP
	}
	if d.IPAddrs == nil {
		d.IPAddrs = []string{}
	}
	d.PendingUpdates = pendUpd
	d.UpdatesLastSearch = upSearch
	d.UpdatesLastInstall = upInstall
	d.UpdatesCheckedAt = upChecked
	d.Battery = bat.DTO()
	d.Labels = jsonbToMap(labels)
	d.ArchivedBy = sv(archBy)
	if d.Tags == nil {
		d.Tags = []string{}
	}
	if ts != nil {
		d.LastSeen = *ts
		d.Online = time.Since(*ts) < a.cfg.OfflineWarn()
	}
	if cpu != nil {
		d.CPUPct = *cpu
	}
	if memUsed != nil {
		d.MemUsed = *memUsed
	}
	if memTotal != nil {
		d.MemTotal = *memTotal
		if *memTotal > 0 && memUsed != nil {
			d.MemRatio = float64(*memUsed) / float64(*memTotal)
		}
	}
	if diskFree != nil {
		d.DiskMinFreePct = *diskFree
	}
	if logonUser != nil {
		d.LogonUser = *logonUser
	}
	if sessState != nil {
		d.SessionState = *sessState
	}
	if openAlert != nil {
		d.OpenAlerts = *openAlert
	}

	// Latest disk usage per mount.
	d.Disks = []model.DiskUsage{}
	drows, err := a.pool.Query(ctx, `
		SELECT DISTINCT ON (mount) mount, used_bytes, total_bytes
		FROM disk_usage_sample WHERE machine_id=$1 AND ts > now() - interval '6 hours'
		ORDER BY mount, ts DESC`, id)
	if err != nil {
		return serverError(err)
	}
	for drows.Next() {
		var du model.DiskUsage
		if err := drows.Scan(&du.Mount, &du.Used, &du.Total); err != nil {
			drows.Close()
			return serverError(err)
		}
		if du.Total > 0 {
			du.FreePct = float64(du.Total-du.Used) / float64(du.Total) * 100
		}
		d.Disks = append(d.Disks, du)
	}
	drows.Close()
	if err := drows.Err(); err != nil {
		return serverError(err)
	}

	// Disk health.
	d.DiskHealth = []model.DiskHealthDTO{}
	hrows, err := a.pool.Query(ctx,
		`SELECT disk, smart_ok, predict_fail, updated_at FROM disk_health WHERE machine_id=$1 ORDER BY disk`, id)
	if err != nil {
		return serverError(err)
	}
	for hrows.Next() {
		var dh model.DiskHealthDTO
		var smart, predict *bool
		if err := hrows.Scan(&dh.Disk, &smart, &predict, &dh.UpdatedAt); err != nil {
			hrows.Close()
			return serverError(err)
		}
		if smart != nil {
			dh.SMARTOk = *smart
		}
		if predict != nil {
			dh.PredictFail = *predict
		}
		d.DiskHealth = append(d.DiskHealth, dh)
	}
	hrows.Close()
	if err := hrows.Err(); err != nil {
		return serverError(err)
	}

	return c.JSON(http.StatusOK, d)
}

// MachineMetrics handles GET /api/machines/{id}/metrics.
func (a *API) MachineMetrics(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	from, to := parseRange(c)
	res := pickResolution(c.QueryParam("res"), from, to)

	want := func(name string) bool {
		m := c.QueryParam("metrics")
		if m == "" {
			return true
		}
		for _, part := range strings.Split(m, ",") {
			if strings.TrimSpace(part) == name {
				return true
			}
		}
		return false
	}

	series := model.MetricSeries{Resolution: res, CPU: []model.SeriesPoint{}, Mem: []model.SeriesPoint{}, Net: []model.NetPoint{}}

	if res == "raw" {
		rows, err := a.pool.Query(ctx, `
			SELECT ts, cpu_pct, mem_used, mem_total, net_sent_delta, net_recv_delta
			FROM metric_sample WHERE machine_id=$1 AND ts BETWEEN $2 AND $3 ORDER BY ts`, id, from, to)
		if err != nil {
			return serverError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var t time.Time
			var cpu *float64
			var mu, mt, sent, recv *int64
			if err := rows.Scan(&t, &cpu, &mu, &mt, &sent, &recv); err != nil {
				return serverError(err)
			}
			if want("cpu") {
				series.CPU = append(series.CPU, model.SeriesPoint{T: t, Avg: fv(cpu), Max: fv(cpu)})
			}
			if want("mem") {
				r := memRatio(mu, mt)
				series.Mem = append(series.Mem, model.SeriesPoint{T: t, Avg: r, Max: r})
			}
			if want("net") {
				series.Net = append(series.Net, model.NetPoint{T: t, Sent: iv(sent), Recv: iv(recv)})
			}
		}
		if err := rows.Err(); err != nil {
			return serverError(err)
		}
		return c.JSON(http.StatusOK, series)
	}

	table := "metric_5m"
	if res == "1h" {
		table = "metric_1h"
	}
	rows, err := a.pool.Query(ctx, `
		SELECT bucket, cpu_avg, cpu_max, mem_ratio_avg, mem_ratio_max, sent_bytes, recv_bytes
		FROM `+table+` WHERE machine_id=$1 AND bucket BETWEEN $2 AND $3 ORDER BY bucket`, id, from, to)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var t time.Time
		var cAvg, cMax, mAvg, mMax *float64
		var sent, recv *int64
		if err := rows.Scan(&t, &cAvg, &cMax, &mAvg, &mMax, &sent, &recv); err != nil {
			return serverError(err)
		}
		if want("cpu") {
			series.CPU = append(series.CPU, model.SeriesPoint{T: t, Avg: fv(cAvg), Max: fv(cMax)})
		}
		if want("mem") {
			series.Mem = append(series.Mem, model.SeriesPoint{T: t, Avg: fv(mAvg), Max: fv(mMax)})
		}
		if want("net") {
			series.Net = append(series.Net, model.NetPoint{T: t, Sent: iv(sent), Recv: iv(recv)})
		}
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, series)
}

// MachineDisks handles GET /api/machines/{id}/disks.
func (a *API) MachineDisks(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	from, to := parseRange(c)

	rows, err := a.pool.Query(ctx, `
		SELECT time_bucket('5 minutes', ts) AS b, mount, max(used_bytes), max(total_bytes)
		FROM disk_usage_sample WHERE machine_id=$1 AND ts BETWEEN $2 AND $3
		GROUP BY b, mount ORDER BY mount, b`, id, from, to)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()

	order := []string{}
	byMount := map[string]*model.DiskSeries{}
	for rows.Next() {
		var t time.Time
		var mount string
		var used, total *int64
		if err := rows.Scan(&t, &mount, &used, &total); err != nil {
			return serverError(err)
		}
		s, ok := byMount[mount]
		if !ok {
			s = &model.DiskSeries{Mount: mount, Points: []model.DiskPoint{}}
			byMount[mount] = s
			order = append(order, mount)
		}
		s.Points = append(s.Points, model.DiskPoint{T: t, Used: iv(used), Total: iv(total)})
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	out := make([]model.DiskSeries, 0, len(order))
	for _, m := range order {
		out = append(out, *byMount[m])
	}
	return c.JSON(http.StatusOK, out)
}

// MachineTopProcesses handles GET /api/machines/{id}/top-processes.
func (a *API) MachineTopProcesses(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	from, to := parseRange(c)
	limit := queryInt(c, "limit", 10)
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	orderExpr := "c"
	if c.QueryParam("by") == "mem" {
		orderExpr = "m"
	}

	rows, err := a.pool.Query(ctx, `
		SELECT process_name, category_id, avg(cpu_pct)::float8 AS c, avg(mem_bytes)::float8 AS m
		FROM process_sample WHERE machine_id=$1 AND ts BETWEEN $2 AND $3
		GROUP BY process_name, category_id ORDER BY `+orderExpr+` DESC NULLS LAST LIMIT $4`,
		id, from, to, limit)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()

	out := []model.TopProcess{}
	for rows.Next() {
		var name string
		var catID *int16
		var cpu, mem *float64
		if err := rows.Scan(&name, &catID, &cpu, &mem); err != nil {
			return serverError(err)
		}
		tp := model.TopProcess{Name: name, CPUPct: fv(cpu), MemBytes: int64(fv(mem))}
		if catID != nil {
			tp.Category = a.cats.Name(*catID)
		}
		out = append(out, tp)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// MachineCategories handles GET /api/machines/{id}/categories (proc_cat_1h).
func (a *API) MachineCategories(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	from, to := parseRange(c)

	rows, err := a.pool.Query(ctx, `
		SELECT category_id, sum(cpu_sum)::float8, sum(samples)::bigint
		FROM proc_cat_1h WHERE machine_id=$1 AND bucket BETWEEN $2 AND $3
		GROUP BY category_id`, id, from, to)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out := []model.CategoryShare{}
	for rows.Next() {
		var catID *int16
		var cs *float64
		var samples int64
		if err := rows.Scan(&catID, &cs, &samples); err != nil {
			return serverError(err)
		}
		out = append(out, model.CategoryShare{Category: a.catName(catID), CPUSum: fv(cs), Samples: samples})
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	// Merge same-name categories (many ids resolve to "system"), drop the system
	// slice, and compute shares over the remaining total.
	return c.JSON(http.StatusOK, mergeCategories(out))
}

// MachineSessions handles GET /api/machines/{id}/sessions (usage_session).
func (a *API) MachineSessions(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	from, to := parseRange(c)

	rows, err := a.pool.Query(ctx, `
		SELECT us.machine_id, m.hostname, us.kind, us.started_at, us.ended_at
		FROM usage_session us JOIN machine m ON m.machine_id = us.machine_id
		WHERE us.machine_id=$1 AND us.started_at < $3 AND (us.ended_at IS NULL OR us.ended_at > $2)
		ORDER BY us.started_at DESC`, id, from, to)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out, err := scanSessions(rows)
	if err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// MachineEvents handles GET /api/machines/{id}/events.
func (a *API) MachineEvents(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	from, to := parseRange(c)
	kind := c.QueryParam("kind")

	sql := `SELECT ts, machine_id, kind, process_name, severity, detail
		FROM event WHERE machine_id=$1 AND ts BETWEEN $2 AND $3`
	args := []any{id, from, to}
	if kind != "" {
		args = append(args, kind)
		sql += " AND kind=$4"
	}
	sql += " ORDER BY ts DESC LIMIT 500"

	rows, err := a.pool.Query(ctx, sql, args...)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out := []model.EventDTO{}
	for rows.Next() {
		var e model.EventDTO
		var pname, sev *string
		var detail []byte
		if err := rows.Scan(&e.TS, &e.MachineID, &e.Kind, &pname, &sev, &detail); err != nil {
			return serverError(err)
		}
		if pname != nil {
			e.ProcessName = *pname
		}
		if sev != nil {
			e.Severity = *sev
		}
		e.Detail = jsonbToMap(detail)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// MachineAlerts handles GET /api/machines/{id}/alerts.
func (a *API) MachineAlerts(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	from, to := parseRange(c)
	sql := alertSelect + ` WHERE a.machine_id=$1 AND a.opened_at BETWEEN $2 AND $3`
	args := []any{id, from, to}
	if status := c.QueryParam("status"); status != "" {
		args = append(args, status)
		sql += " AND a.status=$4"
	}
	sql += " ORDER BY a.opened_at DESC LIMIT 500"
	rows, err := a.pool.Query(ctx, sql, args...)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out, err := scanAlerts(rows)
	if err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// ── scan helpers ─────────────────────────────────────────────────────────────

func (a *API) catName(id *int16) string {
	if id == nil {
		return "bilinmeyen"
	}
	if n := a.cats.Name(*id); n != "" {
		return n
	}
	return "bilinmeyen"
}

func scanSessions(rows pgx.Rows) ([]model.SessionItem, error) {
	out := []model.SessionItem{}
	for rows.Next() {
		var s model.SessionItem
		var ended *time.Time
		if err := rows.Scan(&s.MachineID, &s.Hostname, &s.Kind, &s.StartedAt, &ended); err != nil {
			return nil, err
		}
		s.EndedAt = ended
		out = append(out, s)
	}
	return out, rows.Err()
}

func fv(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func iv(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func memRatio(used, total *int64) float64 {
	if used == nil || total == nil || *total == 0 {
		return 0
	}
	return float64(*used) / float64(*total)
}
