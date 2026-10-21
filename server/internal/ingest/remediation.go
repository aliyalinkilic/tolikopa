package ingest

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aliyalinkilic/tolikopa/server/internal/appcatalog"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// Vocabulary of the fields the agent may send. Anything outside these sets is
// dropped rather than stored: the tables are an audit trail the panel renders,
// and arbitrary agent-supplied text has no business reaching it.
var (
	remediationKinds = map[string]bool{
		appcatalog.KindApp: true, appcatalog.KindStartup: true,
	}
	remediationScopes = map[string]bool{
		"": true, appcatalog.ScopeUser: true, appcatalog.ScopeProvisioned: true, appcatalog.ScopeBoth: true,
	}
	remediationActions = map[string]bool{
		"remove": true, "deprovision": true, "disable": true, "enable": true,
	}
	remediationOutcomes = map[string]bool{
		"ok": true, "failed": true, "dry_run": true, "not_present": true,
		"not_allowed": true, "blocked": true, "timeout": true, "unknown_item": true,
	}
)

// persistRemediation writes one machine's hygiene report inside the ingest
// transaction: presence (full-snapshot semantics), attempts, and the four
// machine.remediation_* posture columns.
//
// Unknown item_ids are STORED AS FREE TEXT, never rejected: an agent newer than
// the server is a normal state during a rollout and must not turn ingest into a
// 500. Nothing here grants an endpoint any capability — see the authority note in
// 0011_remediation.sql.
func persistRemediation(ctx context.Context, tx pgx.Tx, machineID string, r *model.RemediationReport) error {
	// Timestamps get the same window treatment as metric samples: a clock-skewed or
	// hostile agent must not be able to stamp state in 2099 or open a hypertable
	// chunk years away. checked_at is clamped (it is only a "as of" marker), while
	// an out-of-window attempt row is dropped (it is an audit record — a wrong time
	// is worse than a missing one).
	now := time.Now()
	maxFuture := now.Add(5 * time.Minute)
	minPast := now.Add(-7 * 24 * time.Hour)

	checkedAt := r.CheckedAt
	if checkedAt.IsZero() || checkedAt.After(maxFuture) || checkedAt.Before(minPast) {
		checkedAt = now
	}

	// ── Presence ───────────────────────────────────────────────────────────────
	// Collected into parallel arrays so the upsert is ONE statement regardless of
	// catalog size (57 items today, 512 allowed), not a round trip per item.
	kinds := make([]string, 0, len(r.Present))
	ids := make([]string, 0, len(r.Present))
	presents := make([]bool, 0, len(r.Present))
	scopes := make([]*string, 0, len(r.Present))
	enabled := make([]*bool, 0, len(r.Present))
	seen := make(map[string]struct{}, len(r.Present))

	unknown := 0
	for _, it := range r.Present {
		kind := strings.TrimSpace(it.Kind)
		id := strings.TrimSpace(it.ItemID)
		if !remediationKinds[kind] || id == "" || len(id) > maxItemIDLen {
			continue
		}
		// Validate against the server's own catalog, but STORE unknown ids anyway as
		// free text: during a rollout an agent legitimately carries a vocabulary the
		// server has not shipped yet, and losing that report would hide exactly the
		// drift the panel is supposed to show. The count is logged once per batch.
		if _, known := appcatalog.Lookup(kind, id); !known {
			unknown++
		}
		// The upsert targets a primary key; a duplicated (kind,item_id) inside one
		// statement would raise "cannot affect row a second time".
		k := kind + ":" + id
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}

		scope := strings.TrimSpace(it.Scope)
		if !remediationScopes[scope] {
			scope = ""
		}
		kinds = append(kinds, kind)
		ids = append(ids, id)
		presents = append(presents, it.Present)
		scopes = append(scopes, nullStr(scope))
		enabled = append(enabled, it.Enabled)
	}
	if unknown > 0 {
		log.Printf("ingest: machine %s reported %d remediation item(s) outside the server catalog %s (agent newer than server?)",
			machineID, unknown, appcatalog.Version())
	}

	if len(kinds) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO machine_remediation_state
				(machine_id, kind, item_id, present, scope, enabled,
				 first_seen, last_seen, last_present_at, removed_at)
			-- $2 is cast at every site: inside CASE it has no inferable context, and
			-- Postgres then deduces a different type than in the plain columns and
			-- rejects the whole statement with 42P08.
			SELECT $1, t.kind, t.item_id, t.present, t.scope, t.enabled,
			       $2::timestamptz, $2::timestamptz,
			       CASE WHEN t.present THEN $2::timestamptz END,
			       NULL::timestamptz
			FROM unnest($3::text[], $4::text[], $5::boolean[], $6::text[], $7::boolean[])
			     AS t(kind, item_id, present, scope, enabled)
			ON CONFLICT (machine_id, kind, item_id) DO UPDATE SET
				present  = EXCLUDED.present,
				scope    = EXCLUDED.scope,
				enabled  = EXCLUDED.enabled,
				last_seen = GREATEST(machine_remediation_state.last_seen, EXCLUDED.last_seen),
				last_present_at = CASE WHEN EXCLUDED.present
					THEN EXCLUDED.last_seen ELSE machine_remediation_state.last_present_at END,
				-- Stamp removed_at only on the present->absent EDGE, so the panel can
				-- say "removed: <date>" instead of re-stamping it every 60 s.
				removed_at = CASE
					WHEN EXCLUDED.present THEN NULL
					WHEN machine_remediation_state.present THEN EXCLUDED.last_seen
					ELSE machine_remediation_state.removed_at END`,
			machineID, checkedAt, kinds, ids, presents, scopes, enabled); err != nil {
			return err
		}

		// Full-snapshot sweep: an item this machine used to have but the report no
		// longer mentions (retired from the catalog, or the agent stopped scanning
		// it) is marked absent so the fleet counts cannot drift upwards forever.
		//
		// Guarded on len>0: `present` is `omitempty` on the wire, so a report that
		// carries only attempts would otherwise wipe the whole mirror to absent. An
		// empty list therefore means "no presence scan in this batch", not "nothing
		// is installed". last_seen is deliberately NOT touched — the agent did not
		// report on these rows.
		if _, err := tx.Exec(ctx, `
			UPDATE machine_remediation_state s
			SET present = false, removed_at = COALESCE(s.removed_at, $2::timestamptz)
			WHERE s.machine_id = $1 AND s.present
			  AND NOT EXISTS (
				SELECT 1 FROM unnest($3::text[], $4::text[]) AS t(kind, item_id)
				WHERE t.kind = s.kind AND t.item_id = s.item_id)`,
			machineID, checkedAt, kinds, ids); err != nil {
			return err
		}
	}

	// ── Attempts ───────────────────────────────────────────────────────────────
	var attemptRows [][]any
	for _, at := range r.Attempts {
		kind := strings.TrimSpace(at.Kind)
		id := strings.TrimSpace(at.ItemID)
		if at.TS.IsZero() || at.TS.After(maxFuture) || at.TS.Before(minPast) {
			continue
		}
		if !remediationKinds[kind] || id == "" || len(id) > maxItemIDLen {
			continue
		}
		if !remediationActions[at.Action] || !remediationOutcomes[at.Outcome] {
			continue
		}
		// error_code is a CODE ("0x80073cfa"); anything longer than the cap is not a
		// code and is dropped rather than truncated, so no message fragment or path
		// can ever land in the column.
		code := strings.TrimSpace(at.ErrorCode)
		if len(code) > maxErrorCodeLen {
			code = ""
		}
		dur := at.DurationMs
		if dur < 0 {
			dur = 0
		}
		attemptRows = append(attemptRows, []any{
			at.TS, machineID, kind, id, at.Action, at.Outcome, nullStr(code), nullInt(dur),
		})
	}
	if err := execMultiRow(ctx, tx,
		`INSERT INTO remediation_attempt (ts, machine_id, kind, item_id, action, outcome, error_code, duration_ms) VALUES `,
		8, attemptRows,
		` ON CONFLICT (machine_id, ts, kind, item_id, action) DO NOTHING`); err != nil {
		return err
	}

	// ── Posture columns on the inventory row ───────────────────────────────────
	// allowed collapses the three GPO kill switches: the agent can actuate at all
	// only when enforcement is on AND at least one kind is permitted.
	allowed := r.Allowed.Enforce && (r.Allowed.Apps || r.Allowed.Startup)
	if _, err := tx.Exec(ctx, `
		UPDATE machine SET
			remediation_allowed=$2, remediation_catalog_version=$3,
			remediation_policy_revision=$4, remediation_reported_at=$5,
			remediation_allow_apps=$6, remediation_allow_startup=$7, remediation_enforce=$8
		WHERE machine_id=$1`,
		machineID, allowed, nullStr(trimTo(r.CatalogVersion, maxVersionLen)),
		nullStr(trimTo(r.PolicyRevision, maxVersionLen)), checkedAt,
		r.Allowed.Apps, r.Allowed.Startup, r.Allowed.Enforce); err != nil {
		return err
	}
	return nil
}

// trimTo drops an over-long value entirely (rather than truncating it into
// something that looks like a valid version and would silently mismatch).
func trimTo(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		return ""
	}
	return s
}
