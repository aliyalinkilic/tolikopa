package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/config"
)

// RFC 6238 Appendix B, SHA-1 rows. The reference secret is the ASCII string
// "12345678901234567890"; the published values are 8 digits.
var rfc6238 = []struct {
	unix int64
	code string
}{
	{59, "94287082"},
	{1111111109, "07081804"},
	{1111111111, "14050471"},
	{1234567890, "89005924"},
	{2000000000, "69279037"},
	{20000000000, "65353130"},
}

var rfcSecret = []byte("12345678901234567890")

func TestTOTPRFC6238Vectors(t *testing.T) {
	for _, v := range rfc6238 {
		step := uint64(totpStep(time.Unix(v.unix, 0)))
		if got := hotp(rfcSecret, step, 8); got != v.code {
			t.Errorf("T=%d: 8-digit code = %s, want %s", v.unix, got, v.code)
		}
		// The 6-digit code the panel uses is the same truncation mod 10^6.
		want6 := v.code[2:]
		if got := hotp(rfcSecret, step, totpDigits); got != want6 {
			t.Errorf("T=%d: 6-digit code = %s, want %s", v.unix, got, want6)
		}
		if _, ok := verifyTOTP(rfcSecret, want6, time.Unix(v.unix, 0), 0); !ok {
			t.Errorf("T=%d: verifyTOTP rejected the reference code", v.unix)
		}
	}
}

func TestTOTPWindow(t *testing.T) {
	now := time.Unix(1111111111, 0)
	cur := totpStep(now)
	code := func(step int64) string { return hotp(rfcSecret, uint64(step), totpDigits) }

	for _, d := range []int64{-1, 0, 1} {
		step, ok := verifyTOTP(rfcSecret, code(cur+d), now, 0)
		if !ok || step != cur+d {
			t.Errorf("step %+d: ok=%v step=%d, want accepted at %d", d, ok, step, cur+d)
		}
	}
	for _, d := range []int64{-2, 2, 10} {
		if _, ok := verifyTOTP(rfcSecret, code(cur+d), now, 0); ok {
			t.Errorf("step %+d outside the ±1 window was accepted", d)
		}
	}
	for _, bad := range []string{"", "12345", "1234567", "12345a", " 23456", "００００００"} {
		if _, ok := verifyTOTP(rfcSecret, bad, now, 0); ok {
			t.Errorf("malformed code %q accepted", bad)
		}
	}
}

// A code is good once: after it is accepted its step (and every earlier one)
// is dead, even though the wall clock is still inside the window.
func TestTOTPRejectsReplay(t *testing.T) {
	now := time.Unix(1234567890, 0)
	cur := totpStep(now)
	code := hotp(rfcSecret, uint64(cur), totpDigits)

	step, ok := verifyTOTP(rfcSecret, code, now, 0)
	if !ok {
		t.Fatal("first use rejected")
	}
	if _, ok := verifyTOTP(rfcSecret, code, now, step); ok {
		t.Error("same code accepted a second time")
	}
	if _, ok := verifyTOTP(rfcSecret, code, now.Add(20*time.Second), step); ok {
		t.Error("same code accepted again later in the window")
	}
	// The previous step's code must not work after a newer step was used.
	prev := hotp(rfcSecret, uint64(cur-1), totpDigits)
	if _, ok := verifyTOTP(rfcSecret, prev, now, step); ok {
		t.Error("older step accepted after a newer one was consumed")
	}
	// The next step's code is still fine.
	next := hotp(rfcSecret, uint64(cur+1), totpDigits)
	if s, ok := verifyTOTP(rfcSecret, next, now, step); !ok || s != cur+1 {
		t.Error("next step rejected")
	}
}

func TestOTPAuthURL(t *testing.T) {
	got := otpauthURL("ayse", "JBSWY3DPEHPK3PXP")
	want := "otpauth://totp/Tolikopa:ayse?secret=JBSWY3DPEHPK3PXP&issuer=Tolikopa&algorithm=SHA1&digits=6&period=30"
	if got != want {
		t.Errorf("otpauth url\n got %s\nwant %s", got, want)
	}
	if s := b32.EncodeToString(newTOTPSecret()); len(s) != 32 || strings.Contains(s, "=") {
		t.Errorf("secret %q: want 32 unpadded base32 chars", s)
	}
}

func TestSealOpen(t *testing.T) {
	k := deriveKeys("jwt-secret-one")
	secret := newTOTPSecret()
	blob, err := seal(k.totp, secret, totpAAD(7))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, secret) {
		t.Fatal("ciphertext contains the plaintext secret")
	}
	got, err := open(k.totp, blob, totpAAD(7))
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("round trip failed: %v", err)
	}
	blob2, _ := seal(k.totp, secret, totpAAD(7))
	if bytes.Equal(blob, blob2) {
		t.Error("two seals of the same secret are identical (nonce reuse)")
	}
	if _, err := open(k.totp, blob, totpAAD(8)); err == nil {
		t.Error("blob opened under another account's AAD")
	}
	if _, err := open(deriveKeys("jwt-secret-two").totp, blob, totpAAD(7)); err == nil {
		t.Error("blob opened after JWT_SECRET changed")
	}
	blob[len(blob)-1] ^= 1
	if _, err := open(k.totp, blob, totpAAD(7)); err == nil {
		t.Error("tampered blob opened")
	}
	if _, err := open(k.totp, []byte{1, 2, 3}, nil); err == nil {
		t.Error("truncated blob opened")
	}
}

func TestDerivedKeysAreSeparated(t *testing.T) {
	k := deriveKeys("s3cret")
	all := [][]byte{k.totp, k.recovery, k.mfa, []byte("s3cret")}
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			if bytes.Equal(all[i], all[j]) {
				t.Errorf("keys %d and %d are equal", i, j)
			}
		}
	}
	if len(k.totp) != 32 {
		t.Errorf("totp key is %d bytes, want 32 (AES-256)", len(k.totp))
	}
}

func TestRecoveryCodes(t *testing.T) {
	key := deriveKeys("s3cret").recovery
	plain, hashes := newRecoveryCodes(key)
	if len(plain) != recoveryCount || len(hashes) != recoveryCount {
		t.Fatalf("got %d codes / %d hashes, want %d", len(plain), len(hashes), recoveryCount)
	}
	seen := map[string]bool{}
	for i, c := range plain {
		if len(c) != 11 || c[5] != '-' {
			t.Errorf("code %q is not xxxxx-xxxxx", c)
		}
		if seen[c] {
			t.Errorf("duplicate code %q", c)
		}
		seen[c] = true
		for _, h := range hashes {
			if strings.Contains(h, strings.ReplaceAll(c, "-", "")) || h == c {
				t.Fatal("a stored value contains the plaintext code")
			}
		}
		if len(hashes[i]) != 64 {
			t.Errorf("hash %d is %d chars, want 64 hex", i, len(hashes[i]))
		}
	}

	// Accepted in any presentation; consumed on use; never accepted twice.
	code := plain[3]
	for _, form := range []string{code, strings.ToUpper(code), strings.ReplaceAll(code, "-", ""), " " + code[:5] + " " + code[6:] + " "} {
		if _, ok := matchRecovery(hashes, key, form); !ok {
			t.Errorf("form %q not accepted", form)
		}
	}
	used, ok := matchRecovery(hashes, key, code)
	if !ok {
		t.Fatal("valid code rejected")
	}
	remaining := removeRecovery(hashes, used)
	if len(remaining) != recoveryCount-1 {
		t.Fatalf("%d codes remain, want %d", len(remaining), recoveryCount-1)
	}
	if _, ok := matchRecovery(remaining, key, code); ok {
		t.Error("recovery code accepted a second time")
	}
	if _, ok := matchRecovery(remaining, key, plain[4]); !ok {
		t.Error("using one code invalidated another")
	}

	for _, bad := range []string{"", "aaaaa-aaaaa", code[:10], code + "x", "123456"} {
		if _, ok := matchRecovery(hashes, key, bad); ok {
			t.Errorf("bogus code %q accepted", bad)
		}
	}
	// A different server key (rotated JWT_SECRET) voids the codes.
	if _, ok := matchRecovery(hashes, deriveKeys("other").recovery, code); ok {
		t.Error("code accepted under a different key")
	}
}

func TestLimiterEscalates(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newLimiter()
	l.now = func() time.Time { return now }
	key := limitKey("login", "Admin ", "10.0.0.5")
	if key != limitKey("login", "admin", "10.0.0.5") {
		t.Fatal("username is not case/space folded in the key")
	}

	for i, want := range limitWindows {
		for n := 1; n < limitMaxFailures; n++ {
			if d := l.fail(key); d != 0 {
				t.Fatalf("round %d: locked after %d failures", i, n)
			}
			if l.blocked(key) != 0 {
				t.Fatalf("round %d: blocked before the limit", i)
			}
		}
		if d := l.fail(key); d != want {
			t.Fatalf("round %d: lock = %v, want %v", i, d, want)
		}
		if got := l.blocked(key); got != want {
			t.Fatalf("round %d: blocked = %v, want %v", i, got, want)
		}
		now = now.Add(want + time.Second)
		if l.blocked(key) != 0 {
			t.Fatalf("round %d: still blocked after the window", i)
		}
	}
	// The last window repeats.
	for n := 0; n < limitMaxFailures; n++ {
		l.fail(key)
	}
	if got := l.blocked(key); got != limitWindows[len(limitWindows)-1] {
		t.Errorf("cap: blocked = %v", got)
	}
}

func TestLimiterIsolationAndReset(t *testing.T) {
	l := newLimiter()
	a := limitKey("login", "admin", "10.0.0.5")
	for n := 0; n < limitMaxFailures; n++ {
		l.fail(a)
	}
	if l.blocked(a) == 0 {
		t.Fatal("not locked")
	}
	for _, other := range []string{
		limitKey("login", "admin", "10.0.0.6"), // same user, other address
		limitKey("login", "ayse", "10.0.0.5"),  // other user, same address
		limitKey("mfa", "admin", "10.0.0.5"),   // other step
	} {
		if l.blocked(other) != 0 {
			t.Errorf("lock leaked to %q", other)
		}
	}
	b := limitKey("login", "ayse", "10.0.0.5")
	for n := 0; n < limitMaxFailures-1; n++ {
		l.fail(b)
	}
	l.reset(b)
	for n := 0; n < limitMaxFailures-1; n++ {
		if l.fail(b) != 0 {
			t.Fatal("success did not reset the failure count")
		}
	}
}

func testAuth() *Auth {
	return New(nil, &config.Config{JWTSecret: "unit-test-secret"})
}

// The MFA challenge must be worthless as a session, and a session cookie
// worthless as a challenge.
func TestMFATokenIsNotASession(t *testing.T) {
	a := testAuth()
	ac := &account{ID: 3, Username: "ayse", Role: "admin", Epoch: 2}

	mfa, err := a.issueMFAToken(ac)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.VerifyToken(mfa); err == nil {
		t.Error("MFA challenge token was accepted as a session token")
	}
	mc, err := a.verifyMFAToken(mfa)
	if err != nil || mc.Subject != "ayse" || mc.UID != 3 || mc.Epoch != 2 {
		t.Fatalf("challenge round trip: %+v %v", mc, err)
	}
	if ttl := time.Until(mc.ExpiresAt.Time); ttl > mfaTokenTTL || ttl < mfaTokenTTL-time.Minute {
		t.Errorf("challenge ttl = %v, want ~%v", ttl, mfaTokenTTL)
	}

	sess, err := a.issueToken("ayse", "admin", 3, 2, "csrf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.verifyMFAToken(sess); err == nil {
		t.Error("session token was accepted as an MFA challenge")
	}
	c, err := a.VerifyToken(sess)
	if err != nil || c.Subject != "ayse" || c.UID != 3 || c.Epoch != 2 || c.CSRF != "csrf" {
		t.Fatalf("session round trip: %+v %v", c, err)
	}

	// Same claims signed by another instance's secret are rejected.
	other := New(nil, &config.Config{JWTSecret: "another-secret"})
	if _, err := other.VerifyToken(sess); err == nil {
		t.Error("session accepted under a different JWT_SECRET")
	}
	if _, err := other.verifyMFAToken(mfa); err == nil {
		t.Error("challenge accepted under a different JWT_SECRET")
	}
	for _, junk := range []string{"", "a.b.c", sess + "x"} {
		if _, err := a.VerifyToken(junk); err == nil {
			t.Errorf("junk token %q accepted", junk)
		}
	}
}

func TestEnrollmentExempt(t *testing.T) {
	exempt := []string{
		"/api/auth/me", "/api/auth/logout", "/api/auth/login", "/api/auth/login/mfa",
		"/api/me/2fa/setup", "/api/me/2fa/enable", "/api/me/prefs", "/api/settings/public",
	}
	for _, p := range exempt {
		if !enrollmentExempt(p) {
			t.Errorf("%s must stay reachable during forced enrolment", p)
		}
	}
	blocked := []string{
		"/api/fleet", "/api/me/password", "/api/me/profile", "/api/me/2fa/disable",
		"/api/me/2fa/recovery-codes", "/api/admin/panel-users", "/api/admin/settings/general",
		"/api/machines/:id", "/api/authx", "/api/v1/fleet/overview", "",
	}
	for _, p := range blocked {
		if enrollmentExempt(p) {
			t.Errorf("%s must be blocked during forced enrolment", p)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	code := func(err error) string {
		if err == nil {
			return ""
		}
		status, body := apierr.Resolve(err)
		if status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", status)
		}
		return body.Code
	}
	cases := map[string]string{
		"":                       apierr.PasswordTooWeak,
		"123456789":              apierr.PasswordTooWeak,
		"1234567890":             "",
		"şifreşifre":             "", // 10 characters, 12 bytes
		strings.Repeat("a", 72):  "",
		strings.Repeat("a", 73):  apierr.PasswordTooLong,
		strings.Repeat("ş", 37):  apierr.PasswordTooLong, // 74 bytes
		"correct horse battery!": "",
	}
	for pw, want := range cases {
		if got := code(ValidatePassword(pw)); got != want {
			t.Errorf("ValidatePassword(%d chars) = %q, want %q", len([]rune(pw)), got, want)
		}
	}
}

func TestDecodePrefs(t *testing.T) {
	p := decodePrefs(nil)
	if p.Theme != nil || p.Lang != nil {
		t.Errorf("defaults = %+v", p)
	}
	p = decodePrefs([]byte(`{"theme":"dark","lang":"en"}`))
	if p.Theme == nil || *p.Theme != "dark" || p.Lang == nil || *p.Lang != "en" {
		t.Errorf("stored = %+v", p)
	}
	p = decodePrefs([]byte(`{"theme":"neon","lang":"de"}`))
	if p.Theme != nil || p.Lang != nil {
		t.Errorf("invalid stored values not cleared: %+v", p)
	}
	// An explicit "system" is a choice and must survive; unset must be JSON null.
	p = decodePrefs([]byte(`{"theme":"system"}`))
	if p.Theme == nil || *p.Theme != "system" {
		t.Errorf("explicit system lost: %+v", p)
	}
	raw, _ := json.Marshal(decodePrefs([]byte(`{}`)))
	if string(raw) != `{"theme":null,"lang":null}` {
		t.Errorf("unset prefs marshal as %s", raw)
	}
}

// The middleware's refusals that need no database: they must come out of the
// central handler as coded JSON, with the codes the panel switches on.
func TestRequireAuthRefusalsAreCoded(t *testing.T) {
	a := testAuth()
	e := echo.New()
	e.HTTPErrorHandler = apierr.Handler
	e.GET("/api/fleet", func(c echo.Context) error { return c.NoContent(http.StatusOK) }, a.RequireAuth)

	mfa, _ := a.issueMFAToken(&account{ID: 1, Username: "ayse"})
	cases := []struct {
		name, cookie, wantCode string
	}{
		{"no cookie", "", apierr.SessionRequired},
		{"garbage cookie", "not-a-jwt", apierr.SessionInvalid},
		{"mfa challenge used as a session cookie", mfa, apierr.SessionInvalid},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/fleet", nil)
		if tc.cookie != "" {
			req.AddCookie(&http.Cookie{Name: cookieName, Value: tc.cookie})
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		var body apierr.Body
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: body is not JSON: %q", tc.name, rec.Body.String())
		}
		if rec.Code != http.StatusUnauthorized || body.Code != tc.wantCode || body.Message == "" {
			t.Errorf("%s: got %d %+v, want 401 %s", tc.name, rec.Code, body, tc.wantCode)
		}
	}
}

// Changing source address must not buy more second-factor guesses: the account
// budget is shared by every address.
func TestMFAAccountBudgetIsIPIndependent(t *testing.T) {
	a := testAuth()
	ipKey := func(ip string) string { return limitKey("mfa", "ayse", ip) }

	for i := 0; i < limitMaxFailures; i++ {
		ip := "10.0.0." + string(rune('1'+i)) // a fresh address for every guess
		if a.mfaBlocked(ipKey(ip), "ayse") != 0 {
			t.Fatalf("blocked after only %d failures", i)
		}
		a.mfaFailed(ipKey(ip), "ayse")
	}
	// No single address reached its own limit, yet a brand-new one is locked out.
	if a.limiter.blocked(ipKey("10.0.0.1")) != 0 {
		t.Fatal("per-address budget tripped; the test is not exercising the account budget")
	}
	if wait := a.mfaBlocked(ipKey("10.0.0.200"), "Ayse "); wait != limitWindows[0] {
		t.Errorf("fresh address: wait = %v, want the account lock %v", wait, limitWindows[0])
	}
	// Another account is unaffected.
	if a.mfaBlocked(limitKey("mfa", "mehmet", "10.0.0.200"), "mehmet") != 0 {
		t.Error("lock leaked to another account")
	}
	// A password change / reset / disable clears it (SessionsRevoked).
	a.SessionsRevoked("ayse")
	if a.mfaBlocked(ipKey("10.0.0.200"), "ayse") != 0 {
		t.Error("account lock survived a credential change")
	}
	// A correct code clears both budgets.
	for i := 0; i < limitMaxFailures-1; i++ {
		a.mfaFailed(ipKey("10.0.0.9"), "ayse")
	}
	a.mfaPassed(ipKey("10.0.0.9"), "ayse")
	a.mfaFailed(ipKey("10.0.0.9"), "ayse")
	if a.mfaBlocked(ipKey("10.0.0.9"), "ayse") != 0 {
		t.Error("success did not reset the budgets")
	}
}

// A long-lived connection must not outlive its token; this part of Revalidate
// needs no database.
func TestRevalidateRejectsExpiredOrMissingClaims(t *testing.T) {
	a := testAuth()
	expired := &Claims{RegisteredClaims: jwt.RegisteredClaims{
		Subject: "ayse", ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Second)),
	}}
	for name, c := range map[string]*Claims{"nil": nil, "no expiry": {}, "expired": expired} {
		status, body := apierr.Resolve(a.Revalidate(context.Background(), c))
		if status != http.StatusUnauthorized || body.Code != apierr.SessionInvalid {
			t.Errorf("%s: got %d %s, want 401 session_invalid", name, status, body.Code)
		}
	}
}

// The address budget counts failures across usernames with its own, higher
// threshold, so spraying one password over many accounts is capped.
func TestLimiterFailNThreshold(t *testing.T) {
	l := newLimiter()
	ip := limitKey("login-ip", "", "10.0.0.5")
	for n := 1; n < loginIPMaxFailures; n++ {
		if d := l.failN(ip, loginIPMaxFailures); d != 0 {
			t.Fatalf("locked after %d failures, want %d", n, loginIPMaxFailures)
		}
	}
	if d := l.failN(ip, loginIPMaxFailures); d == 0 || l.blocked(ip) == 0 {
		t.Fatal("not locked at the threshold")
	}
}

// Sessions carry a jti so that logout can revoke exactly one token.
func TestSessionTokenHasID(t *testing.T) {
	a := &Auth{cfg: &config.Config{JWTSecret: strings.Repeat("k", 48)}}
	tok, err := a.issueToken("admin", "admin", 1, 0, "c")
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.VerifyToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	tok2, _ := a.issueToken("admin", "admin", 1, 0, "c")
	c2, _ := a.VerifyToken(tok2)
	if c.ID == "" || c.ID == c2.ID {
		t.Fatalf("token ids must be present and unique: %q %q", c.ID, c2.ID)
	}
}
