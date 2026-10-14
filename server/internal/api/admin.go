package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// PutMachineLabels handles PUT /api/admin/machines/{id}/labels.
func (a *API) PutMachineLabels(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	var body struct {
		Labels map[string]any `json:"labels"`
		Tags   *[]string      `json:"tags"` // pointer: update tags only when sent
	}
	if err := c.Bind(&body); err != nil {
		return apierr.BadRequest("invalid request body")
	}
	if body.Labels == nil {
		body.Labels = map[string]any{}
	}
	labels, _ := json.Marshal(body.Labels)
	var affected int64
	if body.Tags != nil {
		ct, err := a.pool.Exec(ctx, `UPDATE machine SET labels=$2, tags=$3 WHERE machine_id=$1`, id, labels, *body.Tags)
		if err != nil {
			return serverError(err)
		}
		affected = ct.RowsAffected()
	} else {
		ct, err := a.pool.Exec(ctx, `UPDATE machine SET labels=$2 WHERE machine_id=$1`, id, labels)
		if err != nil {
			return serverError(err)
		}
		affected = ct.RowsAffected()
	}
	if affected == 0 {
		return apierr.NotFoundf("machine not found")
	}
	return c.JSON(http.StatusOK, map[string]any{"labels": body.Labels, "tags": body.Tags})
}

// ── Category rules CRUD ──────────────────────────────────────────────────────

// ListCategories handles GET /api/admin/categories.
func (a *API) ListCategories(c echo.Context) error {
	rows, err := a.pool.Query(c.Request().Context(),
		`SELECT id, category, match_type, match_value, priority, enabled
		 FROM process_category ORDER BY priority, category, match_value`)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out := []model.CategoryRule{}
	for rows.Next() {
		var r model.CategoryRule
		if err := rows.Scan(&r.ID, &r.Category, &r.MatchType, &r.MatchValue, &r.Priority, &r.Enabled); err != nil {
			return serverError(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// CreateCategory handles POST /api/admin/categories.
func (a *API) CreateCategory(c echo.Context) error {
	ctx := c.Request().Context()
	var r model.CategoryRule
	if err := c.Bind(&r); err != nil {
		return apierr.BadRequest("invalid request body")
	}
	if r.Category == "" || r.MatchValue == "" {
		return apierr.BadRequest("category and match_value are required")
	}
	if r.MatchType == "" {
		r.MatchType = "exact"
	}
	if r.Priority == 0 {
		r.Priority = 100
	}
	err := a.pool.QueryRow(ctx, `
		INSERT INTO process_category (category, match_type, match_value, priority, enabled)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		r.Category, r.MatchType, strings.ToLower(r.MatchValue), r.Priority, r.Enabled).Scan(&r.ID)
	if err != nil {
		return apierr.New(http.StatusConflict, "category_rule_conflict", "rule could not be added (it may already exist)")
	}
	_ = a.cats.Refresh(ctx)
	return c.JSON(http.StatusCreated, r)
}

// UpdateCategory handles PUT /api/admin/categories/{id}.
func (a *API) UpdateCategory(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	var r model.CategoryRule
	if err := c.Bind(&r); err != nil {
		return apierr.BadRequest("invalid request body")
	}
	tag, err := a.pool.Exec(ctx, `
		UPDATE process_category
		SET category=$2, match_type=$3, match_value=$4, priority=$5, enabled=$6, updated_at=now()
		WHERE id=$1`,
		id, r.Category, r.MatchType, strings.ToLower(r.MatchValue), r.Priority, r.Enabled)
	if err != nil {
		return serverError(err)
	}
	if tag.RowsAffected() == 0 {
		return apierr.NotFoundf("rule not found")
	}
	_ = a.cats.Refresh(ctx)
	r.ID = id
	return c.JSON(http.StatusOK, r)
}

// DeleteCategory handles DELETE /api/admin/categories/{id}.
func (a *API) DeleteCategory(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	tag, err := a.pool.Exec(ctx, `DELETE FROM process_category WHERE id=$1`, id)
	if err != nil {
		return serverError(err)
	}
	if tag.RowsAffected() == 0 {
		return apierr.NotFoundf("rule not found")
	}
	_ = a.cats.Refresh(ctx)
	return c.NoContent(http.StatusNoContent)
}

// Unclassified handles GET /api/admin/unclassified.
func (a *API) Unclassified(c echo.Context) error {
	ctx := c.Request().Context()
	from, to := parseRange(c)
	rows, err := a.pool.Query(ctx, `
		SELECT process_name, count(*)::bigint FROM process_sample
		WHERE category_id IS NULL AND ts BETWEEN $1 AND $2
		GROUP BY process_name ORDER BY count(*) DESC LIMIT 100`, from, to)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out := []model.UnclassifiedProcess{}
	for rows.Next() {
		var u model.UnclassifiedProcess
		if err := rows.Scan(&u.Name, &u.Count); err != nil {
			return serverError(err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// Reclassify handles POST /api/admin/reclassify: re-resolve process_sample.category_id
// over a recent window using the current classification map, then refresh the
// category rollups so the change is reflected immediately.
func (a *API) Reclassify(c echo.Context) error {
	ctx := c.Request().Context()
	var body struct {
		Window string `json:"window"`
	}
	_ = c.Bind(&body)
	dur := parseWindow(body.Window, 24*time.Hour)
	to := time.Now()
	from := to.Add(-dur)

	if err := a.cats.Refresh(ctx); err != nil {
		return serverError(err)
	}

	// Distinct process names in the window.
	rows, err := a.pool.Query(ctx,
		`SELECT DISTINCT process_name FROM process_sample WHERE ts BETWEEN $1 AND $2`, from, to)
	if err != nil {
		return serverError(err)
	}
	names := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return serverError(err)
		}
		names = append(names, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return serverError(err)
	}

	var updated int64
	for _, name := range names {
		var catID *int16
		if id, _, ok := a.cats.Resolve(name); ok {
			catID = &id
		}
		tag, err := a.pool.Exec(ctx, `
			UPDATE process_sample SET category_id=$4
			WHERE process_name=$1 AND ts BETWEEN $2 AND $3 AND category_id IS DISTINCT FROM $4`,
			name, from, to, catID)
		if err != nil {
			// Older data may live in compressed chunks; skip and continue.
			continue
		}
		updated += tag.RowsAffected()
	}

	// Refresh category rollups over the window (autocommit; cannot be in a tx).
	// View names are a fixed whitelist (not user input) so inlining is safe and
	// avoids the text->regclass cast ambiguity of a bound parameter.
	// Refresh with a NULL window_end: refreshing up to now() targets the still-open
	// current bucket, which refresh_continuous_aggregate rejects — so the refresh
	// was silently skipped and the category views stayed stale after a reclassify.
	// NULL end materializes every completed bucket from `from` onward.
	for _, view := range []string{"proc_cat_1h", "proc_cat_user_1h"} {
		if _, err := a.pool.Exec(ctx, "CALL refresh_continuous_aggregate('"+view+"', $1, NULL)", from); err != nil {
			// Non-fatal: rollups will catch up via their scheduled policy.
			continue
		}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"updated_rows": updated,
		"from":         from,
		"to":           to,
	})
}

// ── Alert rules ──────────────────────────────────────────────────────────────

// GetAlertRules handles GET /api/admin/alert-rules.
func (a *API) GetAlertRules(c echo.Context) error {
	rows, err := a.pool.Query(c.Request().Context(),
		`SELECT rule, enabled, warn_threshold, crit_threshold, for_seconds FROM alert_rule ORDER BY rule`)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out := []model.AlertRuleDTO{}
	for rows.Next() {
		var r model.AlertRuleDTO
		if err := rows.Scan(&r.Rule, &r.Enabled, &r.WarnThreshold, &r.CritThreshold, &r.ForSeconds); err != nil {
			return serverError(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// PutAlertRules handles PUT /api/admin/alert-rules (bulk threshold update).
func (a *API) PutAlertRules(c echo.Context) error {
	ctx := c.Request().Context()
	var rules []model.AlertRuleDTO
	if err := c.Bind(&rules); err != nil {
		return apierr.BadRequest("invalid request body")
	}
	for _, r := range rules {
		if _, err := a.pool.Exec(ctx, `
			UPDATE alert_rule SET enabled=$2, warn_threshold=$3, crit_threshold=$4, for_seconds=$5, updated_at=now()
			WHERE rule=$1`, r.Rule, r.Enabled, r.WarnThreshold, r.CritThreshold, r.ForSeconds); err != nil {
			return serverError(err)
		}
	}
	return a.GetAlertRules(c)
}

// IngestTokenInfo handles GET /api/admin/ingest-token: masked token + distribution hint.
func (a *API) IngestTokenInfo(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{
		"masked":      maskToken(a.cfg.IngestToken),
		"server_host": a.cfg.ServerHost,
		// hint_code is what the panel localises; hint is the English fallback.
		"hint_code": "ingest_token_config",
		"hint":      "Set this as INGEST_TOKEN in the agent's config.json; it is valid only for /api/v1/ingest.",
	})
}

// ── helpers ──────────────────────────────────────────────────────────────────

// parseWindow parses windows like "6h", "24h", "7d". Falls back to def.
func parseWindow(s string, def time.Duration) time.Duration {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return def
	}
	if strings.HasSuffix(s, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour
		}
		return def
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return d
	}
	return def
}

func maskToken(tok string) string {
	if len(tok) <= 8 {
		return strings.Repeat("*", len(tok))
	}
	return tok[:4] + strings.Repeat("*", len(tok)-8) + tok[len(tok)-4:]
}
