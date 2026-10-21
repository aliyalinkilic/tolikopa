package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/ingest"
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

// ListCategories handles GET /api/admin/categories[?window=7d]. Each rule carries
// how often it was the winning match over the window (from proc_cat_1h, whose
// category_id is the rule id), so the panel can flag rules that never match.
func (a *API) ListCategories(c echo.Context) error {
	ctx := c.Request().Context()
	win := parseWindow(c.QueryParam("window"), 7*24*time.Hour)
	if win > maxReclassifyWindow {
		win = maxReclassifyWindow
	}
	rows, err := a.pool.Query(ctx, `
		SELECT pc.id, pc.category, pc.match_type, pc.match_value, pc.priority, pc.enabled, pc.builtin,
		       coalesce(s.samples, 0), coalesce(s.machines, 0), s.last_seen
		FROM process_category pc
		LEFT JOIN (
			SELECT category_id, sum(samples)::bigint AS samples,
			       count(DISTINCT machine_id)::bigint AS machines, max(bucket) AS last_seen
			FROM proc_cat_1h
			WHERE bucket >= $1 AND category_id IS NOT NULL
			GROUP BY category_id
		) s ON s.category_id = pc.id
		ORDER BY pc.priority, pc.category, pc.match_value`, time.Now().Add(-win))
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out := []model.CategoryRule{}
	for rows.Next() {
		var r model.CategoryRule
		if err := rows.Scan(&r.ID, &r.Category, &r.MatchType, &r.MatchValue, &r.Priority, &r.Enabled,
			&r.Builtin, &r.Samples, &r.Machines, &r.LastSeen); err != nil {
			return serverError(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// TestCategory handles GET /api/admin/categories/test?name=…: which enabled rule
// a process name resolves to with the current cache, plus the rules it shadows.
func (a *API) TestCategory(c echo.Context) error {
	name := c.QueryParam("name")
	if strings.TrimSpace(name) == "" || len(name) > maxMatchValueLen {
		return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
			"name is required (at most 128 characters)", map[string]any{"field": "name"})
	}
	norm, matches := a.cats.Explain(name)
	res := model.CategoryTestResult{Name: norm, Matches: make([]model.CategoryTestMatch, 0, len(matches))}
	for _, m := range matches {
		res.Matches = append(res.Matches, model.CategoryTestMatch{
			ID: int(m.ID), Category: m.Category, MatchType: m.MatchType, MatchValue: m.MatchValue, Priority: m.Priority,
		})
	}
	if len(matches) > 0 {
		res.Category = matches[0].Category
	}
	return c.JSON(http.StatusOK, res)
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
	r.Builtin, r.Samples, r.Machines, r.LastSeen = false, 0, 0, nil
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
		SET category=$2, match_type=$3, match_value=$4, priority=$5, enabled=$6,
		    builtin=false, updated_at=now()
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
	r.Builtin, r.Samples, r.Machines, r.LastSeen = false, 0, 0, nil
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

// Unclassified handles GET /api/admin/unclassified[?from&to&sort=machines|count|cpu].
// Names come from the proc_name_1h rollup and are resolved against the CURRENT
// rule cache, so a name disappears from the list as soon as a rule covers it —
// even before a reclassify rewrites the stored category_id. Each row carries its
// distinct machine count (one raw count over just these names) and its share of
// all process CPU in the window, so the most impactful names come first.
func (a *API) Unclassified(c echo.Context) error {
	ctx := c.Request().Context()
	from, to := parseRange(c)
	// Same bound as ListCategories/reclassify: the raw distinct-machine count
	// below scans process_sample over the whole window.
	if to.Sub(from) > maxReclassifyWindow {
		from = to.Add(-maxReclassifyWindow)
	}
	sortBy := c.QueryParam("sort")
	switch sortBy {
	case "":
		sortBy = "machines"
	case "machines", "count", "cpu":
	default:
		return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
			"sort must be machines, count or cpu", map[string]any{"field": "sort"})
	}

	rows, err := a.pool.Query(ctx, `
		SELECT process_name, sum(samples)::bigint, coalesce(sum(cpu_sum), 0)::float8, max(bucket)
		FROM proc_name_1h
		WHERE bucket >= $1 AND bucket < $2
		GROUP BY process_name`, from.Truncate(time.Hour), to)
	if err != nil {
		return serverError(err)
	}
	var total float64
	byName := map[string]*model.UnclassifiedProcess{}
	cpuSum := map[string]float64{}
	for rows.Next() {
		var name string
		var n int64
		var cs float64
		var last time.Time
		if err := rows.Scan(&name, &n, &cs, &last); err != nil {
			rows.Close()
			return serverError(err)
		}
		total += cs
		if _, _, ok := a.cats.Resolve(name); ok {
			continue
		}
		byName[name] = &model.UnclassifiedProcess{Name: name, Count: n, LastSeen: last.Add(time.Hour)}
		cpuSum[name] = cs
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return serverError(err)
	}

	out := make([]model.UnclassifiedProcess, 0, len(byName))
	if len(byName) > 0 {
		names := topUnclassifiedNames(byName, cpuSum, sortBy, maxUnclassifiedCandidates)
		for n := range byName {
			if _, keep := names[n]; !keep {
				delete(byName, n)
			}
		}
		nameList := make([]string, 0, len(names))
		for n := range names {
			nameList = append(nameList, n)
		}
		// Distinct machines are not summable across hourly buckets; count them
		// from the raw rows, restricted to the unclassified names.
		mrows, err := a.pool.Query(ctx, `
			SELECT process_name, count(DISTINCT machine_id)::bigint
			FROM process_sample
			WHERE ts BETWEEN $1 AND $2 AND process_name = ANY($3)
			GROUP BY process_name`, from, to, nameList)
		if err != nil {
			return serverError(err)
		}
		for mrows.Next() {
			var name string
			var m int64
			if err := mrows.Scan(&name, &m); err != nil {
				mrows.Close()
				return serverError(err)
			}
			if u := byName[name]; u != nil {
				u.Machines = m
			}
		}
		mrows.Close()
		if err := mrows.Err(); err != nil {
			return serverError(err)
		}
		for name, u := range byName {
			cs := cpuSum[name]
			if u.Count > 0 {
				u.CPUAvg = cs / float64(u.Count)
			}
			if total > 0 {
				u.CPUShare = math.Min(cs/total, 1)
			}
			out = append(out, *u)
		}
	}
	sortUnclassified(out, sortBy)
	if len(out) > maxUnclassified {
		out = out[:maxUnclassified]
	}
	// The bucket end can run past the window edge; never report a future time.
	for i := range out {
		if out[i].LastSeen.After(to) {
			out[i].LastSeen = to
		}
	}
	return c.JSON(http.StatusOK, out)
}

// maxUnclassified bounds GET /admin/unclassified (and so the names a single
// reclassify from that list can carry).
const maxUnclassified = 200

// maxUnclassifiedCandidates bounds the names passed to the raw distinct-machine
// count, so a fleet with many thousands of one-off process names cannot turn
// one panel request into a huge `= ANY($3)` scan of process_sample.
const maxUnclassifiedCandidates = 1000

// topUnclassifiedNames keeps the at most max names that weigh most before the
// machine count is known: by CPU for sort=cpu, otherwise by sample count, ties
// broken by the other weight and then by name.
func topUnclassifiedNames(byName map[string]*model.UnclassifiedProcess, cpuSum map[string]float64, sortBy string, max int) map[string]struct{} {
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	if len(names) > max {
		sort.Slice(names, func(i, j int) bool {
			a, b := names[i], names[j]
			ca, cb := byName[a].Count, byName[b].Count
			sa, sb := cpuSum[a], cpuSum[b]
			if sortBy == "cpu" {
				if sa != sb {
					return sa > sb
				}
				if ca != cb {
					return ca > cb
				}
			} else {
				if ca != cb {
					return ca > cb
				}
				if sa != sb {
					return sa > sb
				}
			}
			return a < b
		})
		names = names[:max]
	}
	keep := make(map[string]struct{}, len(names))
	for _, n := range names {
		keep[n] = struct{}{}
	}
	return keep
}

// sortUnclassified orders by the chosen key, breaking ties by the other weights
// and finally by name so the list is stable between refreshes.
func sortUnclassified(list []model.UnclassifiedProcess, by string) {
	cpu := func(u model.UnclassifiedProcess) float64 { return u.CPUShare }
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		switch by {
		case "count":
			if a.Count != b.Count {
				return a.Count > b.Count
			}
		case "cpu":
			if cpu(a) != cpu(b) {
				return cpu(a) > cpu(b)
			}
		}
		if a.Machines != b.Machines {
			return a.Machines > b.Machines
		}
		if cpu(a) != cpu(b) {
			return cpu(a) > cpu(b)
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Name < b.Name
	})
}

// Reclassify handles POST /api/admin/reclassify: re-resolve process_sample.category_id
// over a recent window using the current classification map, then refresh the
// category rollups so the change is reflected immediately.
func (a *API) Reclassify(c echo.Context) error {
	ctx := c.Request().Context()
	// {window?}: "6h", "24h", "7d" … (default 24h, at most 30 days). A value that
	// does not parse used to fall back to 24h silently; it is now a 400.
	// {names?}: restrict the pass to these process names (at most 200) — the panel
	// sends the names a just-created rule matches, so applying one rule does not
	// rewrite the whole window.
	var body struct {
		Window string   `json:"window"`
		Names  []string `json:"names"`
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

	names, err := a.reclassifyNames(ctx, body.Names, from, to)
	if err != nil {
		return err
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
		"names":        len(names),
		"from":         from,
		"to":           to,
	})
}

// maxReclassifyNames bounds the optional names list of POST /admin/reclassify.
const maxReclassifyNames = maxUnclassified

// reclassifyNames returns the process names a reclassify pass covers: the
// validated, normalised request list when one was sent, otherwise every
// distinct name in the window.
func (a *API) reclassifyNames(ctx context.Context, req []string, from, to time.Time) ([]string, error) {
	if req != nil {
		if len(req) == 0 || len(req) > maxReclassifyNames {
			return nil, apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
				"names must list 1 to 200 process names", map[string]any{"field": "names"})
		}
		seen := map[string]bool{}
		out := make([]string, 0, len(req))
		for _, n := range req {
			if len(n) > maxMatchValueLen {
				return nil, apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
					"a process name is longer than 128 characters", map[string]any{"field": "names"})
			}
			// Stored names are normalised at ingest; normalise the same way.
			if norm := ingest.NormalizeName(n); norm != "" && !seen[norm] {
				seen[norm] = true
				out = append(out, norm)
			}
		}
		return out, nil
	}
	rows, err := a.pool.Query(ctx,
		`SELECT DISTINCT process_name FROM process_sample WHERE ts BETWEEN $1 AND $2`, from, to)
	if err != nil {
		return nil, serverError(err)
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, serverError(err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return nil, serverError(err)
	}
	return names, nil
}

// ── Alert rules ──────────────────────────────────────────────────────────────

// GetAlertRules handles GET /api/admin/alert-rules.
func (a *API) GetAlertRules(c echo.Context) error {
	rows, err := a.pool.Query(c.Request().Context(),
		`SELECT rule, enabled, warn_threshold, crit_threshold, for_seconds, params->'ignore_apps'
		 FROM alert_rule ORDER BY rule`)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out := []model.AlertRuleDTO{}
	for rows.Next() {
		var r model.AlertRuleDTO
		var ignore []byte
		if err := rows.Scan(&r.Rule, &r.Enabled, &r.WarnThreshold, &r.CritThreshold, &r.ForSeconds, &ignore); err != nil {
			return serverError(err)
		}
		if r.Rule == crashStormRule {
			apps := decodeIgnoreApps(ignore)
			r.IgnoreApps = &apps
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
	for i, r := range rules {
		if r.ForSeconds < 0 {
			return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
				"for_seconds must not be negative", map[string]any{"field": "for_seconds", "rule": r.Rule})
		}
		if r.IgnoreApps == nil {
			continue
		}
		if r.Rule != crashStormRule {
			return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
				"ignore_apps is only valid for crash_storm",
				map[string]any{"field": "ignore_apps", "rule": truncate(r.Rule, 64)})
		}
		apps, err := normalizeIgnoreApps(*r.IgnoreApps)
		if err != nil {
			return err
		}
		rules[i].IgnoreApps = &apps
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
		if r.IgnoreApps != nil {
			raw, err := json.Marshal(*r.IgnoreApps)
			if err != nil {
				return serverError(err)
			}
			if _, err := tx.Exec(ctx, `
				UPDATE alert_rule
				SET params = COALESCE(params, '{}'::jsonb) || jsonb_build_object('ignore_apps', $2::jsonb)
				WHERE rule=$1`, r.Rule, string(raw)); err != nil {
				return serverError(err)
			}
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
