package auth

import (
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 default; what every authenticator app implements
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"net/url"
	"time"
)

// TOTP parameters (RFC 6238). These are the values every authenticator app
// assumes when the otpauth URL omits them, and the URL states them explicitly.
const (
	totpDigits    = 6
	totpPeriod    = 30 // seconds
	totpSkew      = 1  // accepted steps either side of "now"
	totpSecretLen = 20 // bytes; the SHA-1 block-friendly size RFC 4226 recommends
	totpIssuer    = "Tolikopa"
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

var pow10 = [...]uint32{1, 10, 100, 1000, 10000, 100000, 1000000, 10000000, 100000000}

// hotp is RFC 4226 HOTP with HMAC-SHA1, returning a zero-padded decimal string.
// digits must be 1..8.
func hotp(secret []byte, counter uint64, digits int) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)

	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	v := bin % pow10[digits]

	out := make([]byte, digits)
	for i := digits - 1; i >= 0; i-- {
		out[i] = byte('0' + v%10)
		v /= 10
	}
	return string(out)
}

// totpStep is the RFC 6238 time-step counter for t (T0 = Unix epoch).
func totpStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// verifyTOTP checks code against the steps now-1, now, now+1. It returns the
// step that matched. A step at or before lastStep is never accepted — that is
// the replay guard: a code shoulder-surfed or phished cannot be used a second
// time inside its own validity window. The caller must persist the returned
// step atomically (UPDATE … WHERE totp_last_step < $step).
//
// Every candidate step is evaluated and compared in constant time, so the
// response time does not reveal which step (if any) matched.
func verifyTOTP(secret []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	if !isDigits(code, totpDigits) {
		return 0, false
	}
	cur := totpStep(now)
	var matched int64
	found := 0
	for d := int64(-totpSkew); d <= totpSkew; d++ {
		step := cur + d
		if step < 0 {
			continue
		}
		want := hotp(secret, uint64(step), totpDigits)
		eq := subtle.ConstantTimeCompare([]byte(want), []byte(code))
		if eq == 1 && step > lastStep {
			matched = step
			found = 1
		}
	}
	return matched, found == 1
}

// isDigits reports whether s is exactly n ASCII digits.
func isDigits(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// newTOTPSecret returns 160 bits from the OS CSPRNG.
func newTOTPSecret() []byte {
	b := make([]byte, totpSecretLen)
	_, _ = crand.Read(b) // never fails on supported platforms (Go >= 1.24 aborts instead)
	return b
}

// otpauthURL builds the provisioning URI authenticator apps scan as a QR code
// (Google Authenticator "Key URI Format").
func otpauthURL(username, secretB32 string) string {
	return "otpauth://totp/" + url.PathEscape(totpIssuer+":"+username) +
		"?secret=" + secretB32 +
		"&issuer=" + url.QueryEscape(totpIssuer) +
		"&algorithm=SHA1&digits=6&period=30"
}
