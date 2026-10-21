package api

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// Read-only insight views over the hygiene audit trail: which failures are still
// open, and what successful removals did to machine usage. Nothing here writes,
// and nothing here reads beyond the aggregate metrics the privacy allowlist
// already permits (CPU / RAM gauges, process name + cpu + mem).

const (
	impactDefaultDays = 7
	// impactMaxDays is bounded by process_sample retention (21 d): a before-window
	// plus an after-window of 14 d each still lands inside it for recent removals.
	impactMaxDays = 14
	// impactMinHours is how many ACTIVE hours a machine needs on each side before
	// its delta counts in the fleet figure. Below that one busy afternoon decides.
	impactMinHours   = 3
	failureHostnames = 8
)

// failuresCTE classifies every (machine, item) pair that failed at least once in
// the window by its LATEST actuation attempt. dry_run rows are ignored: they
// never actuate, so they neither open nor close a failure.
//
// A pair is open when that latest attempt is itself a failure AND the machine has
// not since reported the item absent. An item a user uninstalled by hand, or one
// a later pass removed, is therefore closed even if the last row says "failed".
const failuresCTE = `
	WITH latest AS (
		SELECT DISTINCT ON (machine_id, kind, item_id)
		       machine_id, kind, item_id, ts, outcome, error_code
		FROM remediation_attempt
		WHERE ts >= $1 AND outcome <> 'dry_run'
		ORDER BY machine_id, kind, item_id, ts DESC
	),
	ever_failed AS (
		SELECT DISTINCT machine_id, kind, item_id
		FROM remediation_attempt
		WHERE ts >= $1 AND outcome IN ('failed', 'timeout', 'blocked', 'unknown_item')
	),
	classified AS (
		SELECT l.*,
		       (l.outcome IN ('failed', 'timeout', 'blocked', 'unknown_item')
		        AND NOT EXISTS (
		            SELECT 1 FROM machine_remediation_state s
		            WHERE s.machine_id = l.machine_id AND s.kind = l.kind
		              AND s.item_id = l.item_id AND NOT s.present AND s.last_seen > l.ts)
		       ) AS open
		FROM latest l
		JOIN ever_failed USING (machine_id, kind, item_id)
		-- Archived machines are retired: their failures are nobody's to-do (0013).
		JOIN machine am ON am.machine_id = l.machine_id AND am.archived_at IS NULL
	)`

// GetRemediationFailures handles GET /api/admin/remediation/failures — open
// failures grouped by (item, outcome, error code), newest-heaviest first.
func (a *API) GetRemediationFailures(c echo.Context) error {
	ctx := c.Request().Context()
	since := time.Now().Add(-attemptsWindow)

	out := model.RemediationFailuresResponse{Open: []model.RemediationFailureGroup{}}
	if err := a.pool.QueryRow(ctx, failuresCTE+`
		SELECT count(*) FILTER (WHERE open)::int, count(*) FILTER (WHERE NOT open)::int
		FROM classified`, since).Scan(&out.OpenMachineItems, &out.ResolvedMachineItems); err != nil {
		return serverError(err)
	}

	// The fleet's highest reported agent version. Compared as an int array so
	// 1.10.0 sorts above 1.9.0; anything that is not a plain dotted version (a dev
	// build tag) is ignored rather than cast. Linux hosts are left out: they run
	// no hygiene and their agent is built and shipped separately, so a newer Linux
	// build must not mark every Windows PC as "behind".
	if err := a.pool.QueryRow(ctx, `
		SELECT COALESCE((
			SELECT agent_version FROM machine
			WHERE archived_at IS NULL AND kind <> 'linux' AND agent_version ~ '^\d{1,6}(\.\d{1,6}){0,3}$'
			ORDER BY string_to_array(agent_version, '.')::int[] DESC
			LIMIT 1), '')`).Scan(&out.LatestAgentVersion); err != nil {
		return serverError(err)
	}

	rows, err := a.pool.Query(ctx, failuresCTE+`
		SELECT c.kind, c.item_id, ri.display_name, c.outcome, COALESCE(c.error_code, ''),
		       count(*)::int,
		       (array_agg(COALESCE(m.hostname, c.machine_id) ORDER BY c.ts DESC))[1:$2],
		       max(c.ts),
		       count(*) FILTER (WHERE $3::text <> '' AND m.agent_version IS DISTINCT FROM $3::text)::int
		FROM classified c
		LEFT JOIN machine m           ON m.machine_id = c.machine_id
		LEFT JOIN remediation_item ri ON ri.kind = c.kind AND ri.item_id = c.item_id
		WHERE c.open
		GROUP BY c.kind, c.item_id, ri.display_name, c.outcome, COALESCE(c.error_code, '')
		ORDER BY 6 DESC, 8 DESC`, since, failureHostnames, out.LatestAgentVersion)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			g    model.RemediationFailureGroup
			name *string
		)
		if err := rows.Scan(&g.Kind, &g.ItemID, &name, &g.Outcome, &g.ErrorCode,
			&g.Machines, &g.Hostnames, &g.LastTS, &g.StaleAgents); err != nil {
			return serverError(err)
		}
		g.DisplayName = sv(name)
		if g.Hostnames == nil {
			g.Hostnames = []string{}
		}
		out.Open = append(out.Open, g)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// GetRemediationImpact handles GET /api/admin/remediation/impact?days= — machine
// usage in the N days before a machine's first successful app removal against
// the N days after its last one.
//
// Only hours with an ACTIVE session count: an idle night dilutes both sides
// unevenly (a removal pass usually runs while the PC is on), and "usage" is what
// the admin is asking about. metric_1h (UTC) and session_user_1h (Istanbul) share
// hour boundaries because Turkey is a whole-hour offset with no DST.
//
// The before/after delta alone cannot tell a removal from a fleet-wide trend (a
// quiet holiday week, a Windows update), so the same windows are computed for a
// CONTROL group of Windows machines with no successful removal.
func (a *API) GetRemediationImpact(c echo.Context) error {
	ctx := c.Request().Context()

	days := queryInt(c, "days", impactDefaultDays)
	if days < 1 {
		days = 1
	} else if days > impactMaxDays {
		days = impactMaxDays
	}
	now := time.Now()
	since := now.Add(-attemptsWindow)

	out := model.RemediationImpactResponse{
		Days: days, GeneratedAt: now.UTC(), MinHours: impactMinHours,
		Machines: []model.RemediationImpactMachine{},
		Items:    []model.RemediationImpactItem{},
	}

	// ── Per machine ─────────────────────────────────────────────────────────────
	rows, err := a.pool.Query(ctx, `
		WITH removed AS (
			SELECT machine_id, item_id, min(ts) AS removed_at
			FROM remediation_attempt ra
			WHERE kind = 'app' AND outcome = 'ok' AND ts >= $2
			  -- attempts of purged machines stay as audit trail but have no
			  -- metrics left to compare: leave them out of the impact view
			  AND EXISTS (SELECT 1 FROM machine pm WHERE pm.machine_id = ra.machine_id)
			GROUP BY 1, 2
		),
		m AS (
			SELECT machine_id, min(removed_at) AS first_at, max(removed_at) AS last_at,
			       count(*)::int AS items
			FROM removed GROUP BY 1
		),
		act AS (
			SELECT bucket, machine_id
			FROM session_user_1h
			WHERE bucket >= $2::timestamptz - make_interval(days => $1)
			  AND machine_id IN (SELECT machine_id FROM m)
			GROUP BY 1, 2
			HAVING sum(active_samples) > 0
		),
		hourly AS (
			SELECT m.machine_id, h.cpu_avg, h.mem_ratio_avg,
			       CASE WHEN h.bucket < date_trunc('hour', m.first_at) THEN 'b'
			            WHEN h.bucket > m.last_at                      THEN 'a' END AS side
			FROM m
			JOIN metric_1h h ON h.machine_id = m.machine_id
			     AND h.bucket >= m.first_at - make_interval(days => $1)
			     AND h.bucket <= m.last_at  + make_interval(days => $1)
			JOIN act ON act.machine_id = h.machine_id AND act.bucket = h.bucket
		)
		SELECT m.machine_id, mc.hostname, m.first_at, m.last_at, m.items,
		       (m.last_at + make_interval(days => $1)) <= now(),
		       avg(hr.cpu_avg)       FILTER (WHERE hr.side = 'b'),
		       avg(hr.cpu_avg)       FILTER (WHERE hr.side = 'a'),
		       avg(hr.mem_ratio_avg) FILTER (WHERE hr.side = 'b'),
		       avg(hr.mem_ratio_avg) FILTER (WHERE hr.side = 'a'),
		       count(*) FILTER (WHERE hr.side = 'b')::int,
		       count(*) FILTER (WHERE hr.side = 'a')::int
		FROM m
		LEFT JOIN hourly hr     ON hr.machine_id = m.machine_id
		LEFT JOIN machine mc    ON mc.machine_id = m.machine_id
		GROUP BY m.machine_id, mc.hostname, m.first_at, m.last_at, m.items
		ORDER BY m.last_at DESC`, days, since)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			r    model.RemediationImpactMachine
			host *string
		)
		if err := rows.Scan(&r.MachineID, &host, &r.FirstRemoval, &r.LastRemoval, &r.ItemsRemoved,
			&r.Complete, &r.CPUBefore, &r.CPUAfter, &r.MemBefore, &r.MemAfter,
			&r.HoursBefore, &r.HoursAfter); err != nil {
			return serverError(err)
		}
		r.Hostname = sv(host)
		out.Machines = append(out.Machines, r)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	rows.Close()

	windows := make([]model.RemediationImpactWindow, len(out.Machines))
	for i, m := range out.Machines {
		windows[i] = m.RemediationImpactWindow
	}
	out.Fleet, out.FleetMachines = pairedMean(windows)

	// ── Control group ───────────────────────────────────────────────────────────
	// Anchored on the median first/last removal so the calendar windows match the
	// treated machines' as closely as one pair of windows can.
	if len(out.Machines) > 0 {
		firsts := make([]time.Time, len(out.Machines))
		lasts := make([]time.Time, len(out.Machines))
		for i, m := range out.Machines {
			firsts[i], lasts[i] = m.FirstRemoval, m.LastRemoval
		}
		ctl, err := a.controlWindows(ctx, days, since, medianTime(firsts), medianTime(lasts))
		if err != nil {
			return serverError(err)
		}
		out.Control, out.ControlMachines = pairedMean(ctl)
	}

	// ── Per item: the removed app's own processes ──────────────────────────────
	irows, err := a.pool.Query(ctx, `
		WITH removed AS (
			SELECT machine_id, item_id, min(ts) AS removed_at
			FROM remediation_attempt ra
			WHERE kind = 'app' AND outcome = 'ok' AND ts >= $2
			  -- attempts of purged machines stay as audit trail but have no
			  -- metrics left to compare: leave them out of the impact view
			  AND EXISTS (SELECT 1 FROM machine pm WHERE pm.machine_id = ra.machine_id)
			GROUP BY 1, 2
		)
		SELECT r.item_id, ri.display_name, count(DISTINCT r.machine_id)::int,
		       count(DISTINCT p.machine_id) FILTER (WHERE p.before)::int,
		       avg(p.cpu) FILTER (WHERE p.before),
		       avg(p.mem) FILTER (WHERE p.before),
		       count(DISTINCT p.machine_id) FILTER (WHERE NOT p.before)::int
		FROM removed r
		LEFT JOIN remediation_item ri ON ri.kind = 'app' AND ri.item_id = r.item_id
		LEFT JOIN LATERAL (
			-- Summed per sample instant: one app may run several processes.
			SELECT ps.machine_id, ps.ts,
			       sum(ps.cpu_pct)::float8   AS cpu,
			       sum(ps.mem_bytes)::float8 AS mem,
			       ps.ts < r.removed_at      AS before
			FROM process_sample ps
			WHERE ps.machine_id = r.machine_id
			  AND ps.process_name = ANY(ri.process_names)
			  AND ps.ts >= r.removed_at - make_interval(days => $1)
			  AND ps.ts <  r.removed_at + make_interval(days => $1)
			GROUP BY ps.machine_id, ps.ts
		) p ON true
		GROUP BY r.item_id, ri.display_name
		ORDER BY 3 DESC, 1`, days, since)
	if err != nil {
		return serverError(err)
	}
	defer irows.Close()
	for irows.Next() {
		var (
			it   model.RemediationImpactItem
			name *string
		)
		if err := irows.Scan(&it.ItemID, &name, &it.Machines, &it.ProcMachinesBefore,
			&it.ProcCPUBefore, &it.ProcMemBefore, &it.ProcMachinesAfter); err != nil {
			return serverError(err)
		}
		it.DisplayName = sv(name)
		out.Items = append(out.Items, it)
	}
	if err := irows.Err(); err != nil {
		return serverError(err)
	}

	return c.JSON(http.StatusOK, out)
}

// controlWindows computes the same active-hour before/after averages for Windows
// machines that had no successful app removal in the window.
func (a *API) controlWindows(ctx context.Context, days int, since, firstAt, lastAt time.Time) ([]model.RemediationImpactWindow, error) {
	rows, err := a.pool.Query(ctx, `
		WITH ctl AS (
			SELECT machine_id FROM machine
			WHERE kind = 'windows' AND archived_at IS NULL
			  AND machine_id NOT IN (
				SELECT machine_id FROM remediation_attempt
				WHERE kind = 'app' AND outcome = 'ok' AND ts >= $4)
		),
		act AS (
			SELECT bucket, machine_id
			FROM session_user_1h
			WHERE bucket >= $2::timestamptz - make_interval(days => $1)
			  AND bucket <= $3::timestamptz + make_interval(days => $1)
			  AND machine_id IN (SELECT machine_id FROM ctl)
			GROUP BY 1, 2
			HAVING sum(active_samples) > 0
		),
		hourly AS (
			SELECT h.machine_id, h.cpu_avg, h.mem_ratio_avg,
			       CASE WHEN h.bucket < date_trunc('hour', $2::timestamptz) THEN 'b'
			            WHEN h.bucket > $3::timestamptz                   THEN 'a' END AS side
			FROM metric_1h h
			JOIN act ON act.machine_id = h.machine_id AND act.bucket = h.bucket
			WHERE h.bucket >= $2::timestamptz - make_interval(days => $1)
			  AND h.bucket <= $3::timestamptz + make_interval(days => $1)
		)
		SELECT avg(cpu_avg)       FILTER (WHERE side = 'b'),
		       avg(cpu_avg)       FILTER (WHERE side = 'a'),
		       avg(mem_ratio_avg) FILTER (WHERE side = 'b'),
		       avg(mem_ratio_avg) FILTER (WHERE side = 'a'),
		       count(*) FILTER (WHERE side = 'b')::int,
		       count(*) FILTER (WHERE side = 'a')::int
		FROM hourly
		GROUP BY machine_id`, days, firstAt, lastAt, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.RemediationImpactWindow
	for rows.Next() {
		var w model.RemediationImpactWindow
		if err := rows.Scan(&w.CPUBefore, &w.CPUAfter, &w.MemBefore, &w.MemAfter,
			&w.HoursBefore, &w.HoursAfter); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// pairedMean averages each side over the SAME set of machines — those with at
// least impactMinHours active hours before and after. Averaging each side over
// whichever machines happen to have data would compare two different fleets.
func pairedMean(ws []model.RemediationImpactWindow) (model.RemediationImpactWindow, int) {
	var (
		out            model.RemediationImpactWindow
		cb, ca, mb, ma float64
		n, nMem        int
	)
	for _, w := range ws {
		if w.HoursBefore < impactMinHours || w.HoursAfter < impactMinHours ||
			w.CPUBefore == nil || w.CPUAfter == nil {
			continue
		}
		n++
		cb += *w.CPUBefore
		ca += *w.CPUAfter
		out.HoursBefore += w.HoursBefore
		out.HoursAfter += w.HoursAfter
		if w.MemBefore != nil && w.MemAfter != nil {
			nMem++
			mb += *w.MemBefore
			ma += *w.MemAfter
		}
	}
	if n > 0 {
		cb, ca = cb/float64(n), ca/float64(n)
		out.CPUBefore, out.CPUAfter = &cb, &ca
	}
	if nMem > 0 {
		mb, ma = mb/float64(nMem), ma/float64(nMem)
		out.MemBefore, out.MemAfter = &mb, &ma
	}
	return out, n
}

func medianTime(ts []time.Time) time.Time {
	s := append([]time.Time(nil), ts...)
	sort.Slice(s, func(i, j int) bool { return s[i].Before(s[j]) })
	return s[len(s)/2]
}
