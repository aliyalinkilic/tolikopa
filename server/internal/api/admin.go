package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// machineLabelsBody is PUT /api/admin/machines/{id}/labels. Both keys are
// optional and independent — an omitted one keeps its stored value — but at
// least one must be sent. (Labels used to be overwritten with {} whenever the
// key was missing, so a tags-only save silently wiped them.)
type machineLabelsBody struct {
	Labels *map[string]any `json:"labels"`
	Tags   *[]string       `json:"tags"`
}

func (b *machineLabelsBody) validate() error {
	if b.Labels == nil && b.Tags == nil {
		return apierr.BadRequest("labels or tags is required")
	}
	if b.Tags != nil {
		tags := make([]string, 0, len(*b.Tags))
		seen := map[string]struct{}{}
		for _, raw := range *b.Tags {
			t := strings.TrimSpace(raw)
			if t == "" {
				continue
			}
			if len(t) > maxTagLen {
				return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
					"tag is too long", map[string]any{"field": "tags", "max_length": maxTagLen})
			}
			if _, dup := seen[t]; dup {
				continue
			}
			seen[t] = struct{}{}
			tags = append(tags, t)
		}
		b.Tags = &tags
	}
	if b.Labels != nil && *b.Labels == nil {
		empty := map[string]any{}
		b.Labels = &empty
	}
	return nil
}

// PutMachineLabels handles PUT /api/admin/machines/{id}/labels {labels?, tags?}.
func (a *API) PutMachineLabels(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	var body machineLabelsBody
	if err := apierr.Bind(c, &body); err != nil {
		return err
	}
	if err := body.validate(); err != nil {
		return err
	}
	var labels []byte
	if body.Labels != nil {
		labels, _ = json.Marshal(*body.Labels)
	}
	var tags []string
	if body.Tags != nil {
		tags = *body.Tags
	}
	var (
		outLabels []byte
		outTags   []string
	)
	err := a.pool.QueryRow(ctx, `
		UPDATE machine SET
			labels = CASE WHEN $2::jsonb IS NULL THEN labels ELSE $2::jsonb END,
			tags   = CASE WHEN $3::boolean THEN $4::text[] ELSE tags END
		WHERE machine_id=$1
		RETURNING labels, tags`, id, labels, body.Tags != nil, tags).Scan(&outLabels, &outTags)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFoundf("machine not found")
	}
	if err != nil {
		return serverError(err)
	}
	outMap := jsonbToMap(outLabels)
	if outMap == nil {
		outMap = map[string]any{}
	}
	if outTags == nil {
		outTags = []string{}
	}
	return c.JSON(http.StatusOK, map[string]any{"labels": outMap, "tags": outTags})
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

// Category-rule bounds. match_type is the closed set ingest.Categories resolves;
// a rule with any other value would be stored and then never match anything.
const (
	maxCategoryLen   = 64
	maxMatchValueLen = 128
)

var validMatchTypes = map[string]bool{"exact": true, "prefix": true, "glob": true}

// validateCategoryRule normalises and checks a rule body for create/update.
// An empty match_type defaults to "exact"; everything else must be present.
func validateCategoryRule(r *model.CategoryRule) error {
	field := func(name, msg string) error {
		return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest, msg, map[string]any{"field": name})
	}
	r.Category = strings.TrimSpace(r.Category)
	r.MatchValue = strings.ToLower(strings.TrimSpace(r.MatchValue))
	r.MatchType = strings.TrimSpace(r.MatchType)
	if r.Category == "" || len(r.Category) > maxCategoryLen {
		return field("category", "category is required (at most 64 characters)")
	}
	if r.MatchValue == "" || len(r.MatchValue) > maxMatchValueLen {
		return field("match_value", "match_value is required (at most 128 characters)")
	}
	if r.MatchType == "" {
		r.MatchType = "exact"
	}
	if !validMatchTypes[r.MatchType] {
		return field("match_type", "match_type must be exact, prefix or glob")
	}
	return nil
}

// CreateCategory handles POST /api/admin/categories.
func (a *API) CreateCategory(c echo.Context) error {
	ctx := c.Request().Context()
	var r model.CategoryRule
	if err := apierr.Bind(c, &r); err != nil {
		return err
	}
	if err := validateCategoryRule(&r); err != nil {
		return err
	}
	if r.Priority == 0 {
		r.Priority = 100
	}
	err := a.pool.QueryRow(ctx, `
		INSERT INTO process_category (category, match_type, match_value, priority, enabled)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		r.Category, r.MatchType, r.MatchValue, r.Priority, r.Enabled).Scan(&r.ID)
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
	// The body is the full rule (as GET returns it). It used to be bound loosely,
	// so an empty or mis-shaped body overwrote the rule with blanks and answered 200.
	var r model.CategoryRule
	if err := apierr.Bind(c, &r); err != nil {
		return err
	}
	if r.ID != 0 && r.ID != id {
		return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
			"body id does not match the path", map[string]any{"field": "id"})
	}
	if err := validateCategoryRule(&r); err != nil {
		return err
	}
	tag, err := a.pool.Exec(ctx, `
		UPDATE process_category
		SET category=$2, match_type=$3, match_value=$4, priority=$5, enabled=$6, updated_at=now()
		WHERE id=$1`,
		id, r.Category, r.MatchType, r.MatchValue, r.Priority, r.Enabled)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique (match_type, match_value)
		return apierr.New(http.StatusConflict, "category_rule_conflict", "a rule with this match already exists")
	}
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
	// {window?}: "6h", "24h", "7d" … (default 24h, at most 30 days). A value that
	// does not parse used to fall back to 24h silently; it is now a 400.
	var body struct {
		Window string `json:"window"`
	}
	if c.Request().ContentLength != 0 {
		if err := apierr.Bind(c, &body); err != nil {
			return err
		}
	}
	dur := parseWindow(body.Window, 24*time.Hour)
	if w := strings.TrimSpace(body.Window); w != "" && (parseWindow(w, 0) == 0 || dur > maxReclassifyWindow) {
		return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
			"window must look like 6h, 24h or 7d (at most 30d)", map[string]any{"field": "window"})
	}
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
	// The body is a bare array of AlertRuleDTO. An unknown rule name used to be
	// skipped silently (UPDATE … WHERE rule=$1 matching nothing) while the call
	// answered 200; now the whole save is rejected and nothing is written.
	var rules []model.AlertRuleDTO
	if err := apierr.Bind(c, &rules); err != nil {
		return err
	}
	for _, r := range rules {
		if r.ForSeconds < 0 {
			return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
				"for_seconds must not be negative", map[string]any{"field": "for_seconds", "rule": r.Rule})
		}
	}
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return serverError(err)
	}
	defer tx.Rollback(ctx)
	for _, r := range rules {
		tag, err := tx.Exec(ctx, `
			UPDATE alert_rule SET enabled=$2, warn_threshold=$3, crit_threshold=$4, for_seconds=$5, updated_at=now()
			WHERE rule=$1`, r.Rule, r.Enabled, r.WarnThreshold, r.CritThreshold, r.ForSeconds)
		if err != nil {
			return serverError(err)
		}
		if tag.RowsAffected() == 0 {
			return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
				"unknown alert rule: "+truncate(r.Rule, 64), map[string]any{"field": "rule", "rule": truncate(r.Rule, 64)})
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return serverError(err)
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

// maxReclassifyWindow bounds POST /admin/reclassify. Raw process_sample is kept
// 21 days, so anything wider only adds an empty scan.
const maxReclassifyWindow = 30 * 24 * time.Hour

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
