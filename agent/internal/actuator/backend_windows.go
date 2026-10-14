//go:build windows

package actuator

import (
	"context"
	"log/slog"

	"github.com/aliyalinkilic/tolikopa/agent/internal/collector"
)

// winBackend wires the two Windows actuation paths: the out-of-process Appx
// removal host and the in-process (pure registry) startup control.
type winBackend struct {
	log  *slog.Logger
	appx *appxHost
}

func newBackend(log *slog.Logger, exePath string) Backend {
	return &winBackend{log: log, appx: newAppxHost(log, exePath)}
}

func (b *winBackend) ResolveApps(ctx context.Context, families []string) (map[string][]string, bool) {
	live, res := b.appx.resolveFamilies(ctx, families)
	return live, res.Outcome == OutcomeOK
}

func (b *winBackend) RemoveApp(ctx context.Context, family string, fullNames []string) Result {
	return b.appx.removeAndDeprovision(ctx, family, fullNames)
}

func (b *winBackend) SetStartup(ctx context.Context, targets []collector.StartupTarget, enabled bool) Result {
	return setStartupApproved(ctx, b.log, targets, enabled)
}
