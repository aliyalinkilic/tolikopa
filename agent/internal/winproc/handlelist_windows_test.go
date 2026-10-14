//go:build windows

package winproc

import (
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A real CreateProcess through the restricted handle list: the child must still
// get working std handles. (hostname.exe ships with every Windows and only
// prints the computer name.)
func TestChildGetsItsStdHandles(t *testing.T) {
	sa := &windows.SecurityAttributes{InheritHandle: 1}
	sa.Length = uint32(unsafe.Sizeof(*sa))

	var rd, wr windows.Handle
	if err := windows.CreatePipe(&rd, &wr, sa, 0); err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(rd)
	// The read end stays in this process only.
	if err := windows.SetHandleInformation(rd, windows.HANDLE_FLAG_INHERIT, 0); err != nil {
		t.Fatal(err)
	}
	// A second inheritable handle that is NOT in the list, like the other spawner's pipe.
	var decoyR, decoyW windows.Handle
	if err := windows.CreatePipe(&decoyR, &decoyW, sa, 0); err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(decoyR)
	defer windows.CloseHandle(decoyW)

	nul, err := windows.CreateFile(windows.StringToUTF16Ptr("NUL"), windows.GENERIC_READ, 0, sa, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(nul)

	si, err := NewStartupInfo(nul, wr, wr)
	if err != nil {
		t.Fatal(err)
	}
	defer si.Close()

	sys, _ := windows.GetSystemDirectory()
	exe := sys + `\hostname.exe`
	cmd := windows.StringToUTF16Ptr(`"` + exe + `"`)
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(windows.StringToUTF16Ptr(exe), cmd, nil, nil, true,
		Flags|windows.CREATE_NO_WINDOW, nil, nil, si.Base(), &pi); err != nil {
		t.Fatalf("CreateProcess with a handle list: %v", err)
	}
	windows.CloseHandle(wr) // the child holds its own copy now
	defer windows.CloseHandle(pi.Thread)
	defer windows.CloseHandle(pi.Process)

	var out strings.Builder
	buf := make([]byte, 512)
	for {
		var n uint32
		err := windows.ReadFile(rd, buf, &n, nil)
		out.Write(buf[:n])
		if err != nil || n == 0 {
			if err != nil && err != windows.ERROR_BROKEN_PIPE && err != syscall.ERROR_BROKEN_PIPE {
				t.Fatalf("read: %v", err)
			}
			break
		}
	}
	if _, err := windows.WaitForSingleObject(pi.Process, 10000); err != nil {
		t.Fatal(err)
	}
	var code uint32
	_ = windows.GetExitCodeProcess(pi.Process, &code)
	if code != 0 || strings.TrimSpace(out.String()) == "" {
		t.Fatalf("child exit %d, output %q", code, out.String())
	}
}
