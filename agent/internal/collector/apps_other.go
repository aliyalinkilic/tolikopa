//go:build !windows

package collector

import (
	"log/slog"

	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// otherApps is a no-op: Appx packages only exist on Windows. The Linux agent
// (which watches the server itself) reports no hygiene inventory.
type otherApps struct{}

// NewAppInventoryCollector returns the non-Windows stub app-inventory collector.
func NewAppInventoryCollector(_ *slog.Logger) AppInventoryCollector { return otherApps{} }

// Collect returns nil off Windows.
func (otherApps) Collect() []model.RemediationItem { return nil }
