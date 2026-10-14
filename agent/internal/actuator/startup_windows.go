//go:build windows

package actuator

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/aliyalinkilic/tolikopa/agent/internal/collector"
)

// Startup control is pure registry: Tolikopa writes the SAME StartupApproved value
// that Task Manager's "Startup apps" tab and Settings write, and NEVER touches
// the Run value itself.
//
// That distinction is the whole safety story here:
//   - deleting the Run value destroys information (the command line) that nothing
//     on the machine can reconstruct, and the change is not reversible;
//   - writing StartupApproved is exactly what the OS's own UI does, is visible to
//     the user in that UI, and can be undone by the user or by us (see enable).
//
// Blob layout (undocumented but stable since Windows 8; the collector's
// startupApprovedEnabled decodes the same 12 bytes):
//
//	byte  0     state: 0x02 = enabled, 0x03 = disabled
//	bytes 1-3   zero
//	bytes 4-11  FILETIME of the change (little-endian; all-zero when enabling,
//	            which is what the Windows UI writes)
const (
	approvedEnabledByte  = 0x02
	approvedDisabledByte = 0x03
	approvedBlobLen      = 12
)

// setStartupApproved disables (enabled=false) or re-enables every observed
// registry location of one catalog logon entry.
func setStartupApproved(ctx context.Context, log *slog.Logger, targets []collector.StartupTarget, enabled bool) Result {
	if len(targets) == 0 {
		return Result{Outcome: OutcomeNotPresent}
	}

	blob := approvedBlob(enabled, time.Now())
	var (
		wrote     int
		firstCode string
	)
	for _, t := range targets {
		if ctx.Err() != nil {
			return Result{Outcome: OutcomeTimeout}
		}
		// RunOnce has no StartupApproved counterpart. The only way to stop it
		// would be deleting the value, which we never do.
		root, path, ok := collector.StartupApprovedPath(t)
		if !ok {
			if firstCode == "" {
				firstCode = ErrNoApprovedKey
			}
			log.Debug("startup entry has no StartupApproved key (RunOnce), skipping",
				"item_id", t.ItemID)
			continue
		}
		if err := writeApproved(root, path, t.ValueName, blob); err != nil {
			if firstCode == "" {
				firstCode = hresultFromErr(err)
			}
			log.Warn("startup approved write failed",
				"item_id", t.ItemID, "code", hresultFromErr(err))
			continue
		}
		wrote++
	}

	switch {
	case wrote > 0 && firstCode == "":
		return Result{Outcome: OutcomeOK}
	case wrote > 0:
		// Partial success: some hive was not writable. Report failure so the
		// budget counts it and the panel does not claim the item is handled.
		return Result{Outcome: OutcomeFailed, ErrorCode: firstCode}
	default:
		return Result{Outcome: OutcomeFailed, ErrorCode: firstCode}
	}
}

// writeApproved creates the StartupApproved subkey if needed and writes one
// state blob. It only ever writes; it never enumerates or deletes.
func writeApproved(root registry.Key, path, valueName string, blob []byte) error {
	k, _, err := registry.CreateKey(root, path, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetBinaryValue(valueName, blob)
}

// approvedBlob builds the 12-byte state value.
func approvedBlob(enabled bool, now time.Time) []byte {
	b := make([]byte, approvedBlobLen)
	if enabled {
		// The Windows UI writes 0x02 followed by zeros when re-enabling, so match
		// it byte for byte rather than inventing a timestamped "enabled" variant.
		b[0] = approvedEnabledByte
		return b
	}
	b[0] = approvedDisabledByte
	ft := windows.NsecToFiletime(now.UnixNano())
	binary.LittleEndian.PutUint32(b[4:8], ft.LowDateTime)
	binary.LittleEndian.PutUint32(b[8:12], ft.HighDateTime)
	return b
}

// hresultFromErr renders a Windows error as an HRESULT-shaped CODE. Codes only —
// the message text may name a registry path and is a forbidden data class.
func hresultFromErr(err error) string {
	if err == nil {
		return ""
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return fmt.Sprintf("0x%08X", 0x80070000|uint32(errno)&0xFFFF) // HRESULT_FROM_WIN32
	}
	return "0x80004005" // E_FAIL: unspecified
}
