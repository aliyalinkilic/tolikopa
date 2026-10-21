package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// crash_storm ignore list (alert_rule.params.ignore_apps) and the crash-app
// suggestions the panel offers for it.

const crashStormRule = "crash_storm"

// Limits for crash_storm ignore_apps.
const (
	maxIgnoreApps   = 100
	maxIgnoreAppLen = 128
)

// Limits for GET /api/admin/crash-apps.
const (
	crashAppsDefaultWindow = 7 * 24 * time.Hour
	crashAppsMaxWindow     = 30 * 24 * time.Hour
	crashAppsDefaultLimit  = 20
	crashAppsMaxLimit      = 50
)

// canonicalApp is the form the alert engine compares ignore_apps against
// (alerts.ignoredApps): trimmed, lower-case, without a trailing ".exe".
func canonicalApp(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".exe")
}

// normalizeIgnoreApps validates and canonicalises an ignore list: each entry a
// bare process base name (no path separator, no control characters, at most
// maxIgnoreAppLen bytes), empties dropped, duplicates removed (first wins), at
// most maxIgnoreApps entries. Never returns nil.
func normalizeIgnoreApps(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range in {
		app := canonicalApp(raw)
		if app == "" {
			continue
		}
		if len(app) > maxIgnoreAppLen {
			return nil, apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
				"ignore_apps entry is too long",
				map[string]any{"field": "ignore_apps", "rule": crashStormRule, "max": maxIgnoreAppLen})
		}
		if strings.ContainsAny(app, `/\`) ||
			strings.ContainsFunc(app, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return nil, apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
				"ignore_apps entries must be bare process base names",
				map[string]any{"field": "ignore_apps", "rule": crashStormRule})
		}
		if seen[app] {
			continue
		}
		seen[app] = true
		out = append(out, app)
	}
	if len(out) > maxIgnoreApps {
		return nil, apierr.WithParams(http.StatusBadRequest, "too_many_items",
			"too many ignore_apps entries",
			map[string]any{"field": "ignore_apps", "rule": crashStormRule, "max": maxIgnoreApps})
	}
	return out, nil
}

// decodeIgnoreApps reads a stored params.ignore_apps value leniently (what
// the engine would honour): non-strings and empties are skipped, entries are
// canonicalised and de-duplicated. Never returns nil.
func decodeIgnoreApps(raw []byte) []string {
	out := []string{}
	var list []any
	if len(raw) == 0 || json.Unmarshal(raw, &list) != nil {
		return out
	}
	seen := map[string]bool{}
	for _, v := range list {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if app := canonicalApp(s); app != "" && !seen[app] {
			seen[app] = true
			out = append(out, app)
		}
	}
	return out
}

// CrashApps handles GET /api/admin/crash-apps?window=7d&limit=20: the faulting
// processes behind app_crash/app_hang events of non-archived machines in the
// window, noisiest first, so an admin can add a crash-looping app to the
// crash_storm ignore list in one click. Only base names leave the server —
// the same field the alert itself carries.
func (a *API) CrashApps(c echo.Context) error {
	ctx := c.Request().Context()

	win := parseWindow(c.QueryParam("window"), crashAppsDefaultWindow)
	if win > crashAppsMaxWindow {
		win = crashAppsMaxWindow
	}
	limit := queryInt(c, "limit", crashAppsDefaultLimit)
	if limit < 1 {
		limit = 1
	} else if limit > crashAppsMaxLimit {
		limit = crashAppsMaxLimit
	}

	var ignoreRaw []byte
	if err := a.pool.QueryRow(ctx,
		`SELECT params->'ignore_apps' FROM alert_rule WHERE rule = $1`, crashStormRule).Scan(&ignoreRaw); err != nil &&
		!errors.Is(err, pgx.ErrNoRows) { // a missing rule row only means nothing is ignored
		return serverError(err)
	}
	ignored := map[string]bool{}
	for _, app := range decodeIgnoreApps(ignoreRaw) {
		ignored[app] = true
	}

	rows, err := a.pool.Query(ctx, `
		SELECT regexp_replace(lower(btrim(e.process_name)), '\.exe$', '') AS app,
		       count(*)::int, count(DISTINCT e.machine_id)::int
		FROM event e
		JOIN machine m ON m.machine_id = e.machine_id AND m.archived_at IS NULL
		WHERE e.kind IN ('app_crash', 'app_hang')
		  AND e.ts >= $1
		  AND COALESCE(btrim(e.process_name), '') <> ''
		GROUP BY 1
		ORDER BY 2 DESC, 3 DESC, 1
		LIMIT $2`, time.Now().Add(-win), limit)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()

	out := model.CrashAppsResponse{WindowSeconds: int(win / time.Second), Apps: []model.CrashAppDTO{}}
	for rows.Next() {
		var d model.CrashAppDTO
		if err := rows.Scan(&d.App, &d.Crashes, &d.Machines); err != nil {
			return serverError(err)
		}
		if d.App == "" {
			continue
		}
		d.Ignored = ignored[d.App]
		out.Apps = append(out.Apps, d)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}
