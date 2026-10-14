package api

import (
	"math"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// tagParam returns the {tag} path param, URL-decoded (mirrors userParam).
func tagParam(c echo.Context) string {
	raw := c.Param("tag")
	if dec, err := url.PathUnescape(raw); err == nil {
		return dec
	}
	return raw
}

// AdminSetUserProfile handles PUT /api/admin/users/{user} (admin only): upsert the
// display name + department tags for a logon user. Returns the saved profile.
func (a *API) AdminSetUserProfile(c echo.Context) error {
	ctx := c.Request().Context()
	user := userParam(c)
	// Full replace: both keys are required. A loose bind let an empty or
	// mis-shaped body wipe the display name and every department tag with 200.
	var raw struct {
		DisplayName *string   `json:"display_name"`
		Tags        *[]string `json:"tags"`
	}
	if err := apierr.Bind(c, &raw); err != nil {
		return err
	}
	if raw.DisplayName == nil {
		return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
			"display_name is required", map[string]any{"field": "display_name"})
	}
	if raw.Tags == nil {
		return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
			"tags is required", map[string]any{"field": "tags"})
	}
	body := model.UserProfileUpdate{DisplayName: strings.TrimSpace(*raw.DisplayName), Tags: []string{}}
	seen := map[string]struct{}{}
	for _, t := range *raw.Tags {
		t = strings.TrimSpace(t)
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
		body.Tags = append(body.Tags, t)
	}

	var resp struct {
		LogonUser   string   `json:"logon_user"`
		DisplayName string   `json:"display_name"`
		Tags        []string `json:"tags"`
	}
	var displayName *string
	err := a.pool.QueryRow(ctx, `
		INSERT INTO user_profile (logon_user, display_name, tags, updated_at)
		VALUES ($1,$2,$3,now())
		ON CONFLICT (logon_user) DO UPDATE
		  SET display_name=EXCLUDED.display_name, tags=EXCLUDED.tags, updated_at=now()
		RETURNING logon_user, display_name, tags`,
		user, body.DisplayName, body.Tags).Scan(&resp.LogonUser, &displayName, &resp.Tags)
	if err != nil {
		return serverError(err)
	}
	if displayName != nil {
		resp.DisplayName = *displayName
	}
	if resp.Tags == nil {
		resp.Tags = []string{}
	}
	return c.JSON(http.StatusOK, resp)
}

// AdminTags handles GET /api/admin/tags (admin only): every distinct tag in use
// plus how many users carry it.
func (a *API) AdminTags(c echo.Context) error {
	rows, err := a.pool.Query(c.Request().Context(), `
		SELECT t.tag, count(*)::int
		FROM user_profile up, unnest(up.tags) AS t(tag)
		GROUP BY t.tag
		ORDER BY t.tag`)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out := []model.TagInfo{}
	for rows.Next() {
		var t model.TagInfo
		if err := rows.Scan(&t.Tag, &t.UserCount); err != nil {
			return serverError(err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// TagSummary handles GET /api/tags/{tag}/summary?from&to: for every user in a tag,
// their active minutes + top program category over the window, plus the category
// totals summed across the whole tag.
func (a *API) TagSummary(c echo.Context) error {
	ctx := c.Request().Context()
	tag := tagParam(c)
	from, to := parseRange(c)

	resp := model.TagSummary{
		Tag:            tag,
		Users:          []model.TagUserRow{},
		CategoryTotals: []model.CategoryShare{},
	}

	// 1) Users carrying the tag (ordered for a stable response).
	uRows, err := a.pool.Query(ctx,
		`SELECT logon_user, display_name FROM user_profile WHERE $1 = ANY(tags) ORDER BY logon_user`, tag)
	if err != nil {
		return serverError(err)
	}
	type tagUser struct {
		logon   string
		display string
	}
	users := []tagUser{}
	for uRows.Next() {
		var u tagUser
		var dn *string
		if err := uRows.Scan(&u.logon, &dn); err != nil {
			uRows.Close()
			return serverError(err)
		}
		if dn != nil {
			u.display = *dn
		}
		users = append(users, u)
	}
	uRows.Close()
	if err := uRows.Err(); err != nil {
		return serverError(err)
	}
	if len(users) == 0 {
		return c.JSON(http.StatusOK, resp)
	}
	logons := make([]string, len(users))
	for i, u := range users {
		logons[i] = u.logon
	}

	// 2) Active minutes per user (single grouped query, clamped to the window).
	activeMin := map[string]float64{}
	amRows, err := a.pool.Query(ctx, `
		SELECT logon_user,
		       sum(EXTRACT(EPOCH FROM (LEAST(COALESCE(ended_at, now()), $3) - GREATEST(started_at, $2))) / 60)::float8
		FROM usage_session
		WHERE kind='active' AND logon_user = ANY($1)
		  AND started_at < $3 AND (ended_at IS NULL OR ended_at > $2)
		GROUP BY logon_user`, logons, from, to)
	if err != nil {
		return serverError(err)
	}
	for amRows.Next() {
		var u string
		var mins *float64
		if err := amRows.Scan(&u, &mins); err != nil {
			amRows.Close()
			return serverError(err)
		}
		activeMin[u] = fv(mins)
	}
	amRows.Close()
	if err := amRows.Err(); err != nil {
		return serverError(err)
	}

	// 3) Top category per user (highest summed cpu over the window).
	topCat := map[string]string{}
	tcRows, err := a.pool.Query(ctx, `
		SELECT DISTINCT ON (logon_user) logon_user, category_id
		FROM (
			SELECT logon_user, category_id, sum(cpu_sum) AS cs
			FROM proc_cat_user_1h
			WHERE logon_user = ANY($1) AND bucket BETWEEN $2 AND $3
			GROUP BY logon_user, category_id
		) s
		ORDER BY logon_user, cs DESC`, logons, from, to)
	if err != nil {
		return serverError(err)
	}
	for tcRows.Next() {
		var u string
		var catID *int16
		if err := tcRows.Scan(&u, &catID); err != nil {
			tcRows.Close()
			return serverError(err)
		}
		topCat[u] = a.catName(catID)
	}
	tcRows.Close()
	if err := tcRows.Err(); err != nil {
		return serverError(err)
	}

	// 4) Category totals summed across all users in the tag.
	ctRows, err := a.pool.Query(ctx, `
		SELECT category_id, sum(cpu_sum)::float8, sum(samples)::bigint
		FROM proc_cat_user_1h
		WHERE logon_user = ANY($1) AND bucket BETWEEN $2 AND $3
		GROUP BY category_id`, logons, from, to)
	if err != nil {
		return serverError(err)
	}
	for ctRows.Next() {
		var catID *int16
		var cs *float64
		var samples int64
		if err := ctRows.Scan(&catID, &cs, &samples); err != nil {
			ctRows.Close()
			return serverError(err)
		}
		resp.CategoryTotals = append(resp.CategoryTotals, model.CategoryShare{
			Category: a.catName(catID), CPUSum: fv(cs), Samples: samples,
		})
	}
	ctRows.Close()
	if err := ctRows.Err(); err != nil {
		return serverError(err)
	}
	resp.CategoryTotals = mergeCategories(resp.CategoryTotals)

	// Assemble user rows in the stable order from step 1.
	for _, u := range users {
		resp.Users = append(resp.Users, model.TagUserRow{
			LogonUser:     u.logon,
			DisplayName:   u.display,
			ActiveMinutes: int(math.Round(activeMin[u.logon])),
			TopCategory:   topCat[u.logon],
		})
	}
	return c.JSON(http.StatusOK, resp)
}
