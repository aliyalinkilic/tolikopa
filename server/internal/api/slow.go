package api

import (
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// Slowness scoring weights. The score is additive (unlike the fleet health score,
// which subtracts from 100) because "how slow does this machine feel" is a sum of
// independent burdens. Each factor is capped so no single signal can dominate.
const (
	wMemPressure = 30 // sustained RAM pressure — the strongest felt-slowness signal
	wLowRAM      = 20 // 8 GB is the practical Windows 11 floor
	wDiskFull    = 20 // a full disk means paging to what little SSD space is left
	wWeakCPU     = 12 // core count is all we collect; a coarse but real signal
	wSMART       = 25 // a dying disk feels slow long before it fails
	wCrashes     = 15
	wLongUptime  = 10

	slowScoreWarn = 25
	slowScoreCrit = 50
)

// SlowMachines handles GET /api/stats/slow-machines?days=7&limit=20&min_score=0 —
// the "slow machines" diagnostic report. It ranks machines by a slowness score
// derived entirely from telemetry the agent already collects; nothing here needs a
// new collector or any endpoint actuation.
//
// Averages come from metric_1h, which is materialized-only with a 1 h end_offset
// (0003_caggs.sql) and can therefore lag ~2 h. That is immaterial at a 7-day
// horizon; a short-window mode would need metric_5m instead (real-time enabled in
// 0010_usage_stats.sql).
//
// Disk uses the latest sample per mount inside a bounded 12 h window and then the
// fullest mount — machine_live.disk_min_free_pct is COALESCE-preserved at ingest
// and can be stale, and it carries no mount name.
//
// Only machines seen inside the window are ranked. A machine that last reported
// weeks ago has no history to judge and would otherwise be scored on stale facts.
func (a *API) SlowMachines(c echo.Context) error {
	ctx := c.Request().Context()
	now := time.Now()

	days := queryInt(c, "days", 7)
	if days < 1 {
		days = 1
	} else if days > 30 {
		days = 30
	}
	limit := queryInt(c, "limit", 20)
	if limit < 1 {
		limit = 1
	} else if limit > 200 {
		limit = 200
	}
	minScore := queryInt(c, "min_score", 0)

	rows, err := a.pool.Query(ctx, `
		WITH hist AS (
			SELECT machine_id,
			       avg(mem_ratio_avg)::float8 AS mem_avg,
			       max(mem_ratio_max)::float8 AS mem_max,
			       avg(cpu_avg)::float8       AS cpu_avg,
			       count(*)::float8           AS hours
			FROM metric_1h
			WHERE bucket >= now() - make_interval(days => $1)
			GROUP BY machine_id
		), disk_latest AS (
			SELECT DISTINCT ON (machine_id, mount) machine_id, mount, used_bytes, total_bytes
			FROM disk_usage_sample
			WHERE ts > now() - interval '12 hours' AND total_bytes > 0
			ORDER BY machine_id, mount, ts DESC
		), disk AS (
			SELECT DISTINCT ON (machine_id) machine_id, mount,
			       used_bytes::float8 / total_bytes * 100 AS used_pct,
			       total_bytes - used_bytes               AS free_bytes
			FROM disk_latest
			ORDER BY machine_id, used_bytes::float8 / total_bytes DESC
		), crash AS (
			SELECT machine_id, count(*)::int AS n
			FROM event
			WHERE ts >= now() - make_interval(days => $1)
			  AND kind IN ('app_crash', 'app_hang')
			GROUP BY machine_id
		), smart AS (
			SELECT DISTINCT machine_id FROM disk_health
			WHERE smart_ok = false OR predict_fail = true
		)
		SELECT m.machine_id, m.hostname, ml.logon_user, ml.ts,
		       ml.mem_total, ml.boot_time, m.cpu_cores, m.cpu_model,
		       h.mem_avg, h.mem_max, h.cpu_avg, COALESCE(h.hours, 0),
		       d.mount, d.used_pct, d.free_bytes,
		       COALESCE(c.n, 0), (s.machine_id IS NOT NULL)
		FROM machine m
		JOIN machine_live ml ON ml.machine_id = m.machine_id
		LEFT JOIN hist  h ON h.machine_id = m.machine_id
		LEFT JOIN disk  d ON d.machine_id = m.machine_id
		LEFT JOIN crash c ON c.machine_id = m.machine_id
		LEFT JOIN smart s ON s.machine_id = m.machine_id
		WHERE ml.ts >= now() - make_interval(days => $1) AND m.archived_at IS NULL`, days)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()

	warn := a.cfg.OfflineWarn()
	resp := model.SlowMachinesResponse{
		Days:        days,
		GeneratedAt: now.UTC(),
		Machines:    []model.SlowMachine{},
	}

	for rows.Next() {
		var (
			sm                          model.SlowMachine
			lastSeen                    time.Time
			user, mount, cpuModel       *string
			memTotal, freeBytes         *int64
			bootTime                    *time.Time
			cores                       *int
			memAvg, memMax, cpuAvg      *float64
			usedPct                     *float64
			hours                       float64
		)
		if err := rows.Scan(&sm.MachineID, &sm.Hostname, &user, &lastSeen,
			&memTotal, &bootTime, &cores, &cpuModel,
			&memAvg, &memMax, &cpuAvg, &hours,
			&mount, &usedPct, &freeBytes,
			&sm.Crashes, &sm.SMARTBad); err != nil {
			return serverError(err)
		}

		sm.LogonUser = sv(user)
		sm.LastSeen = lastSeen.UTC()
		sm.Online = now.Sub(lastSeen) < warn
		sm.CPUModel = sv(cpuModel)
		sm.DiskMount = sv(mount)
		sm.MemRatioAvg = fv(memAvg)
		sm.MemRatioMax = fv(memMax)
		sm.CPUAvg = fv(cpuAvg)
		sm.DiskUsedPct = fv(usedPct)
		sm.SampleHours = hours
		if memTotal != nil {
			sm.MemTotal = *memTotal
		}
		if freeBytes != nil {
			sm.DiskFreeBytes = *freeBytes
		}
		if cores != nil {
			sm.CPUCores = *cores
		}
		// Uptime is measured against last_seen, not now(): a machine that has been
		// switched off for weeks would otherwise keep accruing "days since boot"
		// and score as if it had never been restarted.
		if bootTime != nil && !bootTime.IsZero() && lastSeen.After(*bootTime) {
			sm.UptimeDays = lastSeen.Sub(*bootTime).Hours() / 24
		}

		scoreSlowMachine(&sm)
		if sm.Score >= minScore {
			resp.Machines = append(resp.Machines, sm)
		}
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}

	// Worst first; hostname breaks ties so the order is stable across refreshes.
	sort.Slice(resp.Machines, func(i, j int) bool {
		if resp.Machines[i].Score != resp.Machines[j].Score {
			return resp.Machines[i].Score > resp.Machines[j].Score
		}
		return resp.Machines[i].Hostname < resp.Machines[j].Hostname
	})
	if len(resp.Machines) > limit {
		resp.Machines = resp.Machines[:limit]
	}

	return c.JSON(http.StatusOK, resp)
}

// scoreSlowMachine fills Reasons, Score and Severity from the measurements already
// on sm. Factors whose input was never reported contribute nothing rather than
// scoring as zero — an unmeasured machine must not rank as healthy.
func scoreSlowMachine(sm *model.SlowMachine) {
	sm.Reasons = []model.SlowReason{}
	total := 0
	add := func(key string, points int, value float64) {
		if points <= 0 {
			return
		}
		sm.Reasons = append(sm.Reasons, model.SlowReason{Key: key, Points: points, Value: value})
		total += points
	}

	// Sustained memory pressure. Ramps from 70% (nothing) to 95% (full weight);
	// requires at least 3 h of history so a freshly-seen machine isn't condemned.
	if sm.SampleHours >= 3 && sm.MemRatioAvg > 0 {
		add("mem_pressure", scaled(sm.MemRatioAvg, 0.70, 0.95, wMemPressure), sm.MemRatioAvg)
	}

	// Total RAM. 8 GB is where Windows 11 plus a WebView2-backed inbox app starts
	// paging; below 4 GB the machine is simply undersized for the OS.
	if sm.MemTotal > 0 {
		gb := float64(sm.MemTotal) / (1 << 30)
		switch {
		case gb < 4.5:
			add("low_ram", wLowRAM, gb)
		case gb < 8.5:
			add("low_ram", wLowRAM*3/5, gb)
		case gb < 12:
			add("low_ram", wLowRAM/4, gb)
		}
	}

	// Disk fill. Ramps from 85% to 98%: below that there is room to page.
	if sm.DiskUsedPct > 0 {
		add("disk_full", scaled(sm.DiskUsedPct, 85, 98, wDiskFull), sm.DiskUsedPct)
	}

	// Core count. cpu_cores is 0 when an older agent never reported it.
	if sm.CPUCores > 0 {
		switch {
		case sm.CPUCores <= 2:
			add("weak_cpu", wWeakCPU, float64(sm.CPUCores))
		case sm.CPUCores <= 4:
			add("weak_cpu", wWeakCPU*5/12, float64(sm.CPUCores))
		}
	}

	if sm.SMARTBad {
		add("smart", wSMART, 1)
	}
	if sm.Crashes > 0 {
		add("crashes", min(wCrashes, sm.Crashes*3), float64(sm.Crashes))
	}

	// Uptime. A machine that has not rebooted in weeks accumulates leaked memory
	// and pending-update state; this is the cheapest thing an admin can fix.
	switch {
	case sm.UptimeDays >= 30:
		add("long_uptime", wLongUptime, sm.UptimeDays)
	case sm.UptimeDays >= 14:
		add("long_uptime", wLongUptime*3/5, sm.UptimeDays)
	case sm.UptimeDays >= 7:
		add("long_uptime", wLongUptime*3/10, sm.UptimeDays)
	}

	// Reasons descend by weight so the UI can show "the top two" and be right.
	sort.SliceStable(sm.Reasons, func(i, j int) bool {
		return sm.Reasons[i].Points > sm.Reasons[j].Points
	})

	sm.Score = min(100, total)
	switch {
	case sm.Score >= slowScoreCrit:
		sm.Severity = "crit"
	case sm.Score >= slowScoreWarn:
		sm.Severity = "warn"
	default:
		sm.Severity = "ok"
	}
}

// scaled linearly maps v from lo..hi onto 0..weight, clamped at both ends.
func scaled(v, lo, hi float64, weight int) int {
	if v <= lo || hi <= lo {
		return 0
	}
	if v >= hi {
		return weight
	}
	return int(math.Round(float64(weight) * (v - lo) / (hi - lo)))
}
