//go:build linux

package collector

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const systemdSessionsDir = "/run/systemd/sessions"

// SessionTracker resolves the logged-on user on Linux (best-effort, via
// systemd-logind session files). It carries no lock state — Linux exposes only
// session present/absent (see docs/AGENT.md).
type SessionTracker struct{}

// NewSessionTracker returns a tracker.
func NewSessionTracker() *SessionTracker { return &SessionTracker{} }

// HandleSessionChange is a no-op on Linux (no SESSIONCHANGE notifications).
func (t *SessionTracker) HandleSessionChange(_, _ uint32) {}

// SetIdleThreshold is a no-op on Linux: there is no idle detection (headless
// server), so a present session is always reported "active".
func (t *SessionTracker) SetIdleThreshold(_ int) {}

// ReportIdle is a no-op on Linux (no in-session helper feeds idle data).
func (t *SessionTracker) ReportIdle(_ int) {}

// Current returns the active logged-on user, or none for a headless host.
func (t *SessionTracker) Current() Session {
	entries, err := os.ReadDir(systemdSessionsDir)
	if err != nil {
		return Session{State: "none"}
	}
	var fallbackUser string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		user, uid, active := parseSessionFile(filepath.Join(systemdSessionsDir, e.Name()))
		if user == "" || uid < 1000 { // skip system/service sessions
			continue
		}
		if active {
			return Session{User: user, State: "active"}
		}
		if fallbackUser == "" {
			fallbackUser = user
		}
	}
	if fallbackUser != "" {
		return Session{User: fallbackUser, State: "active"}
	}
	return Session{State: "none"}
}

// parseSessionFile extracts USER, UID and whether STATE is active/online.
func parseSessionFile(path string) (user string, uid int, active bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, false
	}
	uid = -1
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "USER":
			user = strings.TrimSpace(v)
		case "UID":
			uid, _ = strconv.Atoi(strings.TrimSpace(v))
		case "STATE":
			s := strings.TrimSpace(v)
			active = s == "active" || s == "online"
		}
	}
	return user, uid, active
}
