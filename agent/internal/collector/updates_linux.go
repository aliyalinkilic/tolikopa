//go:build linux

package collector

import (
	"log/slog"

	"github.com/aliyalinkilic/tolikopa/agent/internal/config"
	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// linuxUpdates is a no-op: Windows Update posture is meaningless on the Linux
// target (the server monitoring itself).
type linuxUpdates struct{}

// NewUpdateCollector returns the Linux stub update collector.
func NewUpdateCollector(_ *config.Config, _ *slog.Logger) UpdateCollector {
	return &linuxUpdates{}
}

// Collect returns nil on Linux (no Windows Update data).
func (linuxUpdates) Collect() *model.UpdateStatus { return nil }
