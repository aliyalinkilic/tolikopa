//go:build windows

package winproc

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// StartupInfo is a STARTUPINFOEX restricted to an explicit inheritable-handle
// list. Use it with bInheritHandles=true and EXTENDED_STARTUPINFO_PRESENT in the
// creation flags (see Flags).
//
// Why: CreateProcess(bInheritHandles=true) with a plain STARTUPINFO hands the
// child EVERY inheritable handle in the process at that instant. The agent
// creates inheritable pipe ends for two different children on two different
// goroutines, so without a handle list a user-context helper launched while the
// SYSTEM removal worker's pipes are open would inherit the worker's stdout write
// end and could forge removal results. With the list, each child receives only
// the handles named here, whatever else happens to be inheritable concurrently.
type StartupInfo struct {
	Ex      windows.StartupInfoEx
	attrs   *windows.ProcThreadAttributeListContainer
	handles []windows.Handle
}

// Flags is the creation flag that must be OR-ed in so CreateProcess* reads the
// extended structure.
const Flags = windows.EXTENDED_STARTUPINFO_PRESENT

// NewStartupInfo builds a STARTUPINFOEX with STARTF_USESTDHANDLES and the given
// std handles, whose attribute list allows inheriting exactly the distinct
// non-zero handles among stdin/stdout/stderr. Call Close after CreateProcess*
// returns (success or failure).
func NewStartupInfo(stdin, stdout, stderr windows.Handle) (*StartupInfo, error) {
	var hs []windows.Handle
	for _, h := range []windows.Handle{stdin, stdout, stderr} {
		if h == 0 || h == windows.InvalidHandle {
			continue
		}
		dup := false
		for _, have := range hs {
			if have == h {
				dup = true // the list must not name a handle twice
				break
			}
		}
		if !dup {
			hs = append(hs, h)
		}
	}
	if len(hs) == 0 {
		return nil, errors.New("winproc: no handles to inherit")
	}

	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, err
	}
	si := &StartupInfo{attrs: attrs, handles: hs}
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST,
		unsafe.Pointer(&si.handles[0]), uintptr(len(si.handles))*unsafe.Sizeof(si.handles[0])); err != nil {
		attrs.Delete()
		return nil, err
	}
	si.Ex.Cb = uint32(unsafe.Sizeof(si.Ex))
	si.Ex.Flags = windows.STARTF_USESTDHANDLES
	si.Ex.StdInput = stdin
	si.Ex.StdOutput = stdout
	si.Ex.StdErr = stderr
	si.Ex.ProcThreadAttributeList = attrs.List()
	return si, nil
}

// Base returns the pointer to pass as lpStartupInfo.
func (s *StartupInfo) Base() *windows.StartupInfo { return &s.Ex.StartupInfo }

// Close frees the attribute list. Safe to call more than once.
func (s *StartupInfo) Close() {
	if s.attrs != nil {
		s.attrs.Delete()
		s.attrs = nil
	}
}
