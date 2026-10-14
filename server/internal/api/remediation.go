package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/appcatalog"
	"github.com/aliyalinkilic/tolikopa/server/internal/auth"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// Endpoint hygiene (bloatware removal / startup control).
//
// AUTHORITY: the agent carries a COMPILED allowlist and a local GPO kill switch.
// Everything here can only SELECT FROM that vocabulary — an item_id the agent
// does not recognise is refused ON THE ENDPOINT and reported back as outcome
// "unknown_item". These routes are a UI mirror and an audit trail, never a
// command channel (CLAUDE.md §Safety). Two independent vetoes remain outside the
// panel's reach: remediation_policy.dry_run (server-side, defaults ON) and the
// per-machine GPO switches (client-side, invisible to any server compromise).
//
// Route shapes here are deliberately flat (/admin/remediation, …/policy,
// …/attempts) rather than copying the /admin/categories pattern, whose server
// paths and frontend calls already disagree.

const (
	attemptsDefaultLimit = 100
	attemptsMaxLimit     = 500
	// attemptsWindow bounds the audit feed so it never plans a full scan of the
	// hypertable. Retention is 365 d; anything older is a report, not a feed.
	attemptsWindow = 90 * 24 * time.Hour
	// machineAttempts is the per-machine tail shown on the machine detail page.
	machineAttempts = 50
	// Bounds on admin-supplied values. item_ids are additionally checked against
	// the closed vocabulary; tags are free text and only get these.
	maxItemIDLen    = 64
	maxTagLen       = 64
	maxExcludedTags = 64
)

// GetRemediation handles GET /api/admin/remediation — the one-shot settings
// payload: catalog grouped by kind (with presence counts and selection state),
// the current policy, and the fleet drift summary.
func (a *API) GetRemediation(c echo.Context) error {
	ctx := c.Request().Context()

	out := model.RemediationSettings{
		CatalogVersion: appcatalog.Version(),
		Apps:           []model.RemediationCatalogItem{},
		Startup:        []model.RemediationCatalogItem{},
	}

	// The catalog comes from remediation_item (appcatalog.Sync mirrors the binary
	// there at startup) so `deprecated` and the curated sort order are visible, and
	// so an item retired from the vocabulary can still be shown while machines
	// report it.
	rows, err := a.pool.Query(ctx, `
		SELECT ri.kind, ri.item_id, ri.family, ri.display_name, ri.category, ri.risk_tier,
		       ri.scope, ri.safe_default, ri.process_names, ri.notes, ri.deprecated,
		       (pi.item_id IS NOT NULL) AS selected,
		       COALESCE(pc.n, 0)        AS present_count
		FROM remediation_item ri
		LEFT JOIN remediation_policy_item pi
		       ON pi.kind = ri.kind AND pi.item_id = ri.item_id
		LEFT JOIN (
			SELECT kind, item_id, count(*)::int AS n
			FROM machine_remediation_state WHERE present GROUP BY 1, 2
		) pc ON pc.kind = ri.kind AND pc.item_id = ri.item_id
		ORDER BY ri.sort_order, ri.kind, ri.item_id`)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			it                           model.RemediationCatalogItem
			category, risk, scope, notes *string
		)
		if err := rows.Scan(&it.Kind, &it.ItemID, &it.Family, &it.DisplayName, &category, &risk,
			&scope, &it.SafeDefault, &it.ProcessNames, &notes, &it.Deprecated,
			&it.Selected, &it.PresentCount); err != nil {
			return serverError(err)
		}
		it.Category, it.RiskTier, it.Scope, it.Notes = sv(category), sv(risk), sv(scope), sv(notes)
		if it.Kind == appcatalog.KindStartup {
			out.Startup = append(out.Startup, it)
		} else {
			out.Apps = append(out.Apps, it)
		}
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}

	pol, err := loadPolicy(ctx, a.pool)
	if err != nil {
		return serverError(err)
	}
	out.Policy = pol

	// Fleet drift. policy_current strips the "-x" suffix Policies.Resolve adds for
	// tag-excluded machines: those have applied the current revision too, they just
	// applied its "does not apply to you" form.
	if err := a.pool.QueryRow(ctx, `
		SELECT count(*)::int,
		       count(*) FILTER (WHERE remediation_reported_at IS NOT NULL)::int,
		       count(*) FILTER (WHERE remediation_reported_at IS NOT NULL
		                          AND remediation_catalog_version IS DISTINCT FROM $1)::int,
		       count(*) FILTER (WHERE remediation_reported_at IS NOT NULL
		                          AND split_part(COALESCE(remediation_policy_revision, ''), '-', 1) = $2)::int,
		       count(*) FILTER (WHERE remediation_reported_at IS NOT NULL
		                          AND remediation_allowed IS NOT TRUE)::int
		FROM machine WHERE archived_at IS NULL`, appcatalog.Version(), itoa64(pol.Revision)).
		Scan(&out.Fleet.Machines, &out.Fleet.Reporting, &out.Fleet.CatalogStale,
			&out.Fleet.PolicyCurrent, &out.Fleet.KillSwitchOff); err != nil {
		return serverError(err)
	}
	out.Fleet.NeverReported = out.Fleet.Machines - out.Fleet.Reporting

	return c.JSON(http.StatusOK, out)
}

// PutRemediationPolicy handles PUT /api/admin/remediation/policy.
//
// Transactional: policy + selected items + revision bump + audit row commit
// together, then the ingest cache is refreshed so the next batch already carries
// the new revision. Item ids outside the server's catalog are rejected with 400 —
// the closed vocabulary is enforced here, before anything reaches the database.
func (a *API) PutRemediationPolicy(c echo.Context) error {
	ctx := c.Request().Context()

	var body model.RemediationPolicyUpdate
	if err := c.Bind(&body); err != nil {
		return apierr.BadRequest("invalid request body")
	}

	apps, err := cleanItemIDs(body.Apps, appcatalog.KindApp)
	if err != nil {
		return err
	}
	startup, err := cleanItemIDs(body.Startup, appcatalog.KindStartup)
	if err != nil {
		return err
	}
	tags := cleanTags(body.ExcludedTags)

	// `before` is captured inside the transaction so the audit row cannot record a
	// state that a concurrent write already replaced.
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return serverError(err)
	}
	defer tx.Rollback(ctx)

	before, err := loadPolicy(ctx, tx)
	if err != nil {
		return serverError(err)
	}

	// The dry-run-first interlock is enforced HERE, not only in the panel. The UI
	// gates the enforce radio, but that is cosmetic: a curl, a stale tab, or a
	// second admin client can PUT {enabled:true, dry_run:false} directly and skip
	// the one rail that stops a fleet-wide uninstall from being a single click.
	//
	// Arming enforcement requires that at least one dry-run attempt has actually
	// been observed for the policy being replaced. `force` is the documented
	// override and is recorded in the audit row.
	if before.DryRun && !body.DryRun && body.Enabled && !body.Force {
		var seen bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM remediation_attempt WHERE outcome = 'dry_run')`).Scan(&seen); err != nil {
			return serverError(err)
		}
		if !seen {
			return apierr.New(http.StatusConflict, "dry_run_required",
				"run in dry-run mode first: no machine has reported a dry-run result yet")
		}
	}

	actor := ""
	if claims, ok := auth.ClaimsFrom(c); ok && claims != nil {
		actor = claims.Subject
	}

	after := model.RemediationPolicyDTO{
		Enabled: body.Enabled, DryRun: body.DryRun, ExcludedTags: tags,
		Apps: apps, Startup: startup, UpdatedBy: actor,
	}
	var updatedAt time.Time
	if err := tx.QueryRow(ctx, `
		UPDATE remediation_policy
		SET enabled=$1, dry_run=$2, excluded_tags=$3,
		    revision=revision+1, updated_at=now(), updated_by=$4
		WHERE id = 1
		RETURNING revision, updated_at`,
		body.Enabled, body.DryRun, tags, nullStr(actor)).Scan(&after.Revision, &updatedAt); err != nil {
		return serverError(err)
	}
	after.UpdatedAt = &updatedAt

	// Replace the selection wholesale: the payload is the complete desired set, and
	// a diff would be more code for the same result on a table of at most a few
	// dozen rows.
	if _, err := tx.Exec(ctx, `DELETE FROM remediation_policy_item`); err != nil {
		return serverError(err)
	}
	if len(apps)+len(startup) > 0 {
		kinds := make([]string, 0, len(apps)+len(startup))
		ids := make([]string, 0, len(apps)+len(startup))
		for _, id := range apps {
			kinds, ids = append(kinds, appcatalog.KindApp), append(ids, id)
		}
		for _, id := range startup {
			kinds, ids = append(kinds, appcatalog.KindStartup), append(ids, id)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO remediation_policy_item (kind, item_id)
			SELECT t.kind, t.item_id FROM unnest($1::text[], $2::text[]) AS t(kind, item_id)
			ON CONFLICT (kind, item_id) DO NOTHING`, kinds, ids); err != nil {
			return serverError(err)
		}
	}

	// A fleet-wide destructive capability needs a permanent record of who armed it —
	// including whether the dry-run interlock was overridden.
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(struct {
		model.RemediationPolicyDTO
		Forced bool `json:"forced,omitempty"`
	}{RemediationPolicyDTO: after, Forced: body.Force})
	if _, err := tx.Exec(ctx, `
		INSERT INTO remediation_policy_audit (actor, actor_ip, revision, "before", "after")
		VALUES ($1,$2,$3,$4,$5)`,
		nullStr(actor), nullStr(c.RealIP()), after.Revision, beforeJSON, afterJSON); err != nil {
		return serverError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return serverError(err)
	}

	// Refresh the ingest snapshot so the very next batch carries the new revision.
	// Non-fatal: the write is already committed, and reporting 500 here would push
	// the admin into re-submitting and bumping the revision a second time. The
	// periodic reload in ingest.Policies converges within a minute regardless.
	if err := a.pols.Refresh(ctx); err != nil {
		log.Printf("api: remediation policy cache refresh failed (revision %d): %v", after.Revision, err)
	}
	return c.JSON(http.StatusOK, after)
}

// GetRemediationAttempts handles
// GET /api/admin/remediation/attempts?limit=&outcome=&item_id= — the audit feed.
func (a *API) GetRemediationAttempts(c echo.Context) error {
	ctx := c.Request().Context()

	limit := queryInt(c, "limit", attemptsDefaultLimit)
	if limit < 1 {
		limit = 1
	} else if limit > attemptsMaxLimit {
		limit = attemptsMaxLimit
	}
	outcome := strings.TrimSpace(c.QueryParam("outcome"))
	itemID := strings.TrimSpace(c.QueryParam("item_id"))

	rows, err := a.pool.Query(ctx, `
		SELECT ra.ts, ra.machine_id, m.hostname, ra.kind, ra.item_id, ri.display_name,
		       ra.action, ra.outcome, ra.error_code, ra.duration_ms, `+attemptResolvedSQL+`
		FROM remediation_attempt ra
		LEFT JOIN machine m          ON m.machine_id = ra.machine_id
		LEFT JOIN remediation_item ri ON ri.kind = ra.kind AND ri.item_id = ra.item_id
		WHERE ra.ts >= $1
		  AND ($2::text = '' OR ra.outcome = $2)
		  AND ($3::text = '' OR ra.item_id = $3)
		ORDER BY ra.ts DESC
		LIMIT $4`, time.Now().Add(-attemptsWindow), outcome, itemID, limit)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()

	out := model.RemediationAttemptsResponse{Attempts: []model.RemediationAttemptDTO{}}
	for rows.Next() {
		at, err := scanAttempt(rows)
		if err != nil {
			return serverError(err)
		}
		out.Attempts = append(out.Attempts, at)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// MachineRemediation handles GET /api/machines/{id}/remediation (viewer scope):
// this machine's ALLOWLISTED-item presence plus its recent attempts.
//
// This is NOT a software inventory: anything outside the agent's compiled catalog
// is never inspected on the endpoint, never transmitted and never stored.
func (a *API) MachineRemediation(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	out := model.MachineRemediation{
		MachineID:            id,
		ServerCatalogVersion: appcatalog.Version(),
		Items:                []model.MachineRemediationItem{},
		Attempts:             []model.RemediationAttemptDTO{},
	}

	var catVer, polRev *string
	var allowed *bool
	err := a.pool.QueryRow(ctx, `
		SELECT remediation_catalog_version, remediation_policy_revision,
		       remediation_allowed, remediation_reported_at
		FROM machine WHERE machine_id = $1`, id).
		Scan(&catVer, &polRev, &allowed, &out.ReportedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFoundf("machine not found")
	}
	if err != nil {
		return serverError(err)
	}
	out.CatalogVersion, out.PolicyRevision = sv(catVer), sv(polRev)
	out.Allowed = allowed != nil && *allowed
	// Only a machine that has actually reported can be stale; a silent one is
	// "unknown", not "out of date".
	out.CatalogStale = out.CatalogVersion != "" && out.CatalogVersion != out.ServerCatalogVersion

	rows, err := a.pool.Query(ctx, `
		SELECT s.kind, s.item_id, ri.display_name, ri.risk_tier, s.present, s.scope, s.enabled,
		       (pi.item_id IS NOT NULL) AS selected,
		       s.first_seen, s.last_seen, s.last_present_at, s.removed_at
		FROM machine_remediation_state s
		LEFT JOIN remediation_item ri ON ri.kind = s.kind AND ri.item_id = s.item_id
		LEFT JOIN remediation_policy_item pi ON pi.kind = s.kind AND pi.item_id = s.item_id
		WHERE s.machine_id = $1
		ORDER BY s.present DESC, ri.sort_order NULLS LAST, s.kind, s.item_id`, id)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			it                model.MachineRemediationItem
			name, risk, scope *string
		)
		if err := rows.Scan(&it.Kind, &it.ItemID, &name, &risk, &it.Present, &scope, &it.Enabled,
			&it.Selected, &it.FirstSeen, &it.LastSeen, &it.LastPresentAt, &it.RemovedAt); err != nil {
			return serverError(err)
		}
		it.DisplayName, it.RiskTier, it.Scope = sv(name), sv(risk), sv(scope)
		out.Items = append(out.Items, it)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}

	arows, err := a.pool.Query(ctx, `
		SELECT ra.ts, ra.machine_id, m.hostname, ra.kind, ra.item_id, ri.display_name,
		       ra.action, ra.outcome, ra.error_code, ra.duration_ms, `+attemptResolvedSQL+`
		FROM remediation_attempt ra
		LEFT JOIN machine m           ON m.machine_id = ra.machine_id
		LEFT JOIN remediation_item ri ON ri.kind = ra.kind AND ri.item_id = ra.item_id
		WHERE ra.machine_id = $1 AND ra.ts >= $2
		ORDER BY ra.ts DESC
		LIMIT $3`, id, time.Now().Add(-attemptsWindow), machineAttempts)
	if err != nil {
		return serverError(err)
	}
	defer arows.Close()
	for arows.Next() {
		at, err := scanAttempt(arows)
		if err != nil {
			return serverError(err)
		}
		out.Attempts = append(out.Attempts, at)
	}
	if err := arows.Err(); err != nil {
		return serverError(err)
	}

	return c.JSON(http.StatusOK, out)
}

// ── helpers ──────────────────────────────────────────────────────────────────

// attemptResolvedSQL answers "did this failure later go away?" for the audit feed.
// A failed row stays in the 365-day table forever, so without it the panel keeps
// showing "Failed" for items a later pass removed. NULL for non-failure rows.
// Correlated on `ra` — the alias both attempt SELECTs use.
const attemptResolvedSQL = `
	CASE WHEN ra.outcome IN ('failed', 'timeout', 'blocked') THEN
		EXISTS (SELECT 1 FROM remediation_attempt r2
		        WHERE r2.kind = ra.kind AND r2.item_id = ra.item_id
		          AND r2.machine_id = ra.machine_id AND r2.ts > ra.ts
		          AND r2.outcome IN ('ok', 'not_present'))
		OR EXISTS (SELECT 1 FROM machine_remediation_state s
		           WHERE s.machine_id = ra.machine_id AND s.kind = ra.kind
		             AND s.item_id = ra.item_id AND NOT s.present AND s.last_seen > ra.ts)
	END AS resolved`

// scanAttempt reads one row of the shared attempt SELECT shape.
func scanAttempt(rows pgx.Rows) (model.RemediationAttemptDTO, error) {
	var (
		at               model.RemediationAttemptDTO
		host, name, code *string
		dur              *int
	)
	if err := rows.Scan(&at.TS, &at.MachineID, &host, &at.Kind, &at.ItemID, &name,
		&at.Action, &at.Outcome, &code, &dur, &at.Resolved); err != nil {
		return at, err
	}
	at.Hostname, at.DisplayName, at.ErrorCode = sv(host), sv(name), sv(code)
	if dur != nil {
		at.DurationMs = *dur
	}
	return at, nil
}

// loadPolicy reads the singleton policy plus its selected item ids.
func loadPolicy(ctx context.Context, q querier) (model.RemediationPolicyDTO, error) {
	out := model.RemediationPolicyDTO{
		ExcludedTags: []string{}, Apps: []string{}, Startup: []string{},
	}
	var updatedAt time.Time
	var updatedBy *string
	if err := q.QueryRow(ctx, `
		SELECT enabled, dry_run, excluded_tags, revision, updated_at, updated_by
		FROM remediation_policy WHERE id = 1`).
		Scan(&out.Enabled, &out.DryRun, &out.ExcludedTags, &out.Revision, &updatedAt, &updatedBy); err != nil {
		return out, err
	}
	out.UpdatedAt, out.UpdatedBy = &updatedAt, sv(updatedBy)

	rows, err := q.Query(ctx, `SELECT kind, item_id FROM remediation_policy_item ORDER BY kind, item_id`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id string
		if err := rows.Scan(&kind, &id); err != nil {
			return out, err
		}
		if kind == appcatalog.KindStartup {
			out.Startup = append(out.Startup, id)
		} else {
			out.Apps = append(out.Apps, id)
		}
	}
	return out, rows.Err()
}

// querier is the subset of pgxpool.Pool / pgx.Tx the policy loader needs, so the
// same code serves the read path and the transactional write path.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// cleanItemIDs validates a selection against the CLOSED vocabulary and returns it
// deduplicated and sorted. An id the server does not carry is a 400: silently
// dropping it would leave the admin believing they had selected something.
func cleanItemIDs(in []string, kind string) ([]string, error) {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := appcatalog.Lookup(kind, id); !ok {
			// The offending id is echoed back (truncated) so the panel can point at
			// it, e.g. after a catalog rollback left a stale selection in the UI.
			return nil, apierr.WithParams(http.StatusBadRequest, "unknown_catalog_item",
				"item is not in the catalog: "+kind+":"+truncate(id, maxItemIDLen),
				map[string]any{"kind": kind, "item_id": truncate(id, maxItemIDLen)})
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// cleanTags trims, deduplicates and sorts the exclusion tags so the stored array
// (and therefore the audit diff) is stable regardless of UI ordering.
func cleanTags(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		t := strings.TrimSpace(raw)
		// Tags are free text (they come from PUT /admin/machines/{id}/labels), so
		// they get bounds here rather than a vocabulary check.
		if t == "" || len(t) > maxTagLen || len(out) >= maxExcludedTags {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// truncate bounds a value echoed back in an error message.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }
