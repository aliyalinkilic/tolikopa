package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
)

// keyring holds the purpose-separated keys derived from JWT_SECRET. Each use
// gets its own key so a weakness in one (say, a leaked MFA challenge token)
// says nothing about another (the TOTP secrets at rest).
//
// Session JWTs keep signing with the raw JWT_SECRET so deployed sessions and
// any external tooling that verifies them are unaffected.
type keyring struct {
	totp     []byte // AES-256-GCM key for admin_user.totp_*_enc
	recovery []byte // HMAC-SHA256 key for admin_user.recovery_codes
	mfa      []byte // HS256 key for the short-lived MFA challenge token
}

func deriveKeys(jwtSecret string) keyring {
	derive := func(label string) []byte {
		// HKDF-SHA256 (RFC 5869). The info label is the domain separator; changing
		// a label is a breaking change for whatever that key protects.
		k, err := hkdf.Key(sha256.New, []byte(jwtSecret), nil, label, 32)
		if err != nil {
			// Only reachable for an absurd output length; 32 bytes cannot fail.
			panic("auth: hkdf: " + err.Error())
		}
		return k
	}
	return keyring{
		totp:     derive("tolikopa/totp-secret/v1"),
		recovery: derive("tolikopa/recovery-code/v1"),
		mfa:      derive("tolikopa/mfa-token/v1"),
	}
}

// seal encrypts plaintext with AES-256-GCM under a fresh random nonce and
// returns nonce||ciphertext. aad binds the blob to its row, so a ciphertext
// copied onto another account fails to open.
func seal(key, plaintext, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := crand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, aad), nil
}

// open reverses seal. It fails if the key, the aad or any byte of the blob
// differs — including after a JWT_SECRET rotation.
func open(key, blob, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize()+gcm.Overhead() {
		return nil, errors.New("auth: sealed blob too short")
	}
	nonce, ct := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, aad)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// ── Recovery codes ───────────────────────────────────────────────────────────

const (
	recoveryCount = 10
	recoveryLen   = 10 // characters, shown as xxxxx-xxxxx
	// 32 symbols => exactly 5 bits per character with no modulo bias; 50 bits per
	// code. 'l' and 'o' are left out so a code read off paper is unambiguous.
	recoveryAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"
)

// newRecoveryCodes returns recoveryCount fresh codes in display form plus the
// hashes to store. The plaintext exists only in the response that shows it.
func newRecoveryCodes(key []byte) (plain, hashes []string) {
	plain = make([]string, 0, recoveryCount)
	hashes = make([]string, 0, recoveryCount)
	buf := make([]byte, recoveryLen)
	for len(plain) < recoveryCount {
		_, _ = crand.Read(buf)
		raw := make([]byte, recoveryLen)
		for i, b := range buf {
			raw[i] = recoveryAlphabet[b&31]
		}
		code := string(raw)
		plain = append(plain, code[:5]+"-"+code[5:])
		hashes = append(hashes, hashRecovery(key, code))
	}
	return plain, hashes
}

// normalizeRecovery strips the presentation (dash, spaces, case) from user input.
func normalizeRecovery(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if r == '-' || r == ' ' || r == '\t' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// hashRecovery is HMAC-SHA256 under a server-side key, hex encoded. Keyed
// because 50 bits is strong against online guessing (rate limited) but not
// against an offline search of a leaked table with a bare hash.
func hashRecovery(key []byte, normalized string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(normalized))
	return hex.EncodeToString(mac.Sum(nil))
}

// matchRecovery returns the stored hash that input corresponds to. Every stored
// hash is compared in constant time with no early exit. The caller consumes the
// code by removing the returned hash atomically.
func matchRecovery(hashes []string, key []byte, input string) (string, bool) {
	norm := normalizeRecovery(input)
	if len(norm) != recoveryLen {
		return "", false
	}
	got := []byte(hashRecovery(key, norm))
	matched := ""
	for _, h := range hashes {
		if subtle.ConstantTimeCompare(got, []byte(h)) == 1 {
			matched = h
		}
	}
	return matched, matched != ""
}

// removeRecovery returns hashes without the given entry. It mirrors, for tests
// and documentation, what the SQL array_remove in consumeRecovery does.
func removeRecovery(hashes []string, used string) []string {
	out := make([]string, 0, len(hashes))
	for _, h := range hashes {
		if h != used {
			out = append(out, h)
		}
	}
	return out
}
