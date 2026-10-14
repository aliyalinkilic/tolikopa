//go:build !windows

package service

import (
	"context"
	"log/slog"

	"github.com/aliyalinkilic/tolikopa/agent/internal/collector"
	"github.com/aliyalinkilic/tolikopa/agent/internal/sender"
)

// Foreground application / idle tracking is Windows-only; these are no-ops
// elsewhere so the shared runner and main entrypoint compile unchanged.
func runForegroundManager(_ context.Context, _ string, _ *sender.Sender, _ *collector.SessionTracker, _ *slog.Logger) {
}

// RunForegroundHelper is a no-op on non-Windows platforms.
func RunForegroundHelper() error { return nil }
