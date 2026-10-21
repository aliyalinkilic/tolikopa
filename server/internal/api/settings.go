package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/auth"
	"github.com/aliyalinkilic/tolikopa/server/internal/buildinfo"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
	"github.com/aliyalinkilic/tolikopa/server/internal/workhours"
)

// General settings live in app_setting (key text PK, value jsonb). Keys used
// here: org_name (string), default_lang ("tr" | "en"), require_2fa (boolean),
// work_hours (object, internal/workhours).

const (
	settingOrgName     = "org_name"
	settingDefaultLang = "default_lang"
	maxOrgNameLen      = 120
)

// loadGeneral reads the general settings, falling back to the defaults for a
// missing or malformed row (org_name "", default_lang "tr").
func (a *API) loadGeneral(ctx context.Context) (model.GeneralSettings, error) {
	out := model.GeneralSettings{OrgName: "", DefaultLang: "tr", WorkHours: workhours.Default()}
	rows, err := a.pool.Query(ctx,
		`SELECT key, value FROM app_setting WHERE key = ANY($1)`,
		[]string{settingOrgName, settingDefaultLang, auth.SettingRequire2FA, workhours.SettingKey})
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return out, err
		}
		if key == workhours.SettingKey {
			out.WorkHours = workhours.Decode(raw)
			continue
		}
		if key == auth.SettingRequire2FA {
			var b bool
			if json.Unmarshal(raw, &b) == nil {
				out.Require2FA = b
			}
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			continue
		}
		switch key {
		case settingOrgName:
			out.OrgName = s
		case settingDefaultLang:
			if auth.ValidLang(s) {
				out.DefaultLang = s
			}
		}
	}
	return out, rows.Err()
}

// PublicSettings handles GET /api/settings/public. NO authentication: the login
// page needs the organisation name and default language before anyone signs in.
// Only values that are safe to show to an anonymous visitor belong here.
func (a *API) PublicSettings(c echo.Context) error {
	g, err := a.loadGeneral(c.Request().Context())
	if err != nil {
		return serverError(err)
	}
	needs, err := a.auth.NeedsAdmin(c.Request().Context())
	if err != nil {
		return serverError(err)
	}
	_, completed, err := a.loadSetup(c.Request().Context())
	if err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, model.PublicSettings{
		OrgName: g.OrgName, DefaultLang: g.DefaultLang, Version: buildinfo.Version,
		Require2FA: g.Require2FA, NeedsAdmin: needs, SetupComplete: completed != nil,
	})
}

// GetGeneralSettings handles GET /api/admin/settings/general.
func (a *API) GetGeneralSettings(c echo.Context) error {
	g, err := a.loadGeneral(c.Request().Context())
	if err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, g)
}

// PutGeneralSettings handles PUT /api/admin/settings/general
// {org_name, default_lang, require_2fa, work_hours}. A field that is omitted
// keeps its current value; work_hours, when present, replaces the whole schedule
// (every field required except stale_after_days, which keeps its current value
// when omitted; validated by workhours.Normalize).
//
// require_2fa can only be switched ON by an admin who has two-factor enabled
// themself (409 enable_own_2fa_first): the person imposing the rule is then
// never the one it locks into enrolment, and has demonstrably working 2FA.
func (a *API) PutGeneralSettings(c echo.Context) error {
	ctx := c.Request().Context()
	var body struct {
		OrgName     *string `json:"org_name"`
		DefaultLang *string `json:"default_lang"`
		Require2FA  *bool   `json:"require_2fa"`
		WorkHours   *struct {
			Days                *[]int    `json:"days"`
			Start               *string   `json:"start"`
			End                 *string   `json:"end"`
			Tags                *[]string `json:"tags"`
			GraceMinutes        *int      `json:"grace_minutes"`
			OfflineAfterMinutes *int      `json:"offline_after_minutes"`
			StaleAfterDays      *int      `json:"stale_after_days"`
		} `json:"work_hours"`
	}
	if err := apierr.Bind(c, &body); err != nil {
		return err
	}
	if body.OrgName == nil && body.DefaultLang == nil && body.Require2FA == nil && body.WorkHours == nil {
		return apierr.BadRequest("no settings field in request body")
	}
	cur, err := a.loadGeneral(ctx)
	if err != nil {
		return serverError(err)
	}
	var workHours *model.WorkHours
	if wh := body.WorkHours; wh != nil {
		missing := ""
		switch {
		case wh.Days == nil:
			missing = "days"
		case wh.Start == nil:
			missing = "start"
		case wh.End == nil:
			missing = "end"
		case wh.Tags == nil:
			missing = "tags"
		case wh.GraceMinutes == nil:
			missing = "grace_minutes"
		case wh.OfflineAfterMinutes == nil:
			missing = "offline_after_minutes"
		}
		if missing != "" {
			return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
				"work_hours."+missing+" is required", map[string]any{"field": "work_hours." + missing})
		}
		stale := cur.WorkHours.StaleAfterDays
		if wh.StaleAfterDays != nil {
			stale = *wh.StaleAfterDays
		}
		n, err := workhours.Normalize(model.WorkHours{
			Days: *wh.Days, Start: *wh.Start, End: *wh.End, Tags: *wh.Tags,
			GraceMinutes: *wh.GraceMinutes, OfflineAfterMinutes: *wh.OfflineAfterMinutes,
			StaleAfterDays: stale,
		})
		if err != nil {
			field := "work_hours"
			var inv *workhours.ErrInvalid
			if errors.As(err, &inv) {
				field += "." + inv.Field
			}
			return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest, err.Error(),
				map[string]any{"field": field})
		}
		workHours = &n
	}
	if body.OrgName != nil {
		name := strings.TrimSpace(*body.OrgName)
		if utf8.RuneCountInString(name) > maxOrgNameLen || strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
				"org_name is too long or contains control characters",
				map[string]any{"field": "org_name", "max_length": maxOrgNameLen})
		}
		cur.OrgName = name
	}
	if body.DefaultLang != nil {
		if !auth.ValidLang(*body.DefaultLang) {
			return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
				"default_lang must be tr or en", map[string]any{"field": "default_lang"})
		}
		cur.DefaultLang = *body.DefaultLang
	}
	turningOn := body.Require2FA != nil && *body.Require2FA && !cur.Require2FA
	if turningOn {
		_, actorID := actorOf(c)
		var own bool
		if err := a.pool.QueryRow(ctx,
			`SELECT totp_enabled FROM admin_user WHERE id=$1`, actorID).Scan(&own); err != nil {
			return serverError(err)
		}
		if !own {
			return apierr.New(http.StatusConflict, apierr.EnableOwn2FAFirst,
				"enable two-factor authentication on your own account before requiring it for everyone")
		}
	}
	if body.Require2FA != nil {
		cur.Require2FA = *body.Require2FA
	}
	if _, err := a.pool.Exec(ctx, `
		INSERT INTO app_setting (key, value, updated_at) VALUES
			($1, to_jsonb($2::text), now()),
			($3, to_jsonb($4::text), now()),
			($5, to_jsonb($6::boolean), now())
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`,
		settingOrgName, cur.OrgName, settingDefaultLang, cur.DefaultLang,
		auth.SettingRequire2FA, cur.Require2FA); err != nil {
		return serverError(err)
	}
	if workHours != nil {
		raw, _ := json.Marshal(workHours)
		if _, err := a.pool.Exec(ctx, `
			INSERT INTO app_setting (key, value, updated_at) VALUES ($1, $2::jsonb, now())
			ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`,
			workhours.SettingKey, raw); err != nil {
			return serverError(err)
		}
		actor, _ := actorOf(c)
		log.Printf("settings: %q changed work hours to %v %s–%s tags=%v stale_after_days=%d", actor, workHours.Days, workHours.Start, workHours.End, workHours.Tags, workHours.StaleAfterDays)
	}
	a.auth.InvalidatePolicy()
	if turningOn {
		actor, _ := actorOf(c)
		log.Printf("settings: %q turned require_2fa on", actor)
		// Live WebSocket connections were authenticated before the rule existed;
		// drop those of accounts that now have to enrol. REST needs nothing — it is
		// checked per request.
		a.disconnectUnenrolled(ctx)
	} else if body.Require2FA != nil && !*body.Require2FA {
		actor, _ := actorOf(c)
		log.Printf("settings: %q set require_2fa off", actor)
	}
	return c.NoContent(http.StatusNoContent)
}

// disconnectUnenrolled closes the live connections of every account without
// two-factor. Best effort: a failure only delays the cut-off to the next reconnect.
func (a *API) disconnectUnenrolled(ctx context.Context) {
	rows, err := a.pool.Query(ctx, `SELECT username FROM admin_user WHERE NOT totp_enabled`)
	if err != nil {
		log.Printf("settings: listing accounts without 2FA: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var u string
		if rows.Scan(&u) == nil {
			a.auth.SessionsRevoked(u)
		}
	}
}
