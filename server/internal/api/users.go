package api

import (
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// userParam returns the {user} path param, URL-decoded (e.g. CORP%5Cayse -> CORP\ayse).
func userParam(c echo.Context) string {
	raw := c.Param("user")
	if dec, err := url.PathUnescape(raw); err == nil {
		return dec
	}
	return raw
}

// Users handles GET /api/users (current logon users from machine_live).
// An optional ?tag= filter restricts results to users whose profile carries that
// department tag; display name + tags are joined in from user_profile.
func (a *API) Users(c echo.Context) error {
	ctx := c.Request().Context()
	tag := c.QueryParam("tag")
	rows, err := a.pool.Query(ctx, `
		SELECT DISTINCT ON (ml.logon_user) ml.logon_user, ml.ts, m.hostname, up.display_name, up.tags
		FROM machine_live ml
		JOIN machine m ON m.machine_id = ml.machine_id
		LEFT JOIN user_profile up ON up.logon_user = ml.logon_user
		WHERE ml.logon_user IS NOT NULL AND ml.logon_user <> '' AND m.archived_at IS NULL
		  -- The Users directory lists people at domain PCs; a Linux server's local
		  -- SSH account is not one of them.
		  AND m.kind <> 'linux'
		  AND ($1 = '' OR $1 = ANY(up.tags))
		ORDER BY ml.logon_user, ml.ts DESC`, tag)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()

	warn := a.cfg.OfflineWarn()
	out := []model.UserSummary{}
	for rows.Next() {
		var u model.UserSummary
		var host, displayName *string
		var tags []string
		if err := rows.Scan(&u.LogonUser, &u.LastSeen, &host, &displayName, &tags); err != nil {
			return serverError(err)
		}
		if host != nil {
			u.CurrentMachine = *host
		}
		if displayName != nil {
			u.DisplayName = *displayName
		}
		if tags == nil {
			tags = []string{}
		}
		u.Tags = tags
		u.Online = time.Since(u.LastSeen) < warn
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// UserTimeline handles GET /api/users/{user}/timeline (usage_session across machines).
func (a *API) UserTimeline(c echo.Context) error {
	ctx := c.Request().Context()
	user := userParam(c)
	from, to := parseRange(c)
	rows, err := a.pool.Query(ctx, `
		SELECT us.machine_id, m.hostname, us.kind, us.started_at, us.ended_at
		FROM usage_session us JOIN machine m ON m.machine_id = us.machine_id
		WHERE us.logon_user=$1 AND us.started_at < $3 AND (us.ended_at IS NULL OR us.ended_at > $2)
		ORDER BY us.started_at DESC`, user, from, to)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out, err := scanSessions(rows)
	if err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// logonGap is the silence after which a user's presence on a machine counts as a
// new logon record (matches the usage_session heartbeat gap in the alerts engine).
const logonGap = 5 * time.Minute

// logonHistory is the default look-back for UserLogons: the full raw
// metric_sample retention window.
const logonHistory = 90 * 24 * time.Hour

// UserLogons handles GET /api/users/{user}/logons?from&to — one row per
// continuous stretch this user was the console user of a machine, newest first.
//
// It reads raw metric_sample rather than usage_session on purpose: usage_session
// is keyed per machine and keeps the user it was OPENED with, so a hand-over
// between two accounts with no logged-off sample in between stays attributed to
// the first one. For "which machines did this account sign in to" that is the
// wrong answer, so islands are built per (machine, user) straight from samples.
// Archived machines are included — this is history, not current state.
func (a *API) UserLogons(c echo.Context) error {
	ctx := c.Request().Context()
	user := userParam(c)
	from, to := parseRange(c)
	if c.QueryParam("from") == "" {
		from = to.Add(-logonHistory)
	}
	rows, err := a.pool.Query(ctx, `
		WITH s AS (
			SELECT machine_id, ts, session_state,
			       ts - lag(ts) OVER (PARTITION BY machine_id ORDER BY ts) AS gap
			FROM metric_sample
			WHERE logon_user = $1 AND ts >= $2 AND ts < $3
			  AND session_state IS DISTINCT FROM 'none'
		), g AS (
			SELECT machine_id, ts, session_state,
			       count(*) FILTER (WHERE gap IS NULL OR gap > make_interval(secs => $4))
			         OVER (PARTITION BY machine_id ORDER BY ts) AS grp
			FROM s
		)
		SELECT g.machine_id, COALESCE(m.hostname, g.machine_id),
		       min(g.ts), max(g.ts),
		       count(*) FILTER (WHERE g.session_state = 'active') * 15
		FROM g LEFT JOIN machine m ON m.machine_id = g.machine_id
		GROUP BY g.machine_id, m.hostname, g.grp
		ORDER BY min(g.ts) DESC
		LIMIT 5000`, user, from, to, logonGap.Seconds())
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()

	// A record whose last sample is within the gap of "now" is still running.
	openAfter := time.Now().Add(-logonGap)
	out := []model.UserLogon{}
	for rows.Next() {
		var l model.UserLogon
		var end time.Time
		if err := rows.Scan(&l.MachineID, &l.Hostname, &l.StartedAt, &end, &l.ActiveSeconds); err != nil {
			return serverError(err)
		}
		if end.Before(openAfter) {
			l.EndedAt = &end
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// UserNetwork handles GET /api/users/{user}/network (net_user_1h).
func (a *API) UserNetwork(c echo.Context) error {
	ctx := c.Request().Context()
	user := userParam(c)
	from, to := parseRange(c)
	rows, err := a.pool.Query(ctx, `
		SELECT bucket, sent_bytes, recv_bytes FROM net_user_1h
		WHERE logon_user=$1 AND bucket BETWEEN $2 AND $3 ORDER BY bucket`, user, from, to)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out := []model.NetPoint{}
	for rows.Next() {
		var p model.NetPoint
		var sent, recv *int64
		if err := rows.Scan(&p.T, &sent, &recv); err != nil {
			return serverError(err)
		}
		p.Sent = iv(sent)
		p.Recv = iv(recv)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// UserCategories handles GET /api/users/{user}/categories (proc_cat_user_1h).
func (a *API) UserCategories(c echo.Context) error {
	ctx := c.Request().Context()
	user := userParam(c)
	from, to := parseRange(c)
	rows, err := a.pool.Query(ctx, `
		SELECT category_id, sum(cpu_sum)::float8, sum(samples)::bigint FROM proc_cat_user_1h
		WHERE logon_user=$1 AND bucket BETWEEN $2 AND $3 GROUP BY category_id`, user, from, to)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out := []model.CategoryShare{}
	for rows.Next() {
		var catID *int16
		var cs *float64
		var samples int64
		if err := rows.Scan(&catID, &cs, &samples); err != nil {
			return serverError(err)
		}
		out = append(out, model.CategoryShare{Category: a.catName(catID), CPUSum: fv(cs), Samples: samples})
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, mergeCategories(out))
}

// UserApps handles GET /api/users/{user}/apps — foreground (active-use) time per
// application over the range, top 20 by seconds. NAME ONLY: this reflects which
// applications the user actively used and for how long, never window titles/URLs.
func (a *API) UserApps(c echo.Context) error {
	ctx := c.Request().Context()
	user := userParam(c)
	from, to := parseRange(c)
	rows, err := a.pool.Query(ctx, `
		SELECT app_name, sum(seconds)::bigint AS s
		FROM app_focus_sample
		WHERE logon_user=$1 AND ts >= $2 AND ts < $3
		GROUP BY app_name ORDER BY s DESC LIMIT 20`, user, from, to)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out := []model.AppUsage{}
	for rows.Next() {
		var au model.AppUsage
		if err := rows.Scan(&au.Name, &au.Seconds); err != nil {
			return serverError(err)
		}
		out = append(out, au)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// isSystemCat reports whether a category is OS/system overhead, which we exclude
// from usage breakdowns so real application usage is legible.
func isSystemCat(name string) bool {
	return strings.EqualFold(name, "system") || strings.EqualFold(name, "sistem")
}

// mergeCategories collapses CategoryShare rows that resolve to the same category
// NAME (many process_category ids share e.g. "system"), DROPS the system/OS slice,
// and recomputes shares over the remaining (non-system) total.
func mergeCategories(in []model.CategoryShare) []model.CategoryShare {
	byName := map[string]*model.CategoryShare{}
	order := []string{}
	var total float64
	for _, s := range in {
		if isSystemCat(s.Category) {
			continue // exclude OS/system overhead from the usage breakdown
		}
		c := byName[s.Category]
		if c == nil {
			c = &model.CategoryShare{Category: s.Category}
			byName[s.Category] = c
			order = append(order, s.Category)
		}
		c.CPUSum += s.CPUSum
		c.Samples += s.Samples
		total += s.CPUSum
	}
	out := make([]model.CategoryShare, 0, len(order))
	for _, n := range order {
		c := byName[n]
		if total > 0 {
			c.Share = c.CPUSum / total
		}
		out = append(out, *c)
	}
	return out
}

// dayAcc accumulates per-local-date active + logged-on minutes.
type dayAcc struct {
	active   float64
	loggedOn float64
}

// UserActivity handles GET /api/users/{user}/activity?from&to.
// It derives a weekday×hour ACTIVE-minute heatmap plus a per-day active/logged-on
// breakdown from usage_session, all bucketed in Europe/Istanbul local time.
// Privacy: this is active-hours only — no window titles or URLs are involved.
func (a *API) UserActivity(c echo.Context) error {
	ctx := c.Request().Context()
	user := userParam(c)
	from, to := parseRange(c)

	loc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		return serverError(err)
	}

	rows, err := a.pool.Query(ctx, `
		SELECT kind, started_at, COALESCE(ended_at, now())
		FROM usage_session
		WHERE logon_user=$1 AND kind IN ('active','logged_on')
		  AND started_at < $3 AND (ended_at IS NULL OR ended_at > $2)
		ORDER BY started_at`, user, from, to)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()

	var heat [7][24]float64
	daily := map[string]*dayAcc{}
	var totalActive, totalLogged float64

	for rows.Next() {
		var kind string
		var start, end time.Time
		if err := rows.Scan(&kind, &start, &end); err != nil {
			return serverError(err)
		}
		// Clamp the session to the requested window.
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		if !end.After(start) {
			continue
		}
		active := kind == "active"
		walkHourSegments(start, end, loc, func(local time.Time, minutes float64) {
			date := local.Format("2006-01-02")
			d := daily[date]
			if d == nil {
				d = &dayAcc{}
				daily[date] = d
			}
			if active {
				wd := (int(local.Weekday()) + 6) % 7 // Mon=0 .. Sun=6
				heat[wd][local.Hour()] += minutes
				d.active += minutes
				totalActive += minutes
			} else {
				d.loggedOn += minutes
				totalLogged += minutes
			}
		})
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}

	resp := model.ActivityResponse{
		Heatmap:            []model.HeatmapCell{},
		Daily:              []model.DailyActivity{},
		TotalActiveMinutes: int(math.Round(totalActive)),
		TotalLoggedMinutes: int(math.Round(totalLogged)),
	}
	for wd := 0; wd < 7; wd++ {
		for h := 0; h < 24; h++ {
			if m := int(math.Round(heat[wd][h])); m > 0 {
				resp.Heatmap = append(resp.Heatmap, model.HeatmapCell{Weekday: wd, Hour: h, ActiveMinutes: m})
			}
		}
	}
	dates := make([]string, 0, len(daily))
	for d := range daily {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	for _, d := range dates {
		acc := daily[d]
		resp.Daily = append(resp.Daily, model.DailyActivity{
			Date:            d,
			ActiveMinutes:   int(math.Round(acc.active)),
			LoggedOnMinutes: int(math.Round(acc.loggedOn)),
		})
	}
	return c.JSON(http.StatusOK, resp)
}

// walkHourSegments splits [start,end] at local-hour boundaries and invokes fn once
// per segment with the segment's local start time and its duration in minutes.
// Because it cuts on the hour, each segment falls entirely within a single local
// hour/date/weekday. Recomputing the local time each step keeps it DST-correct.
func walkHourSegments(start, end time.Time, loc *time.Location, fn func(local time.Time, minutes float64)) {
	for cur := start; cur.Before(end); {
		local := cur.In(loc)
		nextHour := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), 0, 0, 0, loc).Add(time.Hour)
		segEnd := nextHour
		if segEnd.After(end) {
			segEnd = end
		}
		fn(local, segEnd.Sub(cur).Minutes())
		cur = segEnd
	}
}
