//go:build !windows

package collector

import (
	"log/slog"

	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// otherStartup is a no-op: the Run/StartupApproved registry model is
// Windows-only. The Linux agent reports no startup inventory.
type otherStartup struct{}

// NewStartupCollector returns the non-Windows stub startup collector.
func NewStartupCollector(_ *slog.Logger) StartupCollector { return otherStartup{} }

// Collect returns nil off Windows.
func (otherStartup) Collect() []model.RemediationItem { return nil }

// Targets returns nil off Windows.
func (otherStartup) Targets() []StartupTarget { return nil }
