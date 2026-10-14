//go:build windows

package actuator

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"golang.org/x/sys/windows/registry"

	"github.com/aliyalinkilic/tolikopa/agent/internal/appcatalog"
)

// deprovisionedKey is what actually stops Windows from re-adding a package to new
// user profiles and after feature updates. Removing a package without writing it
// is only half a removal.
const deprovisionedKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\Appx\AppxAllUserStore\Deprovisioned`

// asyncPollInterval is how often the worker checks a WinRT async operation.
const asyncPollInterval = 250 * time.Millisecond

// asyncBudget bounds the worker's own wait. The parent kills the whole process at
// 180 s; this is the graceful path, so it stops a little earlier.
const asyncBudget = 170 * time.Second

// RunAppxWorker is the `--appx-worker` entry point: a short-lived child of the
// agent that performs ONE package operation and exits.
//
// It re-validates the family against the compiled catalog and the never-remove
// table before touching any Windows API. The parent already did exactly the same
// check — that is the point. The allowlist is enforced twice, in two processes,
// so a bug (or a memory corruption, or a future refactor) in the long-running
// service cannot by itself cause a package to be removed.
func RunAppxWorker() int {
	log := workerLogger()

	req, err := readWorkerRequest()
	if err != nil {
		log.Error("appx worker: unreadable request", "err", err.Error())
		writeWorkerResponse(workerResponse{Outcome: wOutcomeFailed, HResult: ErrWorkerIO})
		return exitFailed
	}

	// ── Allowlist check #2 (independent of the parent's) ──────────────────────
	// "resolve" reads nothing but package identities and validates each family in
	// workerResolve; the mutating ops are gated on a single family here.
	// canonFamily is the CATALOG's spelling; req.Family is not used past this point.
	var canonFamily string
	if req.Op != opResolve {
		var ok bool
		canonFamily, ok = workerFamilyAllowed(req.Family)
		if !ok {
			log.Error("appx worker refused family (not in catalog, or protected)", "family", req.Family)
			writeWorkerResponse(workerResponse{Outcome: wOutcomeProtected, HResult: ErrWorkerRefused})
			return exitRejected
		}
		if req.Op == opRemove && !fullNameMatchesFamily(req.FullName, canonFamily) {
			log.Error("appx worker refused full name (does not belong to the family)", "family", canonFamily)
			writeWorkerResponse(workerResponse{Outcome: wOutcomeProtected, HResult: ErrWorkerRefused})
			return exitRejected
		}
	}

	// ── The WinRT binding table must be complete before any COM call ──────────
	// constantsUnverified checks the VALUES, not just the boolean: a table where
	// someone flipped the flag but left RemovalOptions at its placebo 0 would
	// otherwise remove the package for SYSTEM only and report success. The SDK
	// evidence behind each value is recorded in winrt_constants_windows.go.
	if constantsUnverified() {
		log.Error("appx worker: WinRT binding constants are UNVERIFIED — refusing to act. " +
			"Fill in EVERY value in internal/actuator/winrt_constants_windows.go from the " +
			"Windows SDK (IIDs, vtable slots AND RemovalOptions) and set winrtConstantsVerified.")
		writeWorkerResponse(workerResponse{Outcome: wOutcomeFailed, HResult: ErrWinRTUnverified})
		return exitFailed
	}

	switch req.Op {
	case opResolve:
		return finishWorker(log, workerResolve(log, req.Families))
	case opRemove:
		return finishWorker(log, workerRemove(log, req.FullName))
	case opDeprovision:
		return finishWorker(log, workerDeprovision(log, canonFamily))
	default:
		log.Error("appx worker: unknown op", "op", req.Op)
		writeWorkerResponse(workerResponse{Outcome: wOutcomeProtected, HResult: ErrWorkerRefused})
		return exitRejected
	}
}

// finishWorker writes the response line and maps the outcome to an exit code.
func finishWorker(log *slog.Logger, resp workerResponse) int {
	writeWorkerResponse(resp)
	switch resp.Outcome {
	case wOutcomeOK:
		return exitOK
	case wOutcomeAbsent:
		return exitAbsent
	case wOutcomeTimeout:
		return exitTimeout
	case wOutcomeProtected:
		return exitRejected
	default:
		log.Debug("appx worker failed", "hresult", resp.HResult)
		return exitFailed
	}
}

// workerLogger writes diagnostics to stderr. stdout is the protocol channel and
// must carry exactly one JSON line.
func workerLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// workerFamilyAllowed is the child's own copy of the authority rule: the family
// must be named by the compiled catalog AND must not be in the never-remove
// table. Empty/unknown input fails closed.
// It returns the CATALOG's spelling of the family, and every downstream use must
// prefer that over the request's: EqualFold applies Unicode simple folding, so a
// request could carry a visually different string (Kelvin sign U+212A folds to
// "k") that passes the check and would then be written verbatim into a registry
// key name.
func workerFamilyAllowed(family string) (string, bool) {
	f := strings.TrimSpace(family)
	if f == "" || appcatalog.IsForbiddenFamily(f) {
		return "", false
	}
	for _, it := range appcatalog.Kind(appcatalog.KindApp) {
		if strings.EqualFold(it.Family, f) {
			return it.Family, true
		}
	}
	return "", false
}

// fullNameMatchesFamily checks that a PackageFullName really belongs to the
// family that was authorised: Name_Version_Arch_ResourceId_PublisherId reduces to
// Name_PublisherId. Without this, an authorised family name could be paired with
// an arbitrary full name.
func fullNameMatchesFamily(fullName, family string) bool {
	parts := strings.Split(strings.TrimSpace(fullName), "_")
	if len(parts) < 3 {
		return false
	}
	return strings.EqualFold(parts[0]+"_"+parts[len(parts)-1], strings.TrimSpace(family))
}

// ── WinRT plumbing ───────────────────────────────────────────────────────────
//
// Every vtable slot and IID used below is documented and SDK-verified in
// winrt_constants_windows.go; constantsUnverified() guards the entry point.

// workerResolve answers "which packages do these families actually have deployed
// on this machine, right now". It changes nothing; it is the read both the
// inventory and the removal path need, because the registry cannot answer it
// (see appx_resolve_windows.go).
func workerResolve(log *slog.Logger, families []string) workerResponse {
	// Every family is held to the compiled catalog first — the child's own copy of
	// the authority rule, not the parent's. A family outside it is never even
	// named to the deployment API.
	canon := make([]string, 0, len(families))
	seen := map[string]bool{}
	for _, f := range families {
		c, ok := workerFamilyAllowed(f)
		if !ok {
			log.Error("appx worker refused family in resolve (not in catalog, or protected)")
			continue
		}
		if !seen[c] {
			seen[c] = true
			canon = append(canon, c)
		}
	}
	if len(canon) == 0 {
		return workerResponse{Outcome: wOutcomeAbsent}
	}

	found, resp := resolveFamilies(log, canon)
	if resp.Outcome != wOutcomeOK {
		return resp
	}

	// Belt and braces: the enumeration came from Windows, but every name is still
	// held to the family the catalog authorised before it can become a removal
	// target. A name that fails this is dropped, not removed.
	out := make(map[string][]string, len(found))
	for family, names := range found {
		kept := make([]string, 0, len(names))
		for _, n := range names {
			if fullNameMatchesFamily(n, family) {
				kept = append(kept, n)
			} else {
				log.Error("resolve: dropped a package that does not belong to the family")
			}
		}
		if len(kept) > 0 {
			out[family] = kept
		}
	}
	if len(out) == 0 {
		return workerResponse{Outcome: wOutcomeAbsent}
	}
	return workerResponse{Outcome: wOutcomeOK, Packages: out}
}

// workerRemove removes one package for all users.
func workerRemove(log *slog.Logger, fullName string) workerResponse {
	return withPackageManager(log, &iidPackageManager2, func(pm *ole.IUnknown) workerResponse {
		op, err := callAsyncWithString(pm, slotRemovePackageWithOptionsAsync, fullName,
			uintptr(removalOptionsRemoveForAllUsers))
		if err != nil {
			return failedResponse(err)
		}
		defer releaseUnknown(op)
		return awaitAsync(log, op)
	})
}

// workerDeprovision deprovisions the family and — the part that actually sticks —
// records it in AppxAllUserStore\Deprovisioned so Windows Update and newly
// created profiles do not bring the package back.
func workerDeprovision(log *slog.Logger, family string) workerResponse {
	// DeprovisionPackageForAllUsersAsync lives on IPackageManager8 (Windows 10
	// 1809+), not on IPackageManager2 — a separate interface is queried here.
	resp := withPackageManager(log, &iidPackageManager8, func(pm *ole.IUnknown) workerResponse {
		op, err := callAsyncWithString(pm, slotDeprovisionPackageForAllUsersAsync, family)
		if err != nil {
			return failedResponse(err)
		}
		defer releaseUnknown(op)
		return awaitAsync(log, op)
	})
	// A family that was never provisioned answers ERROR_NOT_FOUND. There is then
	// nothing to deprovision — but the suppression key below is still exactly
	// what we want on this machine, so the write happens either way.
	if resp.Outcome != wOutcomeOK && resp.Outcome != wOutcomeAbsent {
		return resp
	}
	if err := writeDeprovisioned(family); err != nil {
		log.Error("deprovisioned registry write failed", "code", hresultFromErr(err))
		return workerResponse{Outcome: wOutcomeFailed, HResult: hresultFromErr(err)}
	}
	return resp
}

// writeDeprovisioned creates the suppression key for one package family.
func writeDeprovisioned(family string) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, deprovisionedKey+`\`+family,
		registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	return k.Close()
}

// withPackageManager sets up a WinRT apartment on a locked OS thread, activates
// PackageManager, queries the requested interface (iid) and runs fn against it.
// Every COM object is released, and the whole thing is wrapped in recover —
// although this process is disposable by design, a panic here would lose the
// protocol line the parent is waiting for.
func withPackageManager(log *slog.Logger, iid *ole.GUID, fn func(pm *ole.IUnknown) workerResponse) (resp workerResponse) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("appx worker panicked", "recover", fmt.Sprint(r))
			resp = workerResponse{Outcome: wOutcomeFailed, HResult: ErrWorkerIO}
		}
	}()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// RO_INIT_MULTITHREADED: this process has no message pump.
	if err := ole.RoInitialize(1); err != nil {
		return failedResponse(err)
	}
	defer ole.CoUninitialize()

	inspectable, err := ole.RoActivateInstance(classPackageManager)
	if err != nil {
		return failedResponse(err)
	}
	defer releaseUnknown(&inspectable.IUnknown)

	pm, err := queryInterface(&inspectable.IUnknown, iid)
	if err != nil {
		return failedResponse(err)
	}
	defer releaseUnknown(pm)

	return fn(pm)
}

// awaitAsync polls IAsyncInfo until the operation leaves the Started state, then
// closes it. Polling (rather than an IAsyncOperationCompletedHandler) keeps the
// worker free of a callback vtable we would also have to hand-build.
func awaitAsync(log *slog.Logger, op *ole.IUnknown) workerResponse {
	info, err := queryInterface(op, &iidAsyncInfo)
	if err != nil {
		return failedResponse(err)
	}
	defer releaseUnknown(info)
	defer func() { _, _ = callSlot(info, slotAsyncInfoClose) }()

	deadline := time.Now().Add(asyncBudget)
	for {
		var status uint32
		if _, err := callSlot(info, slotAsyncInfoStatus, uintptr(unsafe.Pointer(&status))); err != nil {
			return failedResponse(err)
		}
		switch status {
		case asyncCompleted:
			return workerResponse{Outcome: wOutcomeOK}
		case asyncCanceled:
			return workerResponse{Outcome: wOutcomeTimeout}
		case asyncError:
			var code int32
			_, _ = callSlot(info, slotAsyncInfoErrorCode, uintptr(unsafe.Pointer(&code)))
			hres := fmt.Sprintf("0x%08X", uint32(code))
			// 0x80073CFA is ERROR_REMOVE_FAILED: Windows refused the removal. For
			// an inbox system app that is permanent; fleet data showed it on EVERY
			// item of one machine too, i.e. a broken deployment store. Reported as
			// a refusal; the parent backs it off for weeks, not forever (budget.go).
			if uint32(code) == 0x80073CFA {
				log.Error("removal refused by Windows (protected package)", "hresult", hres)
				return workerResponse{Outcome: wOutcomeProtected, HResult: hres}
			}
			// 0x80070490 is "no such package identity". The desired state is that
			// it is gone, and it is.
			if uint32(code) == errNotFound {
				log.Info("package identity not deployed; nothing to do")
				return workerResponse{Outcome: wOutcomeAbsent, HResult: hres}
			}
			return workerResponse{Outcome: wOutcomeFailed, HResult: hres}
		}
		if time.Now().After(deadline) {
			_, _ = callSlot(info, slotAsyncInfoCancel)
			return workerResponse{Outcome: wOutcomeTimeout}
		}
		time.Sleep(asyncPollInterval)
	}
}

// callAsyncWithString invokes a vtable slot whose signature is
// (HSTRING, [extra args…], out IAsyncOperation**).
func callAsyncWithString(obj *ole.IUnknown, slot int, s string, extra ...uintptr) (*ole.IUnknown, error) {
	h, err := ole.NewHString(s)
	if err != nil {
		return nil, err
	}
	defer ole.DeleteHString(h)

	var out *ole.IUnknown
	args := make([]uintptr, 0, len(extra)+2)
	args = append(args, uintptr(h))
	args = append(args, extra...)
	args = append(args, uintptr(unsafe.Pointer(&out)))
	if _, err := callSlot(obj, slot, args...); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, errors.New("winrt: nil async operation")
	}
	return out, nil
}

// callSlot invokes one vtable slot on a COM object, passing `this` first.
func callSlot(obj *ole.IUnknown, slot int, args ...uintptr) (uintptr, error) {
	fn, err := vtblSlot(obj, slot)
	if err != nil {
		return 0, err
	}
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(unsafe.Pointer(obj)))
	all = append(all, args...)
	hr, _, _ := syscall.SyscallN(fn, all...)
	if hr != 0 {
		return hr, ole.NewError(hr)
	}
	return hr, nil
}

// queryInterface is a raw QueryInterface (vtable slot 0). go-ole's own helper
// returns an IDispatch, which WinRT interfaces are not.
func queryInterface(obj *ole.IUnknown, iid *ole.GUID) (*ole.IUnknown, error) {
	if iid == nil || *iid == (ole.GUID{}) {
		// A zero IID is what an unverified constant looks like. Fail loudly
		// instead of asking COM for "interface nothing".
		return nil, errors.New("winrt: unverified interface id")
	}
	var out *ole.IUnknown
	fn, err := vtblSlot(obj, 0)
	if err != nil {
		return nil, err
	}
	hr, _, _ := syscall.SyscallN(fn,
		uintptr(unsafe.Pointer(obj)),
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(&out)))
	if hr != 0 {
		return nil, ole.NewError(hr)
	}
	return out, nil
}

// vtblSlot returns the function pointer at a vtable index, refusing indices that
// were never established (slotInvalid) so an unverified constant can never be
// dereferenced.
func vtblSlot(obj *ole.IUnknown, slot int) (uintptr, error) {
	if obj == nil {
		return 0, errors.New("winrt: nil interface")
	}
	if slot < 0 {
		return 0, errors.New("winrt: unverified vtable slot")
	}
	// unsafe.Add (not uintptr arithmetic) so the pointer stays a pointer for the
	// compiler and for `go vet`'s unsafeptr check.
	p := unsafe.Add(unsafe.Pointer(obj.RawVTable), uintptr(slot)*unsafe.Sizeof(uintptr(0)))
	return *(*uintptr)(p), nil
}

// releaseUnknown releases a COM object, tolerating nil.
func releaseUnknown(obj *ole.IUnknown) {
	if obj != nil {
		obj.Release()
	}
}

// failedResponse renders an error as a code-only response. ERROR_NOT_FOUND is
// folded into "absent" here too: a COM call can answer it synchronously as well
// as through IAsyncInfo, and it means the same thing on both paths.
func failedResponse(err error) workerResponse {
	var oleErr *ole.OleError
	if errors.As(err, &oleErr) {
		code := uint32(oleErr.Code())
		outcome := wOutcomeFailed
		if code == errNotFound {
			outcome = wOutcomeAbsent
		}
		return workerResponse{Outcome: outcome, HResult: fmt.Sprintf("0x%08X", code)}
	}
	return workerResponse{Outcome: wOutcomeFailed, HResult: hresultFromErr(err)}
}
