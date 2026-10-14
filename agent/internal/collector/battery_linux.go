//go:build linux

package collector

import (
	"log/slog"

	"github.com/aliyalinkilic/tolikopa/agent/internal/config"
	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// linuxBattery is a no-op: the Linux target is the server (no battery).
type linuxBattery struct{}

// NewBatteryCollector returns the Linux stub battery collector.
func NewBatteryCollector(_ *config.Config, _ *slog.Logger) BatteryCollector {
	return &linuxBattery{}
}

// Collect returns nil on Linux (no battery telemetry).
func (linuxBattery) Collect() *model.Battery { return nil }
