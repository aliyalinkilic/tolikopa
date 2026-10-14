//go:build linux

package collector

import (
	"log/slog"

	"github.com/aliyalinkilic/tolikopa/agent/internal/config"
	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// linuxHealth is a best-effort no-op: gopsutil exposes no SMART data and the
// Linux target is the server monitoring itself.
type linuxHealth struct{}

// NewHealthCollector returns the Linux stub SMART collector.
func NewHealthCollector(_ *config.Config, _ *slog.Logger) HealthCollector {
	return &linuxHealth{}
}

// Collect returns no disk-health rows on Linux.
func (linuxHealth) Collect() []model.DiskHealth { return nil }
