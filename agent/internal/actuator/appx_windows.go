//go:build windows

package actuator

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Appx removal runs OUT OF PROCESS, in a short-lived child of this same exe.
//
// WHY: package removal talks to AppXSvc over RPC and can hang for minutes (or,
// on a machine whose deployment stack is damaged, forever). A wedged COM call
// leaks its goroutine AND its locked OS thread permanently. That is tolerable for
// the 60 s read-only WUA query in collector/updates_windows.go, which runs a few
// times a day and only reads; it is not tolerable for a mutation that a service
// running for months may perform repeatedly. A child process can simply be
// terminated, and every leaked handle, thread and COM apartment goes with it.
//
// The child is launched with a fixed literal argv — "<exe>" --appx-worker — with
// CREATE_NO_WINDOW, as SYSTEM in session 0 (CreateProcess, deliberately NOT
// CreateProcessAsUser: removal is a machine-wide operation and must not depend on
// anyone being logged on). It receives one JSON line on stdin and answers with
// one JSON line on stdout. Nothing else crosses the boundary: no token, no
// config, no paths.
const (
	appxWorkerTimeout = 180 * time.Second
	appxWorkerArg     = "--appx-worker"
	// exitTerminated is the code we terminate a wedged worker with.
	exitTerminated = 2
)

type appxHost struct {
	log *slog.Logger
	exe string
}

func newAppxHost(log *slog.Logger, exePath string) *appxHost {
	if exePath == "" {
		if p, err := os.Executable(); err == nil {
			exePath = p
		}
	}
	return &appxHost{log: log, exe: exePath}
}

// resolveFamilies asks the worker which PackageFullNames each family currently
// has deployed on this machine. It is ONE child process for the whole catalog,
// and it is what both the inventory and the removal path build on: the registry
// view cannot answer this question (see appx_resolve_windows.go).
//
// Names that do not belong to the family that was asked about are dropped here
// too — the worker applies the same rule to its own enumeration, so the check
// exists in both processes, the way the allowlist itself is enforced twice.
func (h *appxHost) resolveFamilies(ctx context.Context, families []string) (map[string][]string, Result) {
	if h.exe == "" {
		return nil, Result{Outcome: OutcomeFailed, ErrorCode: ErrWorkerIO}
	}
	if len(families) == 0 {
		return map[string][]string{}, Result{Outcome: OutcomeOK}
	}

	resp, exit := h.exchange(ctx, workerRequest{Op: opResolve, Families: families})
	res := mapWorkerResponse(resp, exit)
	switch res.Outcome {
	case OutcomeOK:
	case OutcomeNotPresent:
		// Nothing in the whole catalog is deployed here. A real answer.
		return map[string][]string{}, Result{Outcome: OutcomeOK}
	default:
		h.log.Warn("live package resolution failed; inventory falls back to the registry hint",
			"outcome", res.Outcome, "code", res.ErrorCode)
		return nil, res
	}

	out := make(map[string][]string, len(resp.Packages))
	for family, names := range resp.Packages {
		kept := make([]string, 0, len(names))
		for _, n := range names {
			if fullNameMatchesFamily(n, family) {
				kept = append(kept, n)
				continue
			}
			h.log.Error("resolve returned a package outside the requested family; dropped",
				"family", family)
		}
		if len(kept) > 0 {
			out[family] = kept
		}
	}
	h.log.Debug("resolved live packages", "families", len(out))
	return out, Result{Outcome: OutcomeOK}
}

// removeAndDeprovision brings one package family to the desired state: no
// instance deployed for any user, and suppressed so it does not come back.
//
// fullNames MUST come from resolveFamilies — that is, from the deployment API at
// the start of this pass. They used to be harvested from the AppxAllUserStore
// registry view, which lists every version ever deployed and keeps them after
// removal; those names answer 0x80070490 (ERROR_NOT_FOUND) and the version
// actually registered was often not in the list at all, so nothing was ever
// removed. Do not reintroduce a registry-derived target.
//
// The deprovision step is PART OF the removal, not an afterthought: without it
// Windows Update and new user profiles simply re-add the package at the next
// opportunity, and the agent would then remove it again on the next cycle,
// forever.
func (h *appxHost) removeAndDeprovision(ctx context.Context, family string, fullNames []string) Result {
	if h.exe == "" {
		return Result{Outcome: OutcomeFailed, ErrorCode: ErrWorkerIO}
	}

	removed := 0
	for _, full := range fullNames {
		if ctx.Err() != nil {
			return Result{Outcome: OutcomeTimeout}
		}
		switch r := h.call(ctx, workerRequest{Op: opRemove, Family: family, FullName: full}); r.Outcome {
		case OutcomeOK:
			removed++
		case OutcomeNotPresent:
			// Gone between the resolve and the removal — a bundle removal takes
			// its architecture and resource packages with it, and a user session
			// may have uninstalled it meanwhile. Not a failure; keep going. The
			// old code returned here, so one stale name aborted the whole item.
		default:
			return r
		}
	}

	// Deprovision even when nothing was registered: suppressing the family is the
	// desired end state either way, and it is what stops the package from being
	// added to the next profile created on this machine.
	if dep := h.call(ctx, workerRequest{Op: opDeprovision, Family: family}); dep.Outcome != OutcomeOK &&
		dep.Outcome != OutcomeNotPresent {
		return dep
	}
	if removed == 0 {
		return Result{Outcome: OutcomeNotPresent}
	}
	return Result{Outcome: OutcomeOK}
}

// call runs one worker round-trip and folds the answer into the model's outcome
// vocabulary.
func (h *appxHost) call(ctx context.Context, req workerRequest) Result {
	res := mapWorkerResponse(h.exchange(ctx, req))
	if res.Outcome != OutcomeOK && res.Outcome != OutcomeNotPresent {
		// Which STEP failed is not otherwise recoverable: the worker's stderr is
		// deliberately unconnected, and the audit row carries only the item and a
		// code. Without this line a 0x… in the panel cannot be traced to a stage.
		h.log.Warn("appx worker step did not succeed",
			"op", req.Op, "family", req.Family, "outcome", res.Outcome, "code", res.ErrorCode)
	}
	return res
}

// exchange spawns one worker, feeds it the request and returns its raw answer
// plus the child's exit code.
func (h *appxHost) exchange(ctx context.Context, req workerRequest) (resp workerResponse, exit uint32) {
	// The pipe/handle juggling below is delicate; a panic here must never take
	// down a service that runs as SYSTEM on every machine in the fleet.
	defer func() {
		if r := recover(); r != nil {
			h.log.Error("appx host panicked", "recover", fmt.Sprint(r))
			resp, exit = workerResponse{Outcome: wOutcomeFailed, HResult: ErrWorkerIO}, exitFailed
		}
	}()

	line, err := json.Marshal(req)
	if err != nil {
		return workerResponse{Outcome: wOutcomeFailed, HResult: ErrWorkerIO}, exitFailed
	}

	proc, err := h.spawn()
	if err != nil {
		h.log.Error("appx worker launch failed", "err", err.Error())
		return workerResponse{Outcome: wOutcomeFailed, HResult: ErrWorkerIO}, exitFailed
	}
	defer proc.close()

	// Feed the request and close stdin so the child's Scan() returns immediately.
	_, _ = proc.stdin.Write(append(line, '\n'))
	_ = proc.stdin.Close()

	replies := make(chan workerResponse, 1)
	go func() {
		// Mirrors service/foreground_windows.go: a reader goroutine on a pipe
		// owned by a child process is wrapped in recover, always.
		defer func() { _ = recover() }()
		sc := bufio.NewScanner(proc.stdout)
		sc.Buffer(make([]byte, 0, 4096), workerLineLimit)
		var resp workerResponse
		if sc.Scan() {
			if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
				resp = workerResponse{Outcome: wOutcomeFailed, HResult: ErrWorkerIO}
			}
		} else {
			resp = workerResponse{Outcome: wOutcomeFailed, HResult: ErrWorkerIO}
		}
		replies <- resp
	}()

	timeout := time.NewTimer(appxWorkerTimeout)
	defer timeout.Stop()

	select {
	case r := <-replies:
		return r, proc.wait(5 * time.Second)
	case <-timeout.C:
		h.log.Warn("appx worker timed out, terminating", "family", req.Family, "op", req.Op)
		proc.kill()
		return workerResponse{Outcome: wOutcomeTimeout}, exitTimeout
	case <-ctx.Done():
		h.log.Warn("appx worker cancelled, terminating", "family", req.Family, "op", req.Op)
		proc.kill()
		return workerResponse{Outcome: wOutcomeTimeout}, exitTimeout
	}
}

// hresultPattern is the only shape allowed to leave the machine in ErrorCode.
var hresultPattern = regexp.MustCompile(`^0[xX][0-9A-Fa-f]{8}$`)

// sanitizeHResult keeps the privacy guarantee on the agent side of the boundary.
// The worker's stdout line may be up to 64 KiB, so a malformed child could
// otherwise put arbitrary text on the wire. The server drops anything over 32
// chars, but relying on the receiver to scrub what the sender should never have
// transmitted is the wrong side of that boundary.
func sanitizeHResult(code, fallback string) string {
	if hresultPattern.MatchString(strings.TrimSpace(code)) {
		return strings.TrimSpace(code)
	}
	return fallback
}

// mapWorkerResponse folds the worker's answer and exit code into the model's
// outcome vocabulary.
func mapWorkerResponse(resp workerResponse, exitCode uint32) Result {
	if exitCode == exitRejected {
		// The child's own allowlist check refused the family. This means the
		// parent and the child disagree, which is a bug — surfaced as "blocked"
		// so it can never look like a successful removal.
		return Result{Outcome: OutcomeBlocked, ErrorCode: sanitizeHResult(resp.HResult, ErrWorkerRefused)}
	}
	switch resp.Outcome {
	case wOutcomeOK:
		return Result{Outcome: OutcomeOK}
	case wOutcomeAbsent:
		// Deliberately code-free: "not present" is the whole story, and an error
		// code beside a non-error outcome reads as a failure in the panel.
		return Result{Outcome: OutcomeNotPresent}
	case wOutcomeProtected:
		return Result{Outcome: OutcomeBlocked, ErrorCode: sanitizeHResult(resp.HResult, ErrWorkerRefused)}
	case wOutcomeTimeout:
		return Result{Outcome: OutcomeTimeout, ErrorCode: sanitizeHResult(resp.HResult, ErrWorkerIO)}
	default:
		return Result{Outcome: OutcomeFailed, ErrorCode: sanitizeHResult(resp.HResult, ErrWorkerIO)}
	}
}

// childProc holds the handles of one running worker.
type childProc struct {
	handle windows.Handle
	stdin  *os.File
	stdout *os.File
	closed bool
}

// spawn creates the worker with inherited stdin/stdout pipes.
func (h *appxHost) spawn() (*childProc, error) {
	sa := &windows.SecurityAttributes{InheritHandle: 1}
	sa.Length = uint32(unsafe.Sizeof(*sa))

	// stdin: parent writes, child reads.
	var inRead, inWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, sa, 0); err != nil {
		return nil, err
	}
	// stdout: child writes, parent reads.
	var outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&outRead, &outWrite, sa, 0); err != nil {
		_ = windows.CloseHandle(inRead)
		_ = windows.CloseHandle(inWrite)
		return nil, err
	}
	// The parent's ends must not leak into the child.
	_ = windows.SetHandleInformation(inWrite, windows.HANDLE_FLAG_INHERIT, 0)
	_ = windows.SetHandleInformation(outRead, windows.HANDLE_FLAG_INHERIT, 0)

	si := &windows.StartupInfo{}
	si.Cb = uint32(unsafe.Sizeof(*si))
	si.Flags = windows.STARTF_USESTDHANDLES
	si.StdInput = inRead
	si.StdOutput = outWrite
	// stderr is left unconnected on purpose: the worker's diagnostics must never
	// be interleaved into the stdout protocol line, and a service has no console.
	si.StdErr = 0

	appName := windows.StringToUTF16Ptr(h.exe)
	// Fixed literal argv. Nothing from the policy, the catalog or the network is
	// ever interpolated into a command line.
	cmdline := windows.StringToUTF16Ptr(`"` + h.exe + `" ` + appxWorkerArg)

	var pi windows.ProcessInformation
	err := windows.CreateProcess(appName, cmdline, nil, nil, true,
		windows.CREATE_NO_WINDOW, nil, nil, si, &pi)

	// The child owns its ends now, in both the success and failure cases.
	_ = windows.CloseHandle(inRead)
	_ = windows.CloseHandle(outWrite)
	if err != nil {
		_ = windows.CloseHandle(inWrite)
		_ = windows.CloseHandle(outRead)
		return nil, err
	}
	_ = windows.CloseHandle(pi.Thread)

	return &childProc{
		handle: pi.Process,
		stdin:  os.NewFile(uintptr(inWrite), "tolikopa-appx-in"),
		stdout: os.NewFile(uintptr(outRead), "tolikopa-appx-out"),
	}, nil
}

// wait blocks for the child to exit and returns its exit code.
func (c *childProc) wait(d time.Duration) uint32 {
	ev, err := windows.WaitForSingleObject(c.handle, uint32(d/time.Millisecond))
	if err != nil || ev != windows.WAIT_OBJECT_0 {
		c.kill()
		return exitTerminated
	}
	var code uint32
	if err := windows.GetExitCodeProcess(c.handle, &code); err != nil {
		return exitFailed
	}
	return code
}

// kill terminates a worker that did not answer in time.
func (c *childProc) kill() {
	_ = windows.TerminateProcess(c.handle, exitTerminated)
}

// close releases everything this round-trip owned.
func (c *childProc) close() {
	if c.closed {
		return
	}
	c.closed = true
	_ = c.stdin.Close()  // no-op when already closed after the write
	_ = c.stdout.Close() // closes outRead
	_ = windows.CloseHandle(c.handle)
}
