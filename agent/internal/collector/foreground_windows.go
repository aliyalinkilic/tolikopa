//go:build windows

package collector

import (
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PRIVACY: this file resolves ONLY the foreground process's base NAME. It calls
// GetForegroundWindow to obtain the window HANDLE, then maps the handle to its
// owning PID and the PID to the executable's base name. It deliberately never
// calls GetWindowText / InternalGetWindowText / GetWindowTextLength or any other
// API that would reveal the window TITLE, document name, browser tab, or URL.

// modKernel32 is declared in session_windows.go (same package); reuse it here.
var (
	modUser32               = windows.NewLazySystemDLL("user32.dll")
	procGetForegroundWindow = modUser32.NewProc("GetForegroundWindow")
	procGetWindowThreadPID  = modUser32.NewProc("GetWindowThreadProcessId")
	procGetLastInputInfo    = modUser32.NewProc("GetLastInputInfo")
	procGetTickCount        = modKernel32.NewProc("GetTickCount")
)

type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

// ForegroundAppName returns the normalized base name (lowercase, no ".exe") of the
// process that owns the current foreground window. NAME ONLY — the window title is
// never read. ok is false when there is no foreground window (locked/secure
// desktop) or the process name cannot be resolved.
func ForegroundAppName() (name string, ok bool) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return "", false
	}
	var pid uint32
	procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return "", false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", false
	}
	return ValidAppName(filepath.Base(windows.UTF16ToString(buf[:size])))
}

// IdleSeconds returns seconds since the last keyboard/mouse input in the calling
// thread's session. Meaningful only from inside the interactive user session (the
// helper) — it measures whether the user is actively using the machine, not what
// they are doing.
func IdleSeconds() int {
	info := lastInputInfo{cbSize: uint32(unsafe.Sizeof(lastInputInfo{}))}
	r, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0
	}
	tick, _, _ := procGetTickCount.Call()
	now := uint32(tick)
	if now < info.dwTime {
		return 0
	}
	return int((now - info.dwTime) / 1000)
}
