package auth

import (
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
)

// First-run setup: creating the first administrator from the browser.
//
// A fresh install has no panel account. Whoever reaches the port first must not
// be able to claim the panel, so creating the first admin needs a one-time SETUP
// CODE that only the person who installed the server can see: it is either set
// by them (SETUP_CODE) or generated at startup and printed to the server log.
// The code lives in memory only, as a hash, and dies when the admin is created.

const (
	// PanelUserLockKey is the pg_advisory_xact_lock key serialising every write
	// that changes the set of panel accounts ("tolikopa").
	PanelUserLockKey int64 = 0x746f6c696b6f7061

	// MaxUsernameLen caps admin_user.username (characters).
	MaxUsernameLen = 64

	// 32 symbols (no I, O, 0, 1): 5 bits per character, no modulo bias.
	setupAlphabet   = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	setupCodeGroups = 4
	setupGroupLen   = 4 // 16 characters = 80 bits
	minSetupCodeLen = 8 // for an operator-supplied SETUP_CODE
)

// setupState is the in-memory first-run state.
type setupState struct {
	mu       sync.Mutex
	codeHash *[32]byte   // nil = no code armed
	hasAdmin atomic.Bool // latched true once any account is known to exist
}

// ValidUsername allows letters and digits of any script plus . _ - @, so a name
// is always safe in a URL path segment, a log line and an otpauth label.
func ValidUsername(s string) bool {
	if s == "" || utf8.RuneCountInString(s) > MaxUsernameLen {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '.', '_', '-', '@':
			continue
		}
		return false
	}
	return true
}

// normalizeSetupCode strips presentation: case, dashes and whitespace.
func normalizeSetupCode(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if r == '-' || unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func hashSetupCode(code string) [32]byte {
	return sha256.Sum256([]byte("tolikopa/setup-code/v1\x00" + normalizeSetupCode(code)))
}

// newSetupCode returns a fresh code in display form (XXXX-XXXX-XXXX-XXXX).
func newSetupCode() string {
	raw := make([]byte, setupCodeGroups*setupGroupLen)
	_, _ = crand.Read(raw)
	var b strings.Builder
	for i, c := range raw {
		if i > 0 && i%setupGroupLen == 0 {
			b.WriteByte('-')
		}
		b.WriteByte(setupAlphabet[c&31])
	}
	return b.String()
}

// armSetupCode stores the code's hash; "" disarms.
func (a *Auth) armSetupCode(code string) {
	a.setup.mu.Lock()
	defer a.setup.mu.Unlock()
	if code == "" {
		a.setup.codeHash = nil
		return
	}
	h := hashSetupCode(code)
	a.setup.codeHash = &h
}

// checkSetupCode compares in constant time. With no code armed nothing matches.
func (a *Auth) checkSetupCode(given string) bool {
	a.setup.mu.Lock()
	want := a.setup.codeHash
	a.setup.mu.Unlock()
	got := hashSetupCode(given)
	if want == nil {
		// Still do a comparison so "no code armed" is not faster than "wrong code".
		var zero [32]byte
		subtle.ConstantTimeCompare(got[:], zero[:])
		return false
	}
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}

// NeedsAdmin reports whether the panel has no account at all (first run). Once
// an account has been seen the answer is latched: the API can never delete the
// last administrator.
func (a *Auth) NeedsAdmin(ctx context.Context) (bool, error) {
	if a.setup.hasAdmin.Load() {
		return false, nil
	}
	var any bool
	if err := a.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM admin_user)`).Scan(&any); err != nil {
		return false, err
	}
	if any {
		a.setup.hasAdmin.Store(true)
	}
	return !any, nil
}

// InitSetup arms the first-run setup code when the panel has no account. Call it
// once at startup, after SeedAdmin. The code is taken from SETUP_CODE when set;
// otherwise one is generated and printed in a block that is hard to miss in
// `docker compose logs`. A generated code is the ONLY secret this package ever
// logs, and it is worthless as soon as the first admin exists.
func (a *Auth) InitSetup(ctx context.Context) error {
	needs, err := a.NeedsAdmin(ctx)
	if err != nil {
		return err
	}
	if !needs {
		return nil
	}
	if env := strings.TrimSpace(a.cfg.SetupCode); env != "" {
		if len(normalizeSetupCode(env)) >= minSetupCodeLen {
			a.armSetupCode(env)
			log.Printf("auth: first-run setup is open; the setup code is the value of SETUP_CODE")
			return nil
		}
		log.Printf("auth: SETUP_CODE is shorter than %d characters and was IGNORED; a code was generated instead", minSetupCodeLen)
	}
	code := newSetupCode()
	a.armSetupCode(code)
	log.Printf("\n"+
		"==================================================================\n"+
		"  TOLIKOPA FIRST-RUN SETUP\n"+
		"\n"+
		"  No administrator account exists yet. Open the panel and enter\n"+
		"  this one-time setup code to create it:\n"+
		"\n"+
		"      %s\n"+
		"\n"+
		"  The code changes on every restart until the account is created.\n"+
		"==================================================================", code)
	return nil
}

// SetupAdmin handles POST /api/setup/admin
// {setup_code, username, password, display_name?, lang?}: create the FIRST
// administrator and sign them in (same body and cookies as a login).
func (a *Auth) SetupAdmin(c echo.Context) error {
	ctx := c.Request().Context()
	var body struct {
		SetupCode   string `json:"setup_code"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		Lang        string `json:"lang"`
	}
	if err := c.Bind(&body); err != nil {
		return apierr.BadRequest("invalid request body")
	}
	done := apierr.New(http.StatusConflict, apierr.SetupAlreadyDone, "an administrator account already exists")

	needs, err := a.NeedsAdmin(ctx)
	if err != nil {
		return apierr.Internal("auth", err)
	}
	if !needs {
		return done
	}

	// Guessing the code is limited per client address, with the login lockout.
	key := limitKey("setup", "", c.RealIP())
	if wait := a.limiter.blocked(key); wait > 0 {
		return tooMany(c, wait)
	}
	if !a.checkSetupCode(body.SetupCode) {
		a.limiter.fail(key)
		return apierr.New(http.StatusUnauthorized, apierr.SetupCodeInvalid, "invalid setup code")
	}

	body.Username = strings.TrimSpace(body.Username)
	if !ValidUsername(body.Username) {
		return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
			"username must be 1-64 letters, digits or . _ - @",
			map[string]any{"field": "username", "max_length": MaxUsernameLen})
	}
	if err := ValidatePassword(body.Password); err != nil {
		return err
	}
	displayName, err := CleanDisplayName(body.DisplayName)
	if err != nil {
		return err
	}
	if body.Lang != "" && !ValidLang(body.Lang) {
		return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
			"lang must be tr or en", map[string]any{"field": "lang"})
	}
	prefs := map[string]string{}
	if body.Lang != "" {
		prefs["lang"] = body.Lang
	}
	prefsJSON, _ := json.Marshal(prefs)
	hash, err := HashPassword(body.Password)
	if err != nil {
		return apierr.Internal("auth", err)
	}

	// Race-safe: the advisory lock makes "table is empty" and the insert one
	// step, so two simultaneous requests cannot both create a first admin.
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return apierr.Internal("auth", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, PanelUserLockKey); err != nil {
		return apierr.Internal("auth", err)
	}
	ac := &account{Username: body.Username, Role: "admin"}
	err = tx.QueryRow(ctx, `
		INSERT INTO admin_user (username, password_hash, role, display_name, prefs)
		SELECT $1, $2, 'admin', $3, $4::jsonb
		WHERE NOT EXISTS (SELECT 1 FROM admin_user)
		RETURNING id, session_epoch`,
		body.Username, hash, displayName, prefsJSON).Scan(&ac.ID, &ac.Epoch)
	if errors.Is(err, pgx.ErrNoRows) {
		a.setup.hasAdmin.Store(true)
		a.armSetupCode("")
		return done
	}
	if err != nil {
		return apierr.Internal("auth", err)
	}
	// The language picked on the first screen also becomes the instance default
	// (login page, alert e-mails); it can be changed in general settings.
	if body.Lang != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO app_setting (key, value, updated_at) VALUES ('default_lang', to_jsonb($1::text), now())
			ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`, body.Lang); err != nil {
			return apierr.Internal("auth", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return apierr.Internal("auth", err)
	}

	// The code has done its job; it must never work again.
	a.setup.hasAdmin.Store(true)
	a.armSetupCode("")
	a.limiter.reset(key)
	log.Printf("auth: first administrator %q created through first-run setup from %s", ac.Username, c.RealIP())
	return a.finishLogin(c, ac)
}
