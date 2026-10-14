package api

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

const fleetSelect = `
	SELECT m.machine_id, m.hostname, m.kind, m.domain,
	       ml.ts, ml.cpu_pct, ml.mem_used, ml.mem_total, ml.disk_min_free_pct,
	       ml.logon_user, ml.session_state, ml.open_alerts, m.agent_version, m.labels, m.tags,
	       m.pending_updates, m.reboot_pending, m.updates_checked_at,
	       m.battery_present, m.battery_percent, m.battery_ac, m.battery_charging,
	       m.battery_health_pct, m.battery_design_mwh, m.battery_full_mwh, m.battery_cycles, m.battery_updated_at
	FROM machine m JOIN machine_live ml ON ml.machine_id = m.machine_id`

// Fleet handles GET /api/fleet (served only from machine_live).
func (a *API) Fleet(c echo.Context) error {
	sql := fleetSelect + ` WHERE m.archived_at IS NULL`
	args := []any{}
	if user := c.QueryParam("user"); user != "" {
		args = append(args, user)
		sql += " AND ml.logon_user = $1"
	}
	if q := c.QueryParam("q"); q != "" {
		args = append(args, "%"+q+"%")
		sql += " AND (m.hostname ILIKE $" + itoa(len(args)) + " OR m.machine_id ILIKE $" + itoa(len(args)) + ")"
	}
	if tag := c.QueryParam("tag"); tag != "" {
		args = append(args, tag)
		sql += " AND $" + itoa(len(args)) + " = ANY(m.tags)"
	}
	sql += " ORDER BY m.hostname"

	rows, err := a.pool.Query(c.Request().Context(), sql, args...)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()

	status := c.QueryParam("status")
	now := time.Now()
	warn := a.cfg.OfflineWarn()
	out := []model.FleetMachine{}
	for rows.Next() {
		fm, err := scanFleetMachine(rows, now, warn)
		if err != nil {
			return serverError(err)
		}
		if status == "online" && !fm.Online {
			continue
		}
		if status == "offline" && fm.Online {
			continue
		}
		out = append(out, fm)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// FleetSummary handles GET /api/fleet/summary.
func (a *API) FleetSummary(c echo.Context) error {
	ctx := c.Request().Context()
	rows, err := a.pool.Query(ctx, fleetSelect+` WHERE m.archived_at IS NULL`)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()

	now := time.Now()
	warn := a.cfg.OfflineWarn()
	sum := model.FleetSummary{AlertsBySeverity: map[string]int{}}
	var cpuSum, memSum float64
	var onlineCount int
	for rows.Next() {
		fm, err := scanFleetMachine(rows, now, warn)
		if err != nil {
			return serverError(err)
		}
		sum.Total++
		if fm.Online {
			sum.Online++
			cpuSum += fm.CPUPct
			memSum += fm.MemRatio
			onlineCount++
			// "idle" (logged on + unlocked, no input >= 5 min) is its own state:
			// it never counts as active. A Linux host is a headless server whose
			// "active" only means an SSH/logind session exists, not that a person
			// is using a PC, so it is not counted as in use.
			switch {
			case fm.Kind == "linux":
			case fm.SessionState == "active":
				sum.Active++
			case fm.SessionState == "idle":
				sum.Idle++
			}
		} else {
			sum.Offline++
		}
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	if onlineCount > 0 {
		sum.AvgCPU = cpuSum / float64(onlineCount)
		// MemRatio is 0..1; expose AvgMem as a percent (0..100) like AvgCPU so the
		// frontend's percent formatter renders it correctly.
		sum.AvgMem = memSum / float64(onlineCount) * 100
	}

	// Open-alert distribution by severity.
	arows, err := a.pool.Query(ctx,
		`SELECT severity, count(*) FROM alert WHERE status <> 'resolved' GROUP BY severity`)
	if err != nil {
		return serverError(err)
	}
	defer arows.Close()
	for arows.Next() {
		var sev string
		var n int
		if err := arows.Scan(&sev, &n); err != nil {
			return serverError(err)
		}
		sum.AlertsBySeverity[sev] = n
	}
	if err := arows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, sum)
}

// scanFleetMachine scans one machine_live+machine row (column order per fleetSelect).
func scanFleetMachine(rows interface {
	Scan(dest ...any) error
}, now time.Time, warn time.Duration) (model.FleetMachine, error) {
	var (
		fm        model.FleetMachine
		domain    *string
		ts        time.Time
		cpu       *float64
		memUsed   *int64
		memTotal  *int64
		diskFree  *float64
		logonUser *string
		sessState *string
		agentVer  *string
		labels    []byte
		pendUpd   *int
		upChecked *time.Time
		bat       model.BatteryCols
	)
	if err := rows.Scan(&fm.MachineID, &fm.Hostname, &fm.Kind, &domain,
		&ts, &cpu, &memUsed, &memTotal, &diskFree,
		&logonUser, &sessState, &fm.OpenAlerts, &agentVer, &labels, &fm.Tags,
		&pendUpd, &fm.RebootPending, &upChecked,
		&bat.Present, &bat.Percent, &bat.AC, &bat.Charging,
		&bat.Health, &bat.Design, &bat.Full, &bat.Cycles, &bat.UpdatedAt); err != nil {
		return fm, err
	}
	fm.PendingUpdates = pendUpd
	fm.UpdatesCheckedAt = upChecked
	fm.Battery = bat.DTO()
	fm.LastSeen = ts
	fm.Online = now.Sub(ts) < warn
	if domain != nil {
		fm.Domain = *domain
	}
	if cpu != nil {
		fm.CPUPct = *cpu
	}
	if memUsed != nil && memTotal != nil && *memTotal > 0 {
		fm.MemRatio = float64(*memUsed) / float64(*memTotal)
	}
	if diskFree != nil {
		fm.DiskMinFreePct = *diskFree
	}
	if logonUser != nil {
		fm.LogonUser = *logonUser
	}
	if sessState != nil {
		fm.SessionState = *sessState
	}
	if agentVer != nil {
		fm.AgentVersion = *agentVer
	}
	fm.Labels = jsonbToMap(labels)
	if fm.Tags == nil {
		fm.Tags = []string{}
	}
	return fm, nil
}
