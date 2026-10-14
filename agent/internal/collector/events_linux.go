//go:build linux

package collector

import (
	"log/slog"

	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// linuxEvents is a no-op: crash-event collection is Windows-only (docs/AGENT.md).
type linuxEvents struct{}

// NewEventCollector returns the Linux stub event collector.
func NewEventCollector(_ *slog.Logger) EventCollector { return &linuxEvents{} }

// Collect returns no events on Linux.
func (linuxEvents) Collect() []model.Event { return nil }
