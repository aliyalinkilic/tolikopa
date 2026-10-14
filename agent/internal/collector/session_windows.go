//go:build windows

package collector

import (
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WTS session-change event types (WM_WTSSESSION_CHANGE wParam values).
const (
	WTSConsoleConnect    = 0x1
	WTSConsoleDisconnect = 0x2
	WTSRemoteConnect     = 0x3
	WTSRemoteDisconnect  = 0x4
	WTSSessionLogon      = 0x5
	WTSSessionLogoff     = 0x6
	WTSSessionLock       = 0x7
	WTSSessionUnlock     = 0x8
)

// WTS_INFO_CLASS values used here.
const (
	wtsUserName     = 5
	wtsDomainName   = 7
	wtsConnectState = 8
)

// WTS_CONNECTSTATE_CLASS values used here.
const (
	wtsActive       = 0
	wtsDisconnected = 4
)

const noSession = 0xFFFFFFFF // WTSGetActiveConsoleSessionId "no session" sentinel

// idleReportMaxAge is how long a helper idle report stays usable. The in-session
// helper emits every ~30s; beyond this age the report is stale (helper dead,
// session gone) and we fall back to "active" rather than guess.
const idleReportMaxAge = 90 * time.Second

// defaultIdleThresholdSec matches config.Default().IdleThresholdSec; used until
// the runner applies the configured value via SetIdleThreshold.
const defaultIdleThresholdSec = 300

var (
	modWtsapi32          = windows.NewLazySystemDLL("wtsapi32.dll")
	modKernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procWTSQuery         = modWtsapi32.NewProc("WTSQuerySessionInformationW")
	procWTSFree          = modWtsapi32.NewProc("WTSFreeMemory")
	procActiveConsoleSID = modKernel32.NewProc("WTSGetActiveConsoleSessionId")
)

// SessionTracker maintains per-session lock state (fed by the service's
// SESSIONCHANGE notifications), the latest idle report from the in-session
// helper, and resolves the current console session/user.
type SessionTracker struct {
	mu            sync.Mutex
	locked        map[uint32]bool
	idleThreshold int       // seconds of no input that turn "active" into "idle"
	idleSeconds   int       // last reported seconds-since-last-input
	idleAt        time.Time // when idleSeconds was received; zero = never
}

// NewSessionTracker returns an empty tracker.
func NewSessionTracker() *SessionTracker {
	return &SessionTracker{
		locked:        make(map[uint32]bool),
		idleThreshold: defaultIdleThresholdSec,
	}
}

// SetIdleThreshold applies the configured idle threshold (seconds). Values below
// 60 are clamped to 60 (config already clamps; this is defense in depth).
func (t *SessionTracker) SetIdleThreshold(sec int) {
	if sec < 60 {
		sec = 60
	}
	t.mu.Lock()
	t.idleThreshold = sec
	t.mu.Unlock()
}

// ReportIdle records the seconds-since-last-input value relayed from the
// in-session helper (GetLastInputInfo recency — an input-presence timing only,
// never what was typed). The receive time stamps freshness: reports older than
// idleReportMaxAge are ignored by Current().
func (t *SessionTracker) ReportIdle(seconds int) {
	if seconds < 0 {
		return
	}
	t.mu.Lock()
	t.idleSeconds = seconds
	t.idleAt = time.Now()
	t.mu.Unlock()
}

// HandleSessionChange updates lock state from a service SESSIONCHANGE event.
// A sessionID of 0 means "resolve the current console session" — the service
// passes 0 rather than dereferencing the (possibly freed) WTSSESSION_NOTIFICATION.
func (t *SessionTracker) HandleSessionChange(eventType, sessionID uint32) {
	if sessionID == 0 {
		sessionID = activeConsoleSessionID()
	}
	if sessionID == noSession {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	switch eventType {
	case WTSSessionLock:
		t.locked[sessionID] = true
	case WTSSessionUnlock, WTSSessionLogon, WTSConsoleConnect:
		t.locked[sessionID] = false
	case WTSSessionLogoff:
		delete(t.locked, sessionID)
	}
}

// Current resolves the active console session into a Session value.
func (t *SessionTracker) Current() Session {
	sid := activeConsoleSessionID()
	if sid == noSession {
		return Session{State: "none"}
	}
	user := wtsQueryString(sid, wtsUserName)
	if user == "" {
		return Session{State: "none"}
	}
	domain := wtsQueryString(sid, wtsDomainName)
	full := user
	if domain != "" {
		full = domain + `\` + user
	}

	state := "active"
	if cs, ok := wtsQueryUint32(sid, wtsConnectState); ok && cs == wtsDisconnected {
		state = "disconnected"
	} else {
		t.mu.Lock()
		switch {
		case t.locked[sid]:
			state = "locked"
		case !t.idleAt.IsZero() &&
			time.Since(t.idleAt) <= idleReportMaxAge &&
			t.idleSeconds >= t.idleThreshold:
			// Logged on, unlocked, but no keyboard/mouse input for >= threshold.
			// Stale or absent idle data falls through to "active" (no guessing).
			state = "idle"
		}
		t.mu.Unlock()
	}
	return Session{User: full, State: state}
}

func activeConsoleSessionID() uint32 {
	r, _, _ := procActiveConsoleSID.Call()
	return uint32(r)
}

// wtsQueryString queries a wide-string WTS_INFO_CLASS for a session.
func wtsQueryString(sessionID uint32, infoClass uint32) string {
	var buf *uint16
	var n uint32
	r, _, _ := procWTSQuery.Call(
		0, // WTS_CURRENT_SERVER_HANDLE
		uintptr(sessionID),
		uintptr(infoClass),
		uintptr(unsafe.Pointer(&buf)),
		uintptr(unsafe.Pointer(&n)),
	)
	if r == 0 || buf == nil {
		return ""
	}
	defer procWTSFree.Call(uintptr(unsafe.Pointer(buf)))
	return strings.TrimSpace(windows.UTF16PtrToString(buf))
}

// wtsQueryUint32 queries a DWORD-valued WTS_INFO_CLASS (e.g. WTSConnectState).
func wtsQueryUint32(sessionID uint32, infoClass uint32) (uint32, bool) {
	var buf *uint16
	var n uint32
	r, _, _ := procWTSQuery.Call(
		0,
		uintptr(sessionID),
		uintptr(infoClass),
		uintptr(unsafe.Pointer(&buf)),
		uintptr(unsafe.Pointer(&n)),
	)
	if r == 0 || buf == nil {
		return 0, false
	}
	defer procWTSFree.Call(uintptr(unsafe.Pointer(buf)))
	return *(*uint32)(unsafe.Pointer(buf)), true
}
