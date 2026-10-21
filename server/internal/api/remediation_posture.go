package api

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/appcatalog"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// The Optimization page's fleet roll-up. Strictly READ-ONLY: it summarises the
// hygiene report (allowlisted presence, attempts, the local switches) and, for
// Linux hosts, turns metrics the agent already sends into ADVISORY
// recommendations. Nothing here writes, selects an action, or reaches an agent —
// the policy PUT in remediation.go stays the only write path.

const (
	// advisoryDays is the history window behind the Linux averages.
	advisoryDays = 7
	// advisoryMinHours: below this many metric_1h buckets an average is one busy
	// afternoon, not a trend — CPU/RAM advice is withheld.
	advisoryMinHours = 12

	linuxDiskCritPct    = 95.0
	linuxDiskWarnPct    = 85.0
	linuxMemPressure    = 0.85
	linuxCPUSustained   = 75.0
	linuxLongUptimeDays = 90.0
)

// GetRemediationPosture handles GET /api/admin/remediation/posture.
func (a *API) GetRemediationPosture(c echo.Context) error {
	ctx := c.Request().Context()
	now := time.Now()
	since := now.Add(-attemptsWindow)

	pol, err := loadPolicy(ctx, a.pool)
	if err != nil {
		return serverError(err)
	}
	out := model.RemediationPostureResponse{
		GeneratedAt:    now.UTC(),
		CatalogVersion: appcatalog.Version(),
		PolicyRevision: pol.Revision,
		Platforms:      []model.RemediationPlatformSummary{},
		Items:          []model.RemediationItemProgress{},
		Machines:       []model.RemediationMachinePosture{},
		Linux:          []model.LinuxAdvisoryMachine{},
	}

	if out.Platforms, err = a.postureFleetPlatforms(ctx); err != nil {
		return serverError(err)
	}

	// Open failures per machine and per item, by the same classification as
	// GET /remediation/failures so the two views never disagree.
	failMachine := map[string]int{}
	failItem := map[string]int{}
	frows, err := a.pool.Query(ctx, failuresCTE+`
		SELECT machine_id, kind, item_id FROM classified WHERE open`, since)
	if err != nil {
		return serverError(err)
	}
	for frows.Next() {
		var mid, kind, id string
		if err := frows.Scan(&mid, &kind, &id); err != nil {
			frows.Close()
			return serverError(err)
		}
		failMachine[mid]++
		failItem[kind+":"+id]++
	}
	frows.Close()
	if err := frows.Err(); err != nil {
		return serverError(err)
	}

	// ── Per item ────────────────────────────────────────────────────────────────
	// "Still to do" means present for an app, present AND enabled for a startup
	// entry (a disabled Run value stays present — it is never deleted).
	irows, err := a.pool.Query(ctx, `
		WITH tgt AS (
			SELECT machine_id FROM machine
			WHERE archived_at IS NULL AND kind <> 'linux' AND NOT (tags && $2::text[])
		),
		pend AS (
			SELECT s.kind, s.item_id, count(*)::int AS n
			FROM machine_remediation_state s
			JOIN tgt ON tgt.machine_id = s.machine_id
			WHERE s.present AND (s.kind <> 'startup' OR s.enabled IS NOT FALSE)
			GROUP BY 1, 2
		),
		att AS (
			SELECT ra.kind, ra.item_id,
			       count(DISTINCT ra.machine_id) FILTER (WHERE ra.outcome = 'dry_run')::int AS dry,
			       count(DISTINCT ra.machine_id) FILTER (
			           WHERE ra.outcome IN ('ok', 'not_present')
			             AND NOT EXISTS (
			                 SELECT 1 FROM machine_remediation_state s
			                 WHERE s.machine_id = ra.machine_id AND s.kind = ra.kind
			                   AND s.item_id = ra.item_id AND s.present
			                   AND (s.kind <> 'startup' OR s.enabled IS NOT FALSE)))::int AS conv
			FROM remediation_attempt ra
			JOIN machine am ON am.machine_id = ra.machine_id AND am.archived_at IS NULL
			WHERE ra.ts >= $1
			GROUP BY 1, 2
		)
		SELECT ri.kind, ri.item_id, ri.display_name, (pi.item_id IS NOT NULL),
		       COALESCE(pend.n, 0), COALESCE(att.conv, 0), COALESCE(att.dry, 0)
		FROM remediation_item ri
		LEFT JOIN remediation_policy_item pi ON pi.kind = ri.kind AND pi.item_id = ri.item_id
		LEFT JOIN pend ON pend.kind = ri.kind AND pend.item_id = ri.item_id
		LEFT JOIN att  ON att.kind  = ri.kind AND att.item_id  = ri.item_id
		WHERE pi.item_id IS NOT NULL OR att.kind IS NOT NULL
		ORDER BY ri.sort_order, ri.kind, ri.item_id`, since, pol.ExcludedTags)
	if err != nil {
		return serverError(err)
	}
	for irows.Next() {
		var it model.RemediationItemProgress
		if err := irows.Scan(&it.Kind, &it.ItemID, &it.DisplayName, &it.Selected,
			&it.Pending, &it.Converged, &it.DryRun); err != nil {
			irows.Close()
			return serverError(err)
		}
		it.OpenFailures = failItem[it.Kind+":"+it.ItemID]
		out.Items = append(out.Items, it)
	}
	irows.Close()
	if err := irows.Err(); err != nil {
		return serverError(err)
	}

	// ── Per Windows machine ─────────────────────────────────────────────────────
	mrows, err := a.pool.Query(ctx, `
		WITH pend AS (
			SELECT s.machine_id,
			       count(*) FILTER (WHERE s.kind = 'app')::int     AS apps,
			       count(*) FILTER (WHERE s.kind = 'startup')::int AS startup
			FROM machine_remediation_state s
			JOIN remediation_policy_item pi ON pi.kind = s.kind AND pi.item_id = s.item_id
			WHERE s.present AND (s.kind <> 'startup' OR s.enabled IS NOT FALSE)
			GROUP BY 1
		),
		last_at AS (
			SELECT machine_id, max(ts) AS ts FROM remediation_attempt
			WHERE ts >= $1 GROUP BY 1
		)
		SELECT m.machine_id, m.hostname, m.tags, (m.tags && $2::text[]),
		       m.remediation_reported_at,
		       m.remediation_allow_apps, m.remediation_allow_startup, m.remediation_enforce,
		       m.remediation_allowed,
		       (m.remediation_reported_at IS NOT NULL
		        AND m.remediation_catalog_version IS DISTINCT FROM $3),
		       (m.remediation_reported_at IS NOT NULL
		        AND split_part(COALESCE(m.remediation_policy_revision, ''), '-', 1) = $4),
		       COALESCE(p.apps, 0), COALESCE(p.startup, 0), la.ts
		FROM machine m
		LEFT JOIN pend p     ON p.machine_id = m.machine_id
		LEFT JOIN last_at la ON la.machine_id = m.machine_id
		WHERE m.archived_at IS NULL AND m.kind <> 'linux'
		ORDER BY lower(m.hostname), m.machine_id`,
		since, pol.ExcludedTags, appcatalog.Version(), itoa64(pol.Revision))
	if err != nil {
		return serverError(err)
	}
	for mrows.Next() {
		var mp model.RemediationMachinePosture
		if err := mrows.Scan(&mp.MachineID, &mp.Hostname, &mp.Tags, &mp.Excluded, &mp.ReportedAt,
			&mp.AllowApps, &mp.AllowStartup, &mp.Enforce, &mp.Allowed,
			&mp.CatalogStale, &mp.PolicyCurrent,
			&mp.PendingApps, &mp.PendingStartup, &mp.LastAttemptAt); err != nil {
			mrows.Close()
			return serverError(err)
		}
		if mp.Tags == nil {
			mp.Tags = []string{}
		}
		// Excluded machines are not asked to do anything, so nothing is pending.
		if mp.Excluded {
			mp.PendingApps, mp.PendingStartup = 0, 0
		}
		mp.OpenFailures = failMachine[mp.MachineID]
		out.Machines = append(out.Machines, mp)
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return serverError(err)
	}

	if out.Linux, err = a.linuxAdvisory(ctx, now); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// postureFleetPlatforms counts machines and hygiene switches per machine kind.
func (a *API) postureFleetPlatforms(ctx context.Context) ([]model.RemediationPlatformSummary, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT kind, count(*)::int,
		       count(*) FILTER (WHERE remediation_reported_at IS NOT NULL)::int,
		       count(*) FILTER (WHERE remediation_reported_at IS NOT NULL AND remediation_allow_apps)::int,
		       count(*) FILTER (WHERE remediation_reported_at IS NOT NULL AND remediation_allow_startup)::int,
		       count(*) FILTER (WHERE remediation_reported_at IS NOT NULL AND remediation_enforce)::int,
		       count(*) FILTER (WHERE remediation_reported_at IS NOT NULL AND remediation_allowed)::int
		FROM machine WHERE archived_at IS NULL
		GROUP BY kind ORDER BY kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.RemediationPlatformSummary{}
	for rows.Next() {
		var p model.RemediationPlatformSummary
		if err := rows.Scan(&p.Kind, &p.Machines, &p.Reporting, &p.AllowApps,
			&p.AllowStartup, &p.Enforce, &p.Actuating); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// linuxAdvisory reads the same measurements the slow-machines report uses, for
// Linux hosts only, and attaches advice. Disk reads the latest sample per mount
// inside a bounded 12 h window (machine_live.disk_min_free_pct carries no mount).
func (a *API) linuxAdvisory(ctx context.Context, now time.Time) ([]model.LinuxAdvisoryMachine, error) {
	rows, err := a.pool.Query(ctx, `
		WITH lx AS (
			SELECT machine_id FROM machine WHERE archived_at IS NULL AND kind = 'linux'
		), hist AS (
			SELECT machine_id, avg(mem_ratio_avg)::float8 AS mem_avg,
			       avg(cpu_avg)::float8 AS cpu_avg, count(*)::int AS hours
			FROM metric_1h
			WHERE bucket >= now() - make_interval(days => $1)
			  AND machine_id IN (SELECT machine_id FROM lx)
			GROUP BY machine_id
		), disk_latest AS (
			SELECT DISTINCT ON (machine_id, mount) machine_id, mount, used_bytes, total_bytes
			FROM disk_usage_sample
			WHERE ts > now() - interval '12 hours' AND total_bytes > 0
			  AND machine_id IN (SELECT machine_id FROM lx)
			ORDER BY machine_id, mount, ts DESC
		), disk AS (
			SELECT DISTINCT ON (machine_id) machine_id, mount,
			       used_bytes::float8 / total_bytes * 100 AS used_pct,
			       total_bytes - used_bytes               AS free_bytes
			FROM disk_latest
			ORDER BY machine_id, used_bytes::float8 / total_bytes DESC
		), smart AS (
			SELECT DISTINCT machine_id FROM disk_health
			WHERE smart_ok = false OR predict_fail = true
		)
		SELECT m.machine_id, m.hostname, COALESCE(ml.ts, m.last_seen), ml.mem_total, ml.boot_time,
		       h.cpu_avg, h.mem_avg, COALESCE(h.hours, 0),
		       d.mount, d.used_pct, d.free_bytes, (s.machine_id IS NOT NULL)
		FROM machine m
		JOIN lx ON lx.machine_id = m.machine_id
		LEFT JOIN machine_live ml ON ml.machine_id = m.machine_id
		LEFT JOIN hist  h ON h.machine_id = m.machine_id
		LEFT JOIN disk  d ON d.machine_id = m.machine_id
		LEFT JOIN smart s ON s.machine_id = m.machine_id
		ORDER BY lower(m.hostname), m.machine_id`, advisoryDays)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	warn := a.cfg.OfflineWarn()
	out := []model.LinuxAdvisoryMachine{}
	for rows.Next() {
		var (
			lm        model.LinuxAdvisoryMachine
			lastSeen  *time.Time
			memTotal  *int64
			freeBytes *int64
			bootTime  *time.Time
			mount     *string
		)
		if err := rows.Scan(&lm.MachineID, &lm.Hostname, &lastSeen, &memTotal, &bootTime,
			&lm.CPUAvg, &lm.MemRatioAvg, &lm.SampleHours,
			&mount, &lm.DiskUsedPct, &freeBytes, &lm.SMARTBad); err != nil {
			return nil, err
		}
		if lastSeen != nil {
			lm.LastSeen = lastSeen.UTC()
			lm.Online = now.Sub(*lastSeen) < warn
		}
		lm.DiskMount = sv(mount)
		if memTotal != nil {
			lm.MemTotal = *memTotal
		}
		if freeBytes != nil {
			lm.DiskFreeBytes = *freeBytes
		}
		// Measured against last_seen (see SlowMachines): a host that has been off
		// for weeks must not keep accruing uptime.
		if bootTime != nil && !bootTime.IsZero() && lastSeen != nil && lastSeen.After(*bootTime) {
			lm.UptimeDays = lastSeen.Sub(*bootTime).Hours() / 24
		}
		lm.Advice = linuxAdvice(lm)
		out = append(out, lm)
	}
	return out, rows.Err()
}

// linuxAdvice turns measurements into recommendations. Inputs that were never
// measured produce no advice rather than a confident "fine".
func linuxAdvice(m model.LinuxAdvisoryMachine) []model.LinuxAdvice {
	out := []model.LinuxAdvice{}
	if m.SMARTBad {
		out = append(out, model.LinuxAdvice{Key: "smart", Severity: "crit", Value: 1})
	}
	if m.DiskUsedPct != nil {
		switch {
		case *m.DiskUsedPct >= linuxDiskCritPct:
			out = append(out, model.LinuxAdvice{Key: "disk_critical", Severity: "crit", Value: *m.DiskUsedPct})
		case *m.DiskUsedPct >= linuxDiskWarnPct:
			out = append(out, model.LinuxAdvice{Key: "disk_low", Severity: "warn", Value: *m.DiskUsedPct})
		}
	}
	if m.SampleHours >= advisoryMinHours {
		if m.MemRatioAvg != nil && *m.MemRatioAvg >= linuxMemPressure {
			out = append(out, model.LinuxAdvice{Key: "mem_pressure", Severity: "warn", Value: *m.MemRatioAvg})
		}
		if m.CPUAvg != nil && *m.CPUAvg >= linuxCPUSustained {
			out = append(out, model.LinuxAdvice{Key: "cpu_sustained", Severity: "warn", Value: *m.CPUAvg})
		}
	}
	// On a server a long uptime is not slowness; the advice is that installed
	// kernel / library updates only take effect after a restart.
	if m.UptimeDays >= linuxLongUptimeDays {
		out = append(out, model.LinuxAdvice{Key: "long_uptime", Severity: "warn", Value: m.UptimeDays})
	}
	return out
}
