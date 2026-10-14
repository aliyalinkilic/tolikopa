//go:build !windows

package actuator

import (
	"context"
	"log/slog"

	"github.com/aliyalinkilic/tolikopa/agent/internal/collector"
)

// otherBackend is a no-op: Appx packages and the Run/StartupApproved registry
// model are Windows-only. The non-Windows collectors report no presence at all,
// so Reconcile never reaches these methods; they exist so the package builds and
// fail closed if it ever did.
type otherBackend struct{}

func newBackend(_ *slog.Logger, _ string) Backend { return otherBackend{} }

// ResolveApps reports a successful read of a machine with no Appx packages:
// off Windows that is an answer, not a failure.
func (otherBackend) ResolveApps(_ context.Context, _ []string) (map[string][]string, bool) {
	return map[string][]string{}, true
}

func (otherBackend) RemoveApp(_ context.Context, _ string, _ []string) Result {
	return Result{Outcome: OutcomeNotPresent}
}

func (otherBackend) SetStartup(_ context.Context, _ []collector.StartupTarget, _ bool) Result {
	return Result{Outcome: OutcomeNotPresent}
}

// RunAppxWorker is the non-Windows stub of the removal worker: there is nothing
// it could ever remove, so it refuses immediately.
func RunAppxWorker() int {
	writeWorkerResponse(workerResponse{Outcome: wOutcomeFailed, HResult: ErrWorkerRefused})
	return exitRejected
}
