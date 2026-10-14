package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// MaxDisplayNameLen caps admin_user.display_name (characters).
const MaxDisplayNameLen = 80

// CleanDisplayName trims a display name and enforces the length cap. Control
// characters are rejected: the value is rendered in the panel and in logs.
func CleanDisplayName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxDisplayNameLen {
		return "", apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
			"display_name is too long", map[string]any{"field": "display_name", "max_length": MaxDisplayNameLen})
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
				"display_name contains control characters", map[string]any{"field": "display_name"})
		}
	}
	return s, nil
}

// session returns the authenticated claims or a session_required error.
func session(c echo.Context) (*Claims, error) {
	claims, ok := ClaimsFrom(c)
	if !ok || claims == nil {
		return nil, apierr.New(http.StatusUnauthorized, apierr.SessionRequired, "sign-in required")
	}
	return claims, nil
}

// self loads the calling user's account row.
func (a *Auth) self(c echo.Context) (*Claims, *account, error) {
	claims, err := session(c)
	if err != nil {
		return nil, nil, err
	}
	ac, err := a.loadAccount(c.Request().Context(), claims.Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, apierr.New(http.StatusUnauthorized, apierr.SessionInvalid, "session is invalid or expired")
	}
	if err != nil {
		return nil, nil, apierr.Internal("auth", err)
	}
	return claims, ac, nil
}

// Me handles GET /api/auth/me.
func (a *Auth) Me(c echo.Context) error {
	claims, err := session(c)
	if err != nil {
		return err
	}
	var (
		out   model.MeResponse
		prefs []byte
	)
	err = a.pool.QueryRow(c.Request().Context(), `
		SELECT username, role, display_name, totp_enabled, prefs, created_at, last_login_at
		FROM admin_user WHERE username=$1`, claims.Subject,
	).Scan(&out.Username, &out.Role, &out.DisplayName, &out.TOTPEnabled, &prefs, &out.CreatedAt, &out.LastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.New(http.StatusUnauthorized, apierr.SessionInvalid, "session is invalid or expired")
	}
	if err != nil {
		return apierr.Internal("auth", err)
	}
	out.Prefs = decodePrefs(prefs)
	if !out.TOTPEnabled {
		required, err := a.Require2FA(c.Request().Context())
		if err != nil {
			return apierr.Internal("auth", err)
		}
		out.MFAEnrollmentRequired = required
	}
	return c.JSON(http.StatusOK, out)
}

// decodePrefs maps the stored jsonb to the response shape. An unset or
// unrecognised theme is null — distinct from an explicit "system", so the UI
// can tell "never chose" from "chose System" — and an unset language is null ("follow the
// instance default").
func decodePrefs(raw []byte) model.UserPrefs {
	p := model.UserPrefs{}
	var stored struct {
		Theme string `json:"theme"`
		Lang  string `json:"lang"`
	}
	if len(raw) > 0 && json.Unmarshal(raw, &stored) == nil {
		if validTheme(stored.Theme) {
			th := stored.Theme
			p.Theme = &th
		}
		if ValidLang(stored.Lang) {
			l := stored.Lang
			p.Lang = &l
		}
	}
	return p
}

func validTheme(s string) bool { return s == "dark" || s == "light" || s == "system" }

// ValidLang reports whether s is a language the panel ships.
func ValidLang(s string) bool { return s == "tr" || s == "en" }

// PutProfile handles PUT /api/me/profile {display_name}.
func (a *Auth) PutProfile(c echo.Context) error {
	claims, err := session(c)
	if err != nil {
		return err
	}
	var body struct {
		DisplayName *string `json:"display_name"`
	}
	if err := c.Bind(&body); err != nil || body.DisplayName == nil {
		return apierr.BadRequest("display_name is required")
	}
	name, err := CleanDisplayName(*body.DisplayName)
	if err != nil {
		return err
	}
	if _, err := a.pool.Exec(c.Request().Context(),
		`UPDATE admin_user SET display_name=$2 WHERE id=$1`, claims.UID, name); err != nil {
		return apierr.Internal("auth", err)
	}
	return c.NoContent(http.StatusNoContent)
}

// PutPrefs handles PUT /api/me/prefs {theme, lang}. A key that is absent is left
// unchanged; an explicit null clears it (theme reads back as null, lang falls back to
// the instance default).
func (a *Auth) PutPrefs(c echo.Context) error {
	claims, err := session(c)
	if err != nil {
		return err
	}
	var body map[string]json.RawMessage
	if err := c.Bind(&body); err != nil {
		return apierr.BadRequest("invalid request body")
	}
	set := map[string]string{}
	del := []string{}
	for _, key := range []string{"theme", "lang"} {
		raw, present := body[key]
		if !present {
			continue
		}
		if string(raw) == "null" {
			del = append(del, key)
			continue
		}
		var v string
		if json.Unmarshal(raw, &v) != nil ||
			(key == "theme" && !validTheme(v)) || (key == "lang" && !ValidLang(v)) {
			return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
				"invalid value for "+key, map[string]any{"field": key})
		}
		set[key] = v
	}
	setJSON, _ := json.Marshal(set)
	if _, err := a.pool.Exec(c.Request().Context(),
		`UPDATE admin_user SET prefs = (prefs - $2::text[]) || $3::jsonb WHERE id=$1`,
		claims.UID, del, setJSON); err != nil {
		return apierr.Internal("auth", err)
	}
	return c.NoContent(http.StatusNoContent)
}

// reauthKey is the attempt budget for password/code re-entry by an already
// signed-in user. Without it a hijacked session could brute-force the current
// password or a TOTP code through the self-service endpoints.
func reauthKey(c echo.Context, username string) string {
	return limitKey("reauth", username, c.RealIP())
}

// PutPassword handles PUT /api/me/password {current_password, new_password}.
// Every other session of the user is invalidated; the caller's cookie is
// re-issued (same CSRF nonce) so the tab that made the change stays signed in.
func (a *Auth) PutPassword(c echo.Context) error {
	var body struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := c.Bind(&body); err != nil {
		return apierr.BadRequest("invalid request body")
	}
	claims, ac, err := a.self(c)
	if err != nil {
		return err
	}
	key := reauthKey(c, ac.Username)
	if wait := a.limiter.blocked(key); wait > 0 {
		return tooMany(c, wait)
	}
	// 400, not 401: the session is fine, and the panel treats 401 as "signed out".
	if bcrypt.CompareHashAndPassword([]byte(ac.Hash), []byte(body.Current)) != nil {
		a.limiter.fail(key)
		return apierr.New(http.StatusBadRequest, apierr.PasswordIncorrect, "current password is incorrect")
	}
	a.limiter.reset(key)

	if err := ValidatePassword(body.New); err != nil {
		return err
	}
	if body.New == body.Current {
		return apierr.WithParams(http.StatusBadRequest, apierr.PasswordTooWeak,
			"new password must differ from the current one", map[string]any{"reason": "same_as_current"})
	}
	hash, err := HashPassword(body.New)
	if err != nil {
		return apierr.Internal("auth", err)
	}
	var epoch int
	if err := a.pool.QueryRow(c.Request().Context(), `
		UPDATE admin_user SET password_hash=$2, session_epoch=session_epoch+1
		WHERE id=$1 RETURNING session_epoch`, ac.ID, hash).Scan(&epoch); err != nil {
		return apierr.Internal("auth", err)
	}
	if err := a.setSession(c, ac.Username, ac.Role, ac.ID, epoch, claims.CSRF); err != nil {
		return apierr.Internal("auth", err)
	}
	log.Printf("auth: password changed by %q", ac.Username)
	a.SessionsRevoked(ac.Username)
	return c.NoContent(http.StatusNoContent)
}

// ── Two-factor (TOTP) ────────────────────────────────────────────────────────

// totpAAD binds a sealed TOTP secret to its account row.
func totpAAD(id int64) []byte { return []byte("totp:" + strconv.FormatInt(id, 10)) }

// TOTPSetup handles POST /api/me/2fa/setup {password}: generate and store a
// PENDING secret. Nothing is enabled until TOTPEnable proves the authenticator
// holds it. Calling it again replaces the pending secret.
//
// The password is required because this is where the secret is handed out: a
// stolen session cookie alone must not be enough to bind the attacker's
// authenticator to an account that has no second factor yet.
func (a *Auth) TOTPSetup(c echo.Context) error {
	var body struct {
		Password string `json:"password"`
	}
	if err := c.Bind(&body); err != nil {
		return apierr.BadRequest("invalid request body")
	}
	_, ac, err := a.self(c)
	if err != nil {
		return err
	}
	if ac.TOTPEnabled {
		return apierr.New(http.StatusConflict, apierr.MFAAlreadyEnabled, "two-factor authentication is already enabled")
	}
	key := reauthKey(c, ac.Username)
	if wait := a.limiter.blocked(key); wait > 0 {
		return tooMany(c, wait)
	}
	// 400, not 401: the session is fine, and the panel treats 401 as "signed out".
	if bcrypt.CompareHashAndPassword([]byte(ac.Hash), []byte(body.Password)) != nil {
		a.limiter.fail(key)
		return apierr.New(http.StatusBadRequest, apierr.PasswordIncorrect, "password is incorrect")
	}
	a.limiter.reset(key)
	secret := newTOTPSecret()
	enc, err := seal(a.keys.totp, secret, totpAAD(ac.ID))
	if err != nil {
		return apierr.Internal("auth", err)
	}
	if _, err := a.pool.Exec(c.Request().Context(),
		`UPDATE admin_user SET totp_pending_enc=$2 WHERE id=$1 AND NOT totp_enabled`, ac.ID, enc); err != nil {
		return apierr.Internal("auth", err)
	}
	b := b32.EncodeToString(secret)
	noStore(c)
	return c.JSON(http.StatusOK, model.TOTPSetupResponse{Secret: b, OTPAuthURL: otpauthURL(ac.Username, b)})
}

// TOTPEnable handles POST /api/me/2fa/enable {code}: verify against the pending
// secret, switch 2FA on and return the recovery codes (shown exactly once).
// Other sessions of the user are invalidated — whoever else holds a cookie for
// this account must now pass the second factor too.
func (a *Auth) TOTPEnable(c echo.Context) error {
	var body struct {
		Code string `json:"code"`
	}
	if err := c.Bind(&body); err != nil {
		return apierr.BadRequest("invalid request body")
	}
	claims, ac, err := a.self(c)
	if err != nil {
		return err
	}
	if ac.TOTPEnabled {
		return apierr.New(http.StatusConflict, apierr.MFAAlreadyEnabled, "two-factor authentication is already enabled")
	}
	if len(ac.PendingEnc) == 0 {
		return apierr.New(http.StatusConflict, apierr.MFASetupRequired, "start two-factor setup first")
	}
	key := reauthKey(c, ac.Username)
	if wait := a.mfaBlocked(key, ac.Username); wait > 0 {
		return tooMany(c, wait)
	}
	secret, err := open(a.keys.totp, ac.PendingEnc, totpAAD(ac.ID))
	if err != nil {
		// Pending secret sealed under a previous JWT_SECRET: setup must be redone.
		return apierr.New(http.StatusConflict, apierr.MFASetupRequired, "start two-factor setup first")
	}
	step, ok := verifyTOTP(secret, cleanCode(body.Code), time.Now(), 0)
	if !ok {
		a.mfaFailed(key, ac.Username)
		return apierr.New(http.StatusBadRequest, apierr.MFAInvalid, "invalid verification code")
	}
	a.mfaPassed(key, ac.Username)

	plain, hashes := newRecoveryCodes(a.keys.recovery)
	var epoch int
	err = a.pool.QueryRow(c.Request().Context(), `
		UPDATE admin_user
		SET totp_secret_enc=totp_pending_enc, totp_pending_enc=NULL, totp_enabled=true,
		    totp_last_step=$2, recovery_codes=$3, session_epoch=session_epoch+1
		WHERE id=$1 AND NOT totp_enabled AND totp_pending_enc IS NOT NULL
		RETURNING session_epoch`, ac.ID, step, hashes).Scan(&epoch)
	if errors.Is(err, pgx.ErrNoRows) {
		// Lost a race with another enable/reset for the same account.
		return apierr.New(http.StatusConflict, apierr.MFAAlreadyEnabled, "two-factor authentication is already enabled")
	}
	if err != nil {
		return apierr.Internal("auth", err)
	}
	if err := a.setSession(c, ac.Username, ac.Role, ac.ID, epoch, claims.CSRF); err != nil {
		return apierr.Internal("auth", err)
	}
	log.Printf("auth: two-factor enabled by %q", ac.Username)
	a.SessionsRevoked(ac.Username)
	noStore(c)
	return c.JSON(http.StatusOK, model.RecoveryCodesResponse{RecoveryCodes: plain})
}

// reauth verifies password + second factor for the sensitive 2FA operations.
func (a *Auth) reauth(c echo.Context, ac *account, password, code string) error {
	if !ac.TOTPEnabled {
		return apierr.New(http.StatusConflict, apierr.MFANotEnabled, "two-factor authentication is not enabled")
	}
	key := reauthKey(c, ac.Username)
	if wait := a.mfaBlocked(key, ac.Username); wait > 0 {
		return tooMany(c, wait)
	}
	if bcrypt.CompareHashAndPassword([]byte(ac.Hash), []byte(password)) != nil {
		// A wrong password counts against this address only; the per-account
		// budget is for the second factor.
		a.limiter.fail(key)
		return apierr.New(http.StatusBadRequest, apierr.PasswordIncorrect, "password is incorrect")
	}
	ok, err := a.checkSecondFactor(c.Request().Context(), ac, code)
	if err != nil {
		return apierr.Internal("auth", err)
	}
	if !ok {
		a.mfaFailed(key, ac.Username)
		return apierr.New(http.StatusBadRequest, apierr.MFAInvalid, "invalid verification code")
	}
	a.mfaPassed(key, ac.Username)
	return nil
}

type reauthBody struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

// TOTPDisable handles POST /api/me/2fa/disable {password, code}.
func (a *Auth) TOTPDisable(c echo.Context) error {
	var body reauthBody
	if err := c.Bind(&body); err != nil {
		return apierr.BadRequest("invalid request body")
	}
	_, ac, err := a.self(c)
	if err != nil {
		return err
	}
	// While the instance requires 2FA nobody may switch their own off. (An admin
	// reset-2fa still works; that user is then back in forced enrolment.)
	if ac.TOTPEnabled {
		required, err := a.Require2FA(c.Request().Context())
		if err != nil {
			return apierr.Internal("auth", err)
		}
		if required {
			return apierr.New(http.StatusConflict, apierr.MFARequiredByPolicy,
				"two-factor authentication is required on this instance and cannot be disabled")
		}
	}
	if err := a.reauth(c, ac, body.Password, body.Code); err != nil {
		return err
	}
	if _, err := a.pool.Exec(c.Request().Context(), ClearTOTPSQL+` WHERE id=$1`, ac.ID); err != nil {
		return apierr.Internal("auth", err)
	}
	log.Printf("auth: two-factor disabled by %q", ac.Username)
	return c.NoContent(http.StatusNoContent)
}

// ClearTOTPSQL is the UPDATE prefix that returns an account to "no 2FA". Shared
// with the admin reset-2fa handler so the two can never drift apart.
const ClearTOTPSQL = `UPDATE admin_user SET totp_enabled=false, totp_secret_enc=NULL,
	totp_pending_enc=NULL, totp_last_step=0, recovery_codes='{}'`

// TOTPRecoveryCodes handles POST /api/me/2fa/recovery-codes {password, code}:
// issue a fresh set; every earlier code stops working.
func (a *Auth) TOTPRecoveryCodes(c echo.Context) error {
	var body reauthBody
	if err := c.Bind(&body); err != nil {
		return apierr.BadRequest("invalid request body")
	}
	_, ac, err := a.self(c)
	if err != nil {
		return err
	}
	if err := a.reauth(c, ac, body.Password, body.Code); err != nil {
		return err
	}
	plain, hashes := newRecoveryCodes(a.keys.recovery)
	tag, err := a.pool.Exec(c.Request().Context(),
		`UPDATE admin_user SET recovery_codes=$2 WHERE id=$1 AND totp_enabled`, ac.ID, hashes)
	if err != nil {
		return apierr.Internal("auth", err)
	}
	if tag.RowsAffected() == 0 {
		return apierr.New(http.StatusConflict, apierr.MFANotEnabled, "two-factor authentication is not enabled")
	}
	log.Printf("auth: recovery codes regenerated by %q", ac.Username)
	noStore(c)
	return c.JSON(http.StatusOK, model.RecoveryCodesResponse{RecoveryCodes: plain})
}

// cleanCode strips the whitespace people type or paste around a code.
func cleanCode(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// checkSecondFactor verifies a 6-digit TOTP code or a recovery code for ac and
// CONSUMES it: the TOTP time step is recorded so the same code cannot be
// replayed, a recovery code is deleted. Both writes are conditional, so of two
// concurrent requests presenting the same code exactly one succeeds.
func (a *Auth) checkSecondFactor(ctx context.Context, ac *account, code string) (bool, error) {
	code = cleanCode(code)
	if isDigits(code, totpDigits) {
		secret, err := open(a.keys.totp, ac.SecretEnc, totpAAD(ac.ID))
		if err != nil {
			// The secret no longer opens — JWT_SECRET was rotated. The account
			// needs an admin 2FA reset (docs/SECURITY.md); log that, not the data.
			log.Printf("auth: stored TOTP secret for user id %d cannot be decrypted (JWT_SECRET rotated?)", ac.ID)
			return false, nil
		}
		step, ok := verifyTOTP(secret, code, time.Now(), ac.LastStep)
		if !ok {
			return false, nil
		}
		tag, err := a.pool.Exec(ctx,
			`UPDATE admin_user SET totp_last_step=$2 WHERE id=$1 AND totp_enabled AND totp_last_step < $2`,
			ac.ID, step)
		if err != nil {
			return false, err
		}
		return tag.RowsAffected() == 1, nil
	}

	used, ok := matchRecovery(ac.Recovery, a.keys.recovery, code)
	if !ok {
		return false, nil
	}
	tag, err := a.pool.Exec(ctx, `
		UPDATE admin_user SET recovery_codes = array_remove(recovery_codes, $2::text)
		WHERE id=$1 AND totp_enabled AND $2::text = ANY(recovery_codes)`, ac.ID, used)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
