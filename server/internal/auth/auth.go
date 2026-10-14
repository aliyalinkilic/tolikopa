// Package auth implements admin-panel authentication: bcrypt password hashing,
// HS256 JWT sessions stored in an httpOnly+Secure+SameSite cookie, a CSRF token
// echoed on writes, TOTP two-factor (RFC 6238) with recovery codes, and the
// self-service account endpoints under /api/me. It also seeds the first admin
// account from env.
//
// Session model. The JWT is not trusted on its own: every request re-reads one
// admin_user row and rejects the token unless the account still exists (same
// id), is enabled, and its session_epoch equals the one in the token. Bumping
// session_epoch therefore logs a user out everywhere at once, and the role is
// always the database's, so a demotion applies to the very next request. Logout
// revokes the one token it was called with by listing its jti in revoked_session.
//
// Nothing in this package logs a password, a TOTP code or secret, a recovery
// code, or a token.
package auth

import (
	"context"
	crand "crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/config"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

const (
	cookieName     = "tolikopa_session"
	csrfCookieName = "tolikopa_csrf"
	csrfHeader     = "X-CSRF-Token"
	sessionTTL     = 12 * time.Hour
	mfaTokenTTL    = 5 * time.Minute
	bcryptCost     = 12
	contextClaims  = "tolikopa_claims"

	// Audiences keep the two token kinds apart: a session verifier demands
	// audSession, the MFA step demands audMFA. They are also signed with
	// different keys (see keyring), so the separation does not rest on one check.
	audSession = "tolikopa:session"
	audMFA     = "tolikopa:mfa"

	// MinPasswordLen is the minimum length, in characters, of a panel password.
	MinPasswordLen = 10
	// loginIPMaxFailures is how many wrong passwords one client address may
	// send, across ALL usernames, before it is locked out. The per-(username,
	// IP) budget alone lets one address spray a common password over every
	// account name; this caps the spray without locking any account.
	loginIPMaxFailures = 20

	// maxPasswordBytes is bcrypt's input limit; longer input is rejected rather
	// than silently truncated.
	maxPasswordBytes = 72
)

// Claims is the session JWT payload. Subject holds the username. Role is
// overwritten from the database on every request — the copy in the token is
// informational only.
type Claims struct {
	Role  string `json:"role"`
	CSRF  string `json:"csrf"`
	UID   int64  `json:"uid"` // admin_user.id — a recreated username is a different account
	Epoch int    `json:"ep"`  // admin_user.session_epoch at issue time
	jwt.RegisteredClaims

	// EnrollmentRequired is NOT part of the token. authenticate sets it per
	// request when the instance requires 2FA and this account has none yet.
	EnrollmentRequired bool `json:"-"`
}

// mfaClaims is the payload of the challenge token returned by a password-only
// login on a 2FA account. It carries no role and no CSRF nonce and can never
// pass VerifyToken.
type mfaClaims struct {
	UID   int64 `json:"uid"`
	Epoch int   `json:"ep"`
	jwt.RegisteredClaims
}

// Auth wires the pool + config for the auth handlers and middleware.
type Auth struct {
	pool    *pgxpool.Pool
	cfg     *config.Config
	keys    keyring
	limiter *limiter

	policy policyCache
	setup  setupState

	dummyOnce sync.Once
	dummyHash []byte

	revokedMu sync.RWMutex
	onRevoked func(username string)
}

func New(pool *pgxpool.Pool, cfg *config.Config) *Auth {
	return &Auth{pool: pool, cfg: cfg, keys: deriveKeys(cfg.JWTSecret), limiter: newLimiter()}
}

// OnSessionsRevoked registers a callback fired whenever a user's sessions are
// invalidated (password change/reset, disable, delete, 2FA enrolment). The
// WebSocket hub uses it to drop that user's live connections, which are
// authenticated only once, at upgrade.
func (a *Auth) OnSessionsRevoked(fn func(username string)) {
	a.revokedMu.Lock()
	a.onRevoked = fn
	a.revokedMu.Unlock()
}

// SessionsRevoked notifies the registered callback. Call it after committing a
// change that bumps session_epoch or removes/disables the account.
func (a *Auth) SessionsRevoked(username string) {
	// Every caller has just changed the credentials or state an attacker's
	// guesses were aimed at (new password, disabled, deleted, 2FA re-enrolled),
	// so the per-account second-factor lock no longer protects anything and
	// would only keep the legitimate user out.
	a.limiter.reset(mfaAccountKey(username))

	a.revokedMu.RLock()
	fn := a.onRevoked
	a.revokedMu.RUnlock()
	if fn != nil {
		fn(username)
	}
}

// mfaAccountKey is the IP-INDEPENDENT attempt budget for an account's second
// factor. The per-(username, IP) budget alone gives someone who holds the
// password five code guesses per source address, and LAN addresses are cheap;
// this one caps guesses per account no matter where they come from.
//
// The price is deliberate: whoever has the password can keep the second step
// locked for the real user. That person already has half the credentials — the
// answer is a password change (which clears this lock), not more guesses.
func mfaAccountKey(username string) string { return limitKey("mfa-account", username, "") }

// mfaBlocked returns the longer of the two locks that apply to a second-factor
// attempt: per (username, IP) and per account.
func (a *Auth) mfaBlocked(ipKey, username string) time.Duration {
	wait := a.limiter.blocked(ipKey)
	if w := a.limiter.blocked(mfaAccountKey(username)); w > wait {
		wait = w
	}
	return wait
}

// mfaFailed records a wrong code against both budgets.
func (a *Auth) mfaFailed(ipKey, username string) {
	a.limiter.fail(ipKey)
	a.limiter.fail(mfaAccountKey(username))
}

// mfaPassed clears both budgets after a correct code.
func (a *Auth) mfaPassed(ipKey, username string) {
	a.limiter.reset(ipKey)
	a.limiter.reset(mfaAccountKey(username))
}

// SeedAdmin creates the first admin account from ADMIN_USERNAME/ADMIN_PASSWORD,
// and ONLY when admin_user is completely empty.
//
// It used to seed whenever the env username was absent. Accounts can be deleted
// now, so that rule resurrected a deliberately deleted shared "admin" at the
// next restart — with the .env password, no 2FA and no policy check. An install
// that already has any account is never touched by the environment again.
//
// The emptiness check and the insert run under the same advisory lock as
// first-run setup (SetupAdmin), so exactly one of the two can create the first
// account.
func (a *Auth) SeedAdmin(ctx context.Context) error {
	if a.cfg.AdminUsername == "" || a.cfg.AdminPassword == "" {
		log.Printf("auth: ADMIN_USERNAME/ADMIN_PASSWORD not set; skipping admin seed")
		return nil
	}
	// Cheap pre-check so a normal start does not pay for a bcrypt hash.
	needs, err := a.NeedsAdmin(ctx)
	if err != nil {
		return err
	}
	if !needs {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(a.cfg.AdminPassword), bcryptCost)
	if err != nil {
		return err
	}
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, PanelUserLockKey); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO admin_user (username, password_hash, role)
		SELECT $1, $2, 'admin'
		WHERE NOT EXISTS (SELECT 1 FROM admin_user)`,
		a.cfg.AdminUsername, string(hash))
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	a.setup.hasAdmin.Store(true)
	if tag.RowsAffected() == 1 {
		log.Printf("auth: seeded initial admin user %q (the table was empty)", a.cfg.AdminUsername)
	}
	return nil
}

// EnsureUsernameIndex makes panel usernames unique regardless of case at the
// schema level (admin_user_username_lower_idx). Migration 0016 creates the
// index unless the table already holds names that differ only by case; in that
// case it must not fail a live database, so it skips. This runs at every start
// and (a) says so loudly in the server log, naming the clashing accounts, and
// (b) creates the index as soon as the duplicates are gone.
func (a *Auth) EnsureUsernameIndex(ctx context.Context) error {
	rows, err := a.pool.Query(ctx, `
		SELECT string_agg(username, ', ' ORDER BY username)
		FROM admin_user GROUP BY lower(username) HAVING count(*) > 1`)
	if err != nil {
		return err
	}
	var clashes []string
	for rows.Next() {
		var names string
		if err := rows.Scan(&names); err != nil {
			rows.Close()
			return err
		}
		clashes = append(clashes, names)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(clashes) > 0 {
		log.Printf("auth: WARNING: panel usernames that differ only by case exist (%s); "+
			"the case-insensitive unique index was NOT created. Rename or delete the duplicates; "+
			"it is created at the next start.", strings.Join(clashes, " | "))
		return nil
	}
	_, err = a.pool.Exec(ctx,
		`CREATE UNIQUE INDEX IF NOT EXISTS admin_user_username_lower_idx ON admin_user (lower(username))`)
	return err
}

// HashPassword hashes a plaintext password with bcrypt (cost 12).
func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	return string(h), err
}

// ValidatePassword enforces the panel password policy and returns a coded API
// error (password_too_weak / password_too_long) or nil.
func ValidatePassword(pw string) error {
	if utf8.RuneCountInString(pw) < MinPasswordLen {
		return apierr.WithParams(http.StatusBadRequest, apierr.PasswordTooWeak,
			"password must be at least "+strconv.Itoa(MinPasswordLen)+" characters",
			map[string]any{"min_length": MinPasswordLen})
	}
	if len(pw) > maxPasswordBytes {
		return apierr.WithParams(http.StatusBadRequest, apierr.PasswordTooLong,
			"password must be at most "+strconv.Itoa(maxPasswordBytes)+" bytes",
			map[string]any{"max_bytes": maxPasswordBytes})
	}
	return nil
}

// account is the admin_user row as the credential paths need it.
type account struct {
	ID          int64
	Username    string
	Hash        string
	Role        string
	Disabled    bool
	Epoch       int
	TOTPEnabled bool
	SecretEnc   []byte
	PendingEnc  []byte
	LastStep    int64
	Recovery    []string
}

// loadAccount reads one account by username, ignoring case (usernames are
// unique regardless of case, migration 0016). pgx.ErrNoRows if absent.
func (a *Auth) loadAccount(ctx context.Context, username string) (*account, error) {
	ac := &account{}
	err := a.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, role, disabled, session_epoch,
		       totp_enabled, totp_secret_enc, totp_pending_enc, totp_last_step, recovery_codes
		FROM admin_user WHERE lower(username)=lower($1)`, username,
	).Scan(&ac.ID, &ac.Username, &ac.Hash, &ac.Role, &ac.Disabled, &ac.Epoch,
		&ac.TOTPEnabled, &ac.SecretEnc, &ac.PendingEnc, &ac.LastStep, &ac.Recovery)
	if err != nil {
		return nil, err
	}
	return ac, nil
}

// burnBcrypt spends one bcrypt comparison against a throwaway hash so a login
// for an unknown username takes as long as one for a real account. The hash is
// generated once, at the real cost — a hand-written placeholder that fails to
// parse would return instantly and defeat the guard.
func (a *Auth) burnBcrypt(password string) {
	a.dummyOnce.Do(func() {
		a.dummyHash, _ = bcrypt.GenerateFromPassword([]byte(randomHex(16)), bcryptCost)
	})
	_ = bcrypt.CompareHashAndPassword(a.dummyHash, []byte(password))
}

// tooMany answers 429 rate_limited with Retry-After.
func tooMany(c echo.Context, wait time.Duration) error {
	secs := int(wait.Seconds())
	if secs < 1 {
		secs = 1
	}
	c.Response().Header().Set("Retry-After", strconv.Itoa(secs))
	return apierr.WithParams(http.StatusTooManyRequests, apierr.RateLimited,
		"too many failed attempts; try again later", map[string]any{"retry_after": secs})
}

// Login handles POST /api/auth/login.
func (a *Auth) Login(c echo.Context) error {
	var req model.LoginRequest
	if err := c.Bind(&req); err != nil {
		return apierr.BadRequest("invalid request body")
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		return apierr.BadRequest("username and password are required")
	}

	key := limitKey("login", req.Username, c.RealIP())
	ipKey := limitKey("login-ip", "", c.RealIP())
	wait := a.limiter.blocked(key)
	if w := a.limiter.blocked(ipKey); w > wait {
		wait = w
	}
	if wait > 0 {
		return tooMany(c, wait)
	}
	invalid := func() error {
		a.limiter.fail(key)
		a.limiter.failN(ipKey, loginIPMaxFailures)
		return apierr.New(http.StatusUnauthorized, apierr.InvalidCredentials, "invalid username or password")
	}

	ac, err := a.loadAccount(c.Request().Context(), req.Username)
	if errors.Is(err, pgx.ErrNoRows) {
		a.burnBcrypt(req.Password)
		return invalid()
	}
	if err != nil {
		return apierr.Internal("auth", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(ac.Hash), []byte(req.Password)) != nil {
		return invalid()
	}
	// Checked only after the password: "this account is disabled" is told to
	// someone who proved they own it, not to anyone probing usernames.
	if ac.Disabled {
		return apierr.New(http.StatusUnauthorized, apierr.AccountDisabled, "account is disabled")
	}
	a.limiter.reset(key)
	// A correct password clears the address budget as well, so a shared office
	// address is not kept locked by colleagues' typos.
	a.limiter.reset(ipKey)

	if ac.TOTPEnabled {
		tok, err := a.issueMFAToken(ac)
		if err != nil {
			return apierr.Internal("auth", err)
		}
		noStore(c)
		return c.JSON(http.StatusOK, model.LoginMFAChallenge{MFARequired: true, MFAToken: tok})
	}
	return a.finishLogin(c, ac)
}

// LoginMFA handles POST /api/auth/login/mfa: the second step for 2FA accounts.
func (a *Auth) LoginMFA(c echo.Context) error {
	var req model.LoginMFARequest
	if err := c.Bind(&req); err != nil {
		return apierr.BadRequest("invalid request body")
	}
	tokenInvalid := apierr.New(http.StatusUnauthorized, apierr.MFATokenInvalid,
		"sign-in challenge expired or invalid; start again")

	mc, err := a.verifyMFAToken(req.MFAToken)
	if err != nil {
		return tokenInvalid
	}
	// A separate budget from the password step: a correct password must not
	// refill the attacker's supply of code guesses.
	key := limitKey("mfa", mc.Subject, c.RealIP())
	if wait := a.mfaBlocked(key, mc.Subject); wait > 0 {
		return tooMany(c, wait)
	}

	ctx := c.Request().Context()
	ac, err := a.loadAccount(ctx, mc.Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return tokenInvalid
	}
	if err != nil {
		return apierr.Internal("auth", err)
	}
	// The challenge is void if anything about the account moved since step one:
	// recreated, disabled, password reset (epoch) or 2FA switched off.
	if ac.ID != mc.UID || ac.Epoch != mc.Epoch || ac.Disabled || !ac.TOTPEnabled {
		return tokenInvalid
	}

	ok, err := a.checkSecondFactor(ctx, ac, req.Code)
	if err != nil {
		return apierr.Internal("auth", err)
	}
	if !ok {
		a.mfaFailed(key, ac.Username)
		return apierr.New(http.StatusUnauthorized, apierr.MFAInvalid, "invalid verification code")
	}
	a.mfaPassed(key, ac.Username)
	return a.finishLogin(c, ac)
}

// finishLogin stamps last_login_at, sets the session + CSRF cookies and writes
// the login success body.
func (a *Auth) finishLogin(c echo.Context, ac *account) error {
	// Forced enrolment, never a refused login: the session is real, but until
	// the user enables 2FA RequireAuth lets them reach only the enrolment calls.
	// Read first: if the policy cannot be read no cookie is issued at all.
	enroll := false
	if !ac.TOTPEnabled {
		required, err := a.Require2FA(c.Request().Context())
		if err != nil {
			return apierr.Internal("auth", err)
		}
		enroll = required
	}
	if _, err := a.pool.Exec(c.Request().Context(),
		`UPDATE admin_user SET last_login_at=now() WHERE id=$1`, ac.ID); err != nil {
		// Cosmetic column; a failed stamp must not block a valid sign-in.
		log.Printf("auth: last_login_at update failed: %v", err)
	}
	csrf := randomHex(16)
	if err := a.setSession(c, ac.Username, ac.Role, ac.ID, ac.Epoch, csrf); err != nil {
		return apierr.Internal("auth", err)
	}
	noStore(c)
	return c.JSON(http.StatusOK, model.LoginResponse{
		Username: ac.Username, Role: ac.Role, CSRF: csrf, MFAEnrollmentRequired: enroll,
	})
}

// setSession issues a session token and writes both cookies.
func (a *Auth) setSession(c echo.Context, username, role string, uid int64, epoch int, csrf string) error {
	token, err := a.issueToken(username, role, uid, epoch, csrf)
	if err != nil {
		return err
	}
	exp := time.Now().Add(sessionTTL)
	c.SetCookie(&http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  exp,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	// Readable double-submit CSRF cookie so the SPA can recover the token after a
	// hard refresh (the value must match the nonce embedded in the httpOnly JWT).
	c.SetCookie(&http.Cookie{
		Name:     csrfCookieName,
		Value:    csrf,
		Path:     "/",
		HttpOnly: false,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  exp,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	return nil
}

// Logout handles POST /api/auth/logout. Besides clearing the cookies it revokes
// the token, so a copy of the cookie stops working too. Other sessions of the
// same user are unaffected.
func (a *Auth) Logout(c echo.Context) error {
	var revokeErr error
	if claims, ok := ClaimsFrom(c); ok && claims.ExpiresAt != nil {
		ctx := c.Request().Context()
		_, revokeErr = a.pool.Exec(ctx, `
			INSERT INTO revoked_session (jti, expires_at) VALUES ($1, $2)
			ON CONFLICT (jti) DO NOTHING`, claims.ID, claims.ExpiresAt.Time)
		if revokeErr == nil {
			if _, err := a.pool.Exec(ctx, `DELETE FROM revoked_session WHERE expires_at < now()`); err != nil {
				log.Printf("auth: revoked_session cleanup failed: %v", err)
			}
		}
	}
	c.SetCookie(&http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
	c.SetCookie(&http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
	// The cookies go either way; a failed revocation is still reported, because
	// saying "done" while the token keeps working would be untrue.
	if revokeErr != nil {
		return apierr.Internal("auth", revokeErr)
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *Auth) issueToken(username, role string, uid int64, epoch int, csrf string) (string, error) {
	now := time.Now()
	claims := Claims{
		Role:  role,
		CSRF:  csrf,
		UID:   uid,
		Epoch: epoch,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   username,
			Audience:  jwt.ClaimStrings{audSession},
			ID:        randomHex(16),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(sessionTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(a.cfg.JWTSecret))
}

func (a *Auth) issueMFAToken(ac *account) (string, error) {
	now := time.Now()
	claims := mfaClaims{
		UID:   ac.ID,
		Epoch: ac.Epoch,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   ac.Username,
			Audience:  jwt.ClaimStrings{audMFA},
			ID:        randomHex(8),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(mfaTokenTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.keys.mfa)
}

func (a *Auth) verifyMFAToken(token string) (*mfaClaims, error) {
	claims := &mfaClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		return a.keys.mfa, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithAudience(audMFA), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	if claims.Subject == "" {
		return nil, errors.New("mfa token without subject")
	}
	return claims, nil
}

// VerifyToken parses and validates a session token's signature, expiry and
// audience, returning its claims. It does NOT consult the database — callers
// that authorise a request must use authenticate (RequireAuth, VerifyRequest).
func (a *Auth) VerifyToken(token string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		return []byte(a.cfg.JWTSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithAudience(audSession), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// authenticate turns a session token into trusted claims: signature + audience
// + expiry, then ONE row lookup that proves the account still exists under the
// same id, is enabled, and has not had its sessions revoked. The role in the
// returned claims is the database's. Errors are coded API errors.
func (a *Auth) authenticate(ctx context.Context, token string) (*Claims, error) {
	invalid := apierr.New(http.StatusUnauthorized, apierr.SessionInvalid, "session is invalid or expired")
	claims, err := a.VerifyToken(token)
	if err != nil || claims.Subject == "" || claims.ID == "" {
		return nil, invalid
	}
	if err := a.checkAccount(ctx, claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// checkAccount is the database half of authentication: one row lookup proving
// the account behind already-verified claims still exists under the same id, is
// enabled, has not had its sessions revoked, and that this token was not signed
// out (revoked_session). It refreshes claims.Role and
// claims.EnrollmentRequired from the database.
func (a *Auth) checkAccount(ctx context.Context, claims *Claims) error {
	invalid := apierr.New(http.StatusUnauthorized, apierr.SessionInvalid, "session is invalid or expired")
	var (
		id       int64
		role     string
		disabled bool
		epoch    int
		totp     bool
		revoked  bool
	)
	err := a.pool.QueryRow(ctx, `
		SELECT id, role, disabled, session_epoch, totp_enabled,
		       EXISTS (SELECT 1 FROM revoked_session WHERE jti=$2)
		FROM admin_user WHERE username=$1`, claims.Subject, claims.ID,
	).Scan(&id, &role, &disabled, &epoch, &totp, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return invalid
	}
	if err != nil {
		return apierr.Internal("auth", err)
	}
	if id != claims.UID || epoch != claims.Epoch || revoked {
		return invalid
	}
	if disabled {
		return apierr.New(http.StatusUnauthorized, apierr.AccountDisabled, "account is disabled")
	}
	claims.Role = role
	// The policy (cached) is consulted only for accounts it could affect. Reading
	// totp_enabled from the row on every request is what lifts the restriction
	// the instant enrolment completes, with no new login. If the policy cannot be
	// read the request fails (500): "unknown" must never be treated as "off".
	claims.EnrollmentRequired = false
	if !totp {
		required, err := a.Require2FA(ctx)
		if err != nil {
			return apierr.Internal("auth", err)
		}
		claims.EnrollmentRequired = required
	}
	return nil
}

// Revalidate re-checks claims that were authenticated earlier, for connections
// that outlive a single request (the WebSocket). It fails when the token has
// expired, when the account was deleted, disabled or had its sessions revoked —
// including by another process, such as the break-glass CLI — or when the
// account has fallen into forced 2FA enrolment. A 5xx-coded error means the
// check itself could not run (database unavailable), not that the session is bad.
func (a *Auth) Revalidate(ctx context.Context, claims *Claims) error {
	if claims == nil || claims.ExpiresAt == nil || !time.Now().Before(claims.ExpiresAt.Time) {
		return apierr.New(http.StatusUnauthorized, apierr.SessionInvalid, "session is invalid or expired")
	}
	if err := a.checkAccount(ctx, claims); err != nil {
		return err
	}
	if claims.EnrollmentRequired {
		return enrollmentRequired()
	}
	return nil
}

// verifyCookie authenticates the session cookie of r (token + database check).
func (a *Auth) verifyCookie(r *http.Request) (*Claims, error) {
	ck, err := r.Cookie(cookieName)
	if err != nil {
		return nil, apierr.New(http.StatusUnauthorized, apierr.SessionRequired, "sign-in required")
	}
	return a.authenticate(r.Context(), ck.Value)
}

func enrollmentRequired() error {
	return apierr.New(http.StatusForbidden, apierr.MFAEnrollmentRequired,
		"two-factor authentication must be set up before using the panel")
}

// VerifyRequest fully validates the session cookie of an HTTP request for
// callers outside the Echo middleware chain — the WebSocket handler. It applies
// the same checks as RequireAuth on a non-exempt route, so an account still in
// forced 2FA enrolment is refused.
func (a *Auth) VerifyRequest(r *http.Request) (*Claims, error) {
	claims, err := a.verifyCookie(r)
	if err != nil {
		return nil, err
	}
	if claims.EnrollmentRequired {
		return nil, enrollmentRequired()
	}
	return claims, nil
}

// enrollmentExempt reports whether a route stays reachable for an account that
// must still enrol in 2FA: the session endpoints, the two enrolment calls, and
// preferences (so the enrolment screen can be shown in the user's language).
// path is the registered route pattern, not the raw URL.
func enrollmentExempt(path string) bool {
	if strings.HasPrefix(path, "/api/auth/") {
		return true
	}
	switch path {
	case "/api/me/2fa/setup", "/api/me/2fa/enable", "/api/me/prefs", "/api/settings/public":
		return true
	}
	return false
}

// RequireAuth is Echo middleware that requires a valid session cookie and, on
// write methods, a matching CSRF header.
func (a *Auth) RequireAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		claims, err := a.verifyCookie(c.Request())
		if err != nil {
			return err
		}
		if isWrite(c.Request().Method) {
			if subtle.ConstantTimeCompare([]byte(c.Request().Header.Get(csrfHeader)), []byte(claims.CSRF)) != 1 {
				return apierr.New(http.StatusForbidden, apierr.CSRFFailed, "CSRF validation failed")
			}
		}
		if claims.EnrollmentRequired && !enrollmentExempt(c.Path()) {
			return enrollmentRequired()
		}
		c.Set(contextClaims, claims)
		return next(c)
	}
}

// RequireAdmin is RequireAuth plus a role==admin check (against the role read
// from the database for this request).
func (a *Auth) RequireAdmin(next echo.HandlerFunc) echo.HandlerFunc {
	return a.RequireAuth(func(c echo.Context) error {
		claims, _ := ClaimsFrom(c)
		if claims == nil || claims.Role != "admin" {
			return apierr.New(http.StatusForbidden, apierr.AdminRequired, "administrator role required")
		}
		return next(c)
	})
}

// ClaimsFrom returns the claims stored on the Echo context by RequireAuth.
func ClaimsFrom(c echo.Context) (*Claims, bool) {
	v, ok := c.Get(contextClaims).(*Claims)
	return v, ok
}

func isWrite(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return true
	}
	return false
}

// noStore keeps credential-bearing responses out of any cache.
func noStore(c echo.Context) {
	c.Response().Header().Set("Cache-Control", "no-store")
}

// randomHex returns a cryptographically random hex string of nBytes bytes.
func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = crand.Read(b) // never fails on supported platforms (Go >= 1.24 aborts instead)
	return hex.EncodeToString(b)
}
