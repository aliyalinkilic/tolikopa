//go:build windows

package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/aliyalinkilic/tolikopa/agent/internal/collector"
	"github.com/aliyalinkilic/tolikopa/agent/internal/sender"
)

// noSession marks "no interactive console session".
const noSession = 0xFFFFFFFF

var procWTSGetActiveConsoleSessionId = windows.NewLazySystemDLL("kernel32.dll").
	NewProc("WTSGetActiveConsoleSessionId")

// helperProc tracks one launched in-session helper.
type helperProc struct {
	session uint32
	cancel  context.CancelFunc
	done    chan struct{} // closed when the reader goroutine sees the child exit
}

// runForegroundManager supervises the in-session foreground helper: it launches
// the helper into the active console session, relaunches it when the session
// changes or the helper dies, feeds the helper's per-app second tallies into the
// sender, and routes its "__idle" control field into the shared SessionTracker.
// Every failure is best-effort and logged at debug — it can never disturb the
// main agent loop.
func runForegroundManager(ctx context.Context, exePath string, snd *sender.Sender, tracker *collector.SessionTracker, log *slog.Logger) {
	// This goroutine touches delicate Win32 session APIs; a panic here must never
	// take down the agent (which runs as SYSTEM fleet-wide). Degrade to "no app
	// usage" instead.
	defer func() {
		if r := recover(); r != nil {
			log.Warn("fg manager panicked; app-usage disabled", "recover", fmt.Sprint(r))
		}
	}()

	// Needed to launch a process into another session as that user. LocalSystem
	// holds these; enable them so CreateProcessAsUser/WTSQueryUserToken succeed.
	for _, p := range []string{"SeTcbPrivilege", "SeAssignPrimaryTokenPrivilege", "SeIncreaseQuotaPrivilege"} {
		if err := enablePrivilege(p); err != nil {
			log.Debug("fg enable privilege failed", "priv", p, "err", err.Error())
		}
	}

	var hp *helperProc
	stop := func() {
		if hp != nil {
			hp.cancel()
			hp = nil
		}
	}
	defer stop()

	ensure := func() {
		sid := activeConsoleSession()
		if sid == 0 || sid == noSession {
			stop() // no interactive user
			return
		}
		if hp != nil {
			select {
			case <-hp.done: // helper exited — relaunch
				stop()
			default:
				if hp.session == sid {
					return // healthy helper for the current session
				}
				stop() // active session changed
			}
		}
		newHp, err := launchHelper(ctx, exePath, sid, snd, tracker, log)
		if err != nil {
			log.Debug("fg helper launch failed", "session", sid, "err", err.Error())
			return
		}
		hp = newHp
	}

	select {
	case <-ctx.Done():
		return
	case <-time.After(20 * time.Second): // let the desktop settle after boot/login
		ensure()
	}
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ensure()
		}
	}
}

// activeConsoleSession returns the session id currently attached to the physical
// console, or noSession when none.
func activeConsoleSession() uint32 {
	r, _, _ := procWTSGetActiveConsoleSessionId.Call()
	return uint32(r)
}

// launchHelper starts the `--fg-helper` process inside the given session as that
// session's user, wiring the child's stdout to a pipe the agent reads.
func launchHelper(ctx context.Context, exePath string, session uint32, snd *sender.Sender, tracker *collector.SessionTracker, log *slog.Logger) (*helperProc, error) {
	var token windows.Token
	if err := windows.WTSQueryUserToken(session, &token); err != nil {
		return nil, err
	}
	defer token.Close()

	var readEnd, writeEnd windows.Handle
	sa := &windows.SecurityAttributes{InheritHandle: 1}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	if err := windows.CreatePipe(&readEnd, &writeEnd, sa, 0); err != nil {
		return nil, err
	}
	// The parent's read end must not be inherited by the child.
	_ = windows.SetHandleInformation(readEnd, windows.HANDLE_FLAG_INHERIT, 0)

	si := &windows.StartupInfo{}
	si.Cb = uint32(unsafe.Sizeof(*si))
	si.Flags = windows.STARTF_USESTDHANDLES
	si.StdOutput = writeEnd
	si.StdErr = writeEnd
	si.Desktop = windows.StringToUTF16Ptr(`winsta0\default`) // interactive desktop

	appName := windows.StringToUTF16Ptr(exePath)
	cmdline := windows.StringToUTF16Ptr(`"` + exePath + `" --fg-helper`)

	var pi windows.ProcessInformation
	err := windows.CreateProcessAsUser(token, appName, cmdline, nil, nil, true,
		windows.CREATE_NO_WINDOW, nil, nil, si, &pi)
	_ = windows.CloseHandle(writeEnd) // child owns the write end now
	if err != nil {
		_ = windows.CloseHandle(readEnd)
		return nil, err
	}
	_ = windows.CloseHandle(pi.Thread)

	hp := &helperProc{session: session, done: make(chan struct{})}
	hctx, cancel := context.WithCancel(ctx)
	hp.cancel = cancel

	// Reader: consume per-app tallies until the child exits (EOF). Keys with the
	// "__" prefix are reserved control fields (never app names): "__idle" carries
	// seconds since last input and feeds the shared session tracker; unknown
	// control keys are dropped.
	go func() {
		defer func() { _ = recover() }() // never let a reader panic crash the agent
		defer close(hp.done)
		f := os.NewFile(uintptr(readEnd), "tolikopa-fg")
		defer f.Close() // closes readEnd
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 8192), 1<<20)
		for sc.Scan() {
			var m map[string]int
			if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
				continue
			}
			for name, sec := range m {
				if strings.HasPrefix(name, "__") {
					if name == "__idle" {
						tracker.ReportIdle(sec)
					}
					continue
				}
				snd.AddAppFocus(name, sec)
			}
		}
	}()

	// Terminator: kill and reap the child when its context is cancelled.
	go func() {
		<-hctx.Done()
		_ = windows.TerminateProcess(pi.Process, 0)
		_ = windows.CloseHandle(pi.Process)
	}()

	log.Debug("fg helper launched", "session", session, "pid", pi.ProcessId)
	return hp, nil
}

// enablePrivilege enables a named privilege in the current process token.
func enablePrivilege(name string) error {
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &tok); err != nil {
		return err
	}
	defer tok.Close()
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr(name), &luid); err != nil {
		return err
	}
	tp := windows.Tokenprivileges{PrivilegeCount: 1}
	tp.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
	return windows.AdjustTokenPrivileges(tok, false, &tp, 0, nil, nil)
}
