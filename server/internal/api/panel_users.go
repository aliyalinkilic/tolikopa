package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/auth"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// Panel-user management (admin only), /api/admin/panel-users. "Panel users" are
// the accounts that sign in to this panel (admin_user) — not the domain users
// whose machines are monitored (those are /api/users and /api/admin/users/:user).

const (
	panelUserCols = `id, username, display_name, role, disabled, totp_enabled, created_at, last_login_at`

	maxUsernameLen = auth.MaxUsernameLen

	// panelUserLock serialises every write that can change the set of enabled
	// admins (pg_advisory_xact_lock). Without it two admins demoting each other
	// at the same instant would each see "one other admin remains" and leave none.
	// Shared with first-run setup, which creates the first account under it.
	panelUserLock = auth.PanelUserLockKey
)

func scanPanelUser(row pgx.Row) (model.PanelUser, error) {
	var u model.PanelUser
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.Disabled,
		&u.TOTPEnabled, &u.CreatedAt, &u.LastLoginAt)
	return u, err
}

// usernameParam returns the {username} path param, URL-decoded.
func usernameParam(c echo.Context) string {
	raw := c.Param("username")
	if dec, err := url.PathUnescape(raw); err == nil {
		return dec
	}
	return raw
}

func validRole(r string) bool { return r == "admin" || r == "viewer" }

// validUsername allows letters and digits of any script plus . _ - @, so a name
// is always safe in a URL path segment, a log line and an otpauth label.
func validUsername(s string) bool { return auth.ValidUsername(s) }

// adminChange describes a pending change to a panel account, for the guards.
type adminChange struct {
	Self                 bool // the actor is changing their own account
	SelfForbidden        bool // the change is one nobody may apply to themself
	TargetIsEnabledAdmin bool // target is an enabled admin BEFORE the change
	RemovesAdmin         bool // after the change the target is no longer an enabled admin
	OtherEnabledAdmins   int  // enabled admins other than the target
}

// guardAdminChange returns the error code that blocks the change, or "".
//
//   - cannot_modify_self: an admin may not delete, disable or demote their own
//     account, nor reset their own password or 2FA through the admin endpoints
//     (those skip re-authentication; /api/me/* is the path that asks for it).
//   - last_admin: the instance must always keep at least one enabled admin.
func guardAdminChange(ch adminChange) string {
	if ch.Self && ch.SelfForbidden {
		return apierr.CannotModifySelf
	}
	if ch.TargetIsEnabledAdmin && ch.RemovesAdmin && ch.OtherEnabledAdmins == 0 {
		return apierr.LastAdmin
	}
	return ""
}

func guardError(code string) error {
	switch code {
	case apierr.CannotModifySelf:
		return apierr.New(http.StatusConflict, code, "you cannot apply this change to your own account")
	case apierr.LastAdmin:
		return apierr.New(http.StatusConflict, code, "at least one enabled administrator must remain")
	}
	return nil
}

func userNotFound() error {
	return apierr.New(http.StatusNotFound, apierr.UserNotFound, "panel user not found")
}

// actorOf returns the acting admin's username and account id.
func actorOf(c echo.Context) (string, int64) {
	if claims, ok := auth.ClaimsFrom(c); ok && claims != nil {
		return claims.Subject, claims.UID
	}
	return "", 0
}

// lockTarget takes the panel-user lock and loads the target row FOR UPDATE,
// plus the number of OTHER enabled admins. Must run inside tx.
func lockTarget(ctx context.Context, tx pgx.Tx, username string) (model.PanelUser, int, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, panelUserLock); err != nil {
		return model.PanelUser{}, 0, err
	}
	u, err := scanPanelUser(tx.QueryRow(ctx,
		`SELECT `+panelUserCols+` FROM admin_user WHERE username=$1 FOR UPDATE`, username))
	if err != nil {
		return u, 0, err
	}
	var others int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM admin_user WHERE role='admin' AND NOT disabled AND id<>$1`, u.ID,
	).Scan(&others); err != nil {
		return u, 0, err
	}
	return u, others, nil
}

// ListPanelUsers handles GET /api/admin/panel-users (alias: GET /api/admin/users).
func (a *API) ListPanelUsers(c echo.Context) error {
	rows, err := a.pool.Query(c.Request().Context(),
		`SELECT `+panelUserCols+` FROM admin_user ORDER BY lower(username), username`)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out := []model.PanelUser{}
	for rows.Next() {
		u, err := scanPanelUser(rows)
		if err != nil {
			return serverError(err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// CreatePanelUser handles POST /api/admin/panel-users (alias: POST /api/admin/users).
func (a *API) CreatePanelUser(c echo.Context) error {
	ctx := c.Request().Context()
	var body struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		Role        string `json:"role"`
		DisplayName string `json:"display_name"`
	}
	if err := apierr.Bind(c, &body); err != nil {
		return err
	}
	body.Username = strings.TrimSpace(body.Username)
	if !validUsername(body.Username) {
		return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
			"username must be 1-64 letters, digits or . _ - @",
			map[string]any{"field": "username", "max_length": maxUsernameLen})
	}
	if body.Role == "" {
		body.Role = "viewer"
	}
	if !validRole(body.Role) {
		return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
			"role must be admin or viewer", map[string]any{"field": "role"})
	}
	if err := auth.ValidatePassword(body.Password); err != nil {
		return err
	}
	displayName, err := auth.CleanDisplayName(body.DisplayName)
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		return serverError(err)
	}

	taken := apierr.New(http.StatusConflict, apierr.UsernameTaken, "username is already in use")
	// The UNIQUE constraint is case-sensitive; refuse names that differ only by
	// case so "Admin" cannot be created next to "admin".
	u, err := scanPanelUser(a.pool.QueryRow(ctx, `
		INSERT INTO admin_user (username, password_hash, role, display_name)
		SELECT $1, $2, $3, $4
		WHERE NOT EXISTS (SELECT 1 FROM admin_user WHERE lower(username) = lower($1))
		RETURNING `+panelUserCols,
		body.Username, hash, body.Role, displayName))
	if errors.Is(err, pgx.ErrNoRows) {
		return taken
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation (concurrent create)
		return taken
	}
	if err != nil {
		return serverError(err)
	}
	actor, _ := actorOf(c)
	log.Printf("panel-users: %q created %q (role=%s)", actor, u.Username, u.Role)
	return c.JSON(http.StatusCreated, u)
}

// PatchPanelUser handles PATCH /api/admin/panel-users/{username}
// {role?, disabled?, display_name?}. Returns the updated account.
func (a *API) PatchPanelUser(c echo.Context) error {
	ctx := c.Request().Context()
	var body struct {
		Role        *string `json:"role"`
		Disabled    *bool   `json:"disabled"`
		DisplayName *string `json:"display_name"`
	}
	if err := apierr.Bind(c, &body); err != nil {
		return err
	}
	if body.Role == nil && body.Disabled == nil && body.DisplayName == nil {
		return apierr.BadRequest("no field to change in request body")
	}
	if body.Role != nil && !validRole(*body.Role) {
		return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
			"role must be admin or viewer", map[string]any{"field": "role"})
	}
	if body.DisplayName != nil {
		clean, err := auth.CleanDisplayName(*body.DisplayName)
		if err != nil {
			return err
		}
		body.DisplayName = &clean
	}
	actor, actorID := actorOf(c)

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return serverError(err)
	}
	defer tx.Rollback(ctx)

	target, others, err := lockTarget(ctx, tx, usernameParam(c))
	if errors.Is(err, pgx.ErrNoRows) {
		return userNotFound()
	}
	if err != nil {
		return serverError(err)
	}

	demotes := body.Role != nil && *body.Role != "admin" && target.Role == "admin"
	disables := body.Disabled != nil && *body.Disabled && !target.Disabled
	if code := guardAdminChange(adminChange{
		Self:                 target.ID == actorID,
		SelfForbidden:        demotes || disables,
		TargetIsEnabledAdmin: target.Role == "admin" && !target.Disabled,
		RemovesAdmin:         demotes || disables,
		OtherEnabledAdmins:   others,
	}); code != "" {
		return guardError(code)
	}

	// Disabling ends the user's sessions (epoch bump). A role change needs no
	// bump: the role is re-read from this table on every request.
	bump := 0
	if disables {
		bump = 1
	}
	updated, err := scanPanelUser(tx.QueryRow(ctx, `
		UPDATE admin_user SET
			role          = COALESCE($2::text, role),
			disabled      = COALESCE($3::boolean, disabled),
			display_name  = COALESCE($4::text, display_name),
			session_epoch = session_epoch + $5::int
		WHERE id=$1
		RETURNING `+panelUserCols,
		target.ID, body.Role, body.Disabled, body.DisplayName, bump))
	if err != nil {
		return serverError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return serverError(err)
	}
	if disables {
		a.auth.SessionsRevoked(target.Username)
	}
	log.Printf("panel-users: %q updated %q (role=%s disabled=%t)", actor, updated.Username, updated.Role, updated.Disabled)
	return c.JSON(http.StatusOK, updated)
}

// ResetPanelUserPassword handles POST /api/admin/panel-users/{username}/reset-password
// {new_password}. Every session of the target is invalidated.
func (a *API) ResetPanelUserPassword(c echo.Context) error {
	ctx := c.Request().Context()
	var body struct {
		NewPassword string `json:"new_password"`
	}
	if err := apierr.Bind(c, &body); err != nil {
		return err
	}
	username := usernameParam(c)
	actor, actorID := actorOf(c)

	var targetID int64
	err := a.pool.QueryRow(ctx, `SELECT id FROM admin_user WHERE username=$1`, username).Scan(&targetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return userNotFound()
	}
	if err != nil {
		return serverError(err)
	}
	// Own password goes through PUT /api/me/password, which demands the current
	// one; this endpoint would let a hijacked admin session take the account over.
	if code := guardAdminChange(adminChange{Self: targetID == actorID, SelfForbidden: true}); code != "" {
		return guardError(code)
	}
	if err := auth.ValidatePassword(body.NewPassword); err != nil {
		return err
	}
	hash, err := auth.HashPassword(body.NewPassword)
	if err != nil {
		return serverError(err)
	}
	tag, err := a.pool.Exec(ctx,
		`UPDATE admin_user SET password_hash=$2, session_epoch=session_epoch+1 WHERE id=$1`, targetID, hash)
	if err != nil {
		return serverError(err)
	}
	if tag.RowsAffected() == 0 {
		return userNotFound()
	}
	a.auth.SessionsRevoked(username)
	log.Printf("panel-users: %q reset the password of %q", actor, username)
	return c.NoContent(http.StatusNoContent)
}

// ResetPanelUser2FA handles POST /api/admin/panel-users/{username}/reset-2fa:
// switch two-factor off for a user who lost their authenticator.
func (a *API) ResetPanelUser2FA(c echo.Context) error {
	ctx := c.Request().Context()
	username := usernameParam(c)
	actor, actorID := actorOf(c)

	var targetID int64
	err := a.pool.QueryRow(ctx, `SELECT id FROM admin_user WHERE username=$1`, username).Scan(&targetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return userNotFound()
	}
	if err != nil {
		return serverError(err)
	}
	// Own 2FA is switched off via POST /api/me/2fa/disable (password + code).
	if code := guardAdminChange(adminChange{Self: targetID == actorID, SelfForbidden: true}); code != "" {
		return guardError(code)
	}
	tag, err := a.pool.Exec(ctx, auth.ClearTOTPSQL+` WHERE id=$1`, targetID)
	if err != nil {
		return serverError(err)
	}
	if tag.RowsAffected() == 0 {
		return userNotFound()
	}
	// Under the "require 2FA" policy the user is now back in forced enrolment;
	// REST is checked per request, a live WebSocket has to be cut.
	// (If the policy cannot be read, cut the socket anyway: the periodic
	// re-validation would do it within a minute.)
	if required, err := a.auth.Require2FA(ctx); err != nil || required {
		a.auth.SessionsRevoked(username)
	}
	log.Printf("panel-users: %q reset two-factor of %q", actor, username)
	return c.NoContent(http.StatusNoContent)
}

// DeletePanelUser handles DELETE /api/admin/panel-users/{username}.
func (a *API) DeletePanelUser(c echo.Context) error {
	ctx := c.Request().Context()
	actor, actorID := actorOf(c)

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return serverError(err)
	}
	defer tx.Rollback(ctx)

	target, others, err := lockTarget(ctx, tx, usernameParam(c))
	if errors.Is(err, pgx.ErrNoRows) {
		return userNotFound()
	}
	if err != nil {
		return serverError(err)
	}
	if code := guardAdminChange(adminChange{
		Self:                 target.ID == actorID,
		SelfForbidden:        true,
		TargetIsEnabledAdmin: target.Role == "admin" && !target.Disabled,
		RemovesAdmin:         true,
		OtherEnabledAdmins:   others,
	}); code != "" {
		return guardError(code)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM admin_user WHERE id=$1`, target.ID); err != nil {
		return serverError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return serverError(err)
	}
	a.auth.SessionsRevoked(target.Username)
	log.Printf("panel-users: %q deleted %q", actor, target.Username)
	return c.NoContent(http.StatusNoContent)
}
