//go:build windows

package collector

import (
	"log/slog"
	"regexp"
	"runtime"
	"strconv"
	"time"

	"github.com/yusufpapurcu/wmi"

	"github.com/aliyalinkilic/tolikopa/agent/internal/config"
	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// wmiQueryTimeout bounds a single Collect() so a wedged WMI provider cannot
// stall the agent.
const wmiQueryTimeout = 25 * time.Second

// msftPhysicalDisk mirrors the queried columns of root\Microsoft\Windows\Storage
// MSFT_PhysicalDisk. HealthStatus: 0=Healthy, 1=Warning, 2=Unhealthy.
type msftPhysicalDisk struct {
	DeviceId     string
	FriendlyName string
	HealthStatus uint16
}

// msStorageFailurePredict mirrors root\wmi MSStorageDriver_FailurePredictStatus.
type msStorageFailurePredict struct {
	InstanceName   string
	PredictFailure bool
}

type winHealth struct {
	log *slog.Logger
}

// NewHealthCollector returns the Windows WMI-backed SMART health collector.
func NewHealthCollector(_ *config.Config, log *slog.Logger) HealthCollector {
	return &winHealth{log: log}
}

// Collect runs both WMI queries on a dedicated OS-locked thread, bounded by a
// timeout. It is intended to be called on a low cadence (30-60 min).
func (h *winHealth) Collect() []model.DiskHealth {
	resCh := make(chan []model.DiskHealth, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		resCh <- h.query()
	}()
	select {
	case r := <-resCh:
		return r
	case <-time.After(wmiQueryTimeout):
		h.log.Warn("wmi SMART query timed out")
		return nil
	}
}

var trailingInt = regexp.MustCompile(`(\d+)\D*$`)

func (h *winHealth) query() []model.DiskHealth {
	var disks []msftPhysicalDisk
	if err := wmi.QueryNamespace(
		"SELECT DeviceId, FriendlyName, HealthStatus FROM MSFT_PhysicalDisk",
		&disks, `root\Microsoft\Windows\Storage`); err != nil {
		h.log.Warn("MSFT_PhysicalDisk query failed", "err", err.Error())
		return nil
	}

	out := make([]model.DiskHealth, 0, len(disks))
	idx := make(map[string]int, len(disks)) // DeviceId -> position in out
	for _, d := range disks {
		name := d.FriendlyName
		if name == "" {
			name = "PhysicalDisk" + d.DeviceId
		}
		idx[d.DeviceId] = len(out)
		out = append(out, model.DiskHealth{
			Disk:    name,
			SMARTOk: d.HealthStatus == 0,
		})
	}

	// Secondary (best-effort): overlay predictive-failure flags.
	var preds []msStorageFailurePredict
	if err := wmi.QueryNamespace(
		"SELECT InstanceName, PredictFailure FROM MSStorageDriver_FailurePredictStatus",
		&preds, `root\wmi`); err != nil {
		h.log.Debug("MSStorageDriver_FailurePredictStatus unavailable", "err", err.Error())
		return out
	}
	for i, p := range preds {
		if !p.PredictFailure {
			continue
		}
		pos := -1
		if m := trailingInt.FindStringSubmatch(p.InstanceName); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				if j, ok := idx[strconv.Itoa(n)]; ok {
					pos = j
				}
			}
		}
		if pos < 0 && i < len(out) { // fall back to ordinal alignment
			pos = i
		}
		if pos >= 0 && pos < len(out) {
			out[pos].PredictFail = true
		}
	}
	return out
}
