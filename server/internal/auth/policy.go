package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// SettingRequire2FA is the app_setting key of the "require 2FA" policy (jsonb
// boolean). When true, an account without two-factor can sign in but reaches
// only the enrolment endpoints until it enrols (see enrollmentExempt).
const SettingRequire2FA = "require_2fa"

// policyTTL bounds how stale the cached policy can be. InvalidatePolicy makes a
// change through the API immediate; the TTL exists for changes made from
// another process — the break-glass CLI (`server admin require-2fa off`).
const policyTTL = 15 * time.Second

type policyCache struct {
	mu      sync.Mutex
	loaded  bool
	at      time.Time
	require bool
}

// Require2FA reports whether the instance currently requires two-factor. The
// value is cached for policyTTL.
//
// A failed read is returned as an error and is never papered over with a
// default or a stale value: guessing "off" would silently lift the restriction
// from every unenrolled account, guessing "on" would lock a fresh install into
// enrolment. Callers turn the error into a 500.
func (a *Auth) Require2FA(ctx context.Context) (bool, error) {
	p := &a.policy
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loaded && time.Since(p.at) < policyTTL {
		return p.require, nil
	}
	var v bool
	err := a.pool.QueryRow(ctx,
		`SELECT value = 'true'::jsonb FROM app_setting WHERE key=$1`, SettingRequire2FA).Scan(&v)
	switch {
	case err == nil:
		p.require = v
	case errors.Is(err, pgx.ErrNoRows):
		p.require = false
	default:
		p.loaded = false
		return false, fmt.Errorf("reading %s: %w", SettingRequire2FA, err)
	}
	p.loaded = true
	p.at = time.Now()
	return p.require, nil
}

// InvalidatePolicy drops the cached policy so the next request re-reads it.
// Called by the settings handler after a write.
func (a *Auth) InvalidatePolicy() {
	a.policy.mu.Lock()
	a.policy.loaded = false
	a.policy.mu.Unlock()
}
