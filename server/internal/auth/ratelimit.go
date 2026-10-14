package auth

import (
	"strings"
	"sync"
	"time"
)

// Attempt limiting for the credential-checking endpoints. In memory on purpose:
// the panel is a single process, and a restart forgiving the counters is an
// acceptable trade for not needing a table that an attacker can grow.
const (
	limitMaxFailures = 5
	limitForgetAfter = 24 * time.Hour // idle time after which strikes are forgiven
	limitSweepAt     = 5000           // entries; opportunistic cleanup threshold
	limitHardCap     = 20000          // entries; beyond this unlocked entries are dropped
)

// limitWindows is the escalating lock: the n-th lockout of a key lasts
// limitWindows[n-1], the last value repeating.
var limitWindows = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

type limitEntry struct {
	fails   int       // consecutive failures since the last lock/reset
	strikes int       // lockouts so far (selects the window)
	until   time.Time // locked until
	last    time.Time // last failure
}

type limiter struct {
	mu  sync.Mutex
	m   map[string]*limitEntry
	now func() time.Time
}

func newLimiter() *limiter {
	return &limiter{m: make(map[string]*limitEntry), now: time.Now}
}

// limitKey scopes a counter to (purpose, username, client IP). Usernames are
// folded so "Admin" and "admin" share a budget; the length is capped so a
// client cannot make the key arbitrarily large.
func limitKey(scope, username, ip string) string {
	u := strings.ToLower(strings.TrimSpace(username))
	if len(u) > 128 {
		u = u[:128]
	}
	return scope + "\x00" + u + "\x00" + ip
}

// blocked returns how long the key remains locked (0 = not locked).
func (l *limiter) blocked(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.m[key]
	if e == nil {
		return 0
	}
	if d := e.until.Sub(l.now()); d > 0 {
		return d
	}
	return 0
}

// fail records one failed attempt. It returns the lock duration when this
// failure tripped a lockout, otherwise 0.
func (l *limiter) fail(key string) time.Duration { return l.failN(key, limitMaxFailures) }

// failN is fail with its own threshold: max consecutive failures per lockout.
func (l *limiter) failN(key string, max int) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	e := l.m[key]
	if e == nil {
		if len(l.m) >= limitSweepAt {
			l.sweep(now)
		}
		e = &limitEntry{}
		l.m[key] = e
	} else if now.Sub(e.last) > limitForgetAfter && !now.Before(e.until) {
		*e = limitEntry{}
	}
	e.last = now
	e.fails++
	if e.fails < max {
		return 0
	}
	e.fails = 0
	idx := e.strikes
	if idx >= len(limitWindows) {
		idx = len(limitWindows) - 1
	}
	e.strikes++
	e.until = now.Add(limitWindows[idx])
	return limitWindows[idx]
}

// reset forgets a key after a successful authentication.
func (l *limiter) reset(key string) {
	l.mu.Lock()
	delete(l.m, key)
	l.mu.Unlock()
}

// sweep drops stale entries; the caller holds l.mu. Active locks always survive,
// so flooding the table with throwaway usernames cannot unlock a real target.
func (l *limiter) sweep(now time.Time) {
	for k, e := range l.m {
		if now.After(e.until) && now.Sub(e.last) > limitForgetAfter {
			delete(l.m, k)
		}
	}
	if len(l.m) < limitHardCap {
		return
	}
	for k, e := range l.m {
		if now.After(e.until) {
			delete(l.m, k)
		}
	}
}
