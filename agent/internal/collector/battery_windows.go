//go:build windows

package collector

import (
	"log/slog"
	"runtime"
	"time"
	"unsafe"

	"github.com/yusufpapurcu/wmi"

	"github.com/aliyalinkilic/tolikopa/agent/internal/config"
	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// procGetSystemPowerStatus lives in kernel32 (modKernel32 declared in session_windows.go).
var procGetSystemPowerStatus = modKernel32.NewProc("GetSystemPowerStatus")

// systemPowerStatus mirrors SYSTEM_POWER_STATUS.
type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

// root\wmi battery classes (mWh capacities + cycle count).
type batteryStaticData struct {
	InstanceName     string
	DesignedCapacity uint32
}
type batteryFullChargedCapacity struct {
	InstanceName        string
	FullChargedCapacity uint32
}
type batteryCycleCount struct {
	InstanceName string
	CycleCount   uint32
}

type winBattery struct {
	log *slog.Logger
}

// NewBatteryCollector returns the Windows battery collector.
func NewBatteryCollector(_ *config.Config, log *slog.Logger) BatteryCollector {
	return &winBattery{log: log}
}

// Collect reads charge/AC/charging via GetSystemPowerStatus (cheap) and, for
// laptops, battery wear via WMI design/full capacity. Never panics; degrades to
// unknown fields on any failure. Desktops/VMs report Present=false.
func (b *winBattery) Collect() *model.Battery {
	bat := &model.Battery{Percent: -1, HealthPct: -1, CycleCount: -1, CheckedAt: time.Now().UTC()}

	var sps systemPowerStatus
	if r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&sps))); r != 0 {
		// BatteryFlag: 128 = no system battery, 255 = unknown; anything else is a
		// state bitmask (bit 3 / value 8 = charging).
		bat.Present = sps.BatteryFlag != 128 && sps.BatteryFlag != 255
		if sps.BatteryLifePercent != 255 {
			bat.Percent = int(sps.BatteryLifePercent)
		}
		bat.ACConnected = sps.ACLineStatus == 1
		bat.Charging = bat.Present && sps.BatteryFlag&8 != 0
	}
	if !bat.Present {
		return bat // desktop / VM / no battery — skip WMI
	}

	design, full, cycles := b.wmiCapacities()
	if design > 0 && full > 0 {
		bat.DesignCapacity = design
		bat.FullChargeCapacity = full
		h := int(float64(full) / float64(design) * 100)
		if h > 100 {
			h = 100
		}
		bat.HealthPct = h
	}
	if cycles > 0 {
		bat.CycleCount = cycles
	}
	return bat
}

// wmiCapacities reads design/full capacity (mWh) and cycle count from root\wmi on
// a dedicated OS-locked thread, bounded by wmiQueryTimeout. Returns zeros when a
// value is unavailable (common — many batteries omit cycle count).
func (b *winBattery) wmiCapacities() (design, full, cycles int) {
	resCh := make(chan [3]int, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		var out [3]int
		var sd []batteryStaticData
		if err := wmi.QueryNamespace("SELECT InstanceName, DesignedCapacity FROM BatteryStaticData", &sd, `root\wmi`); err == nil && len(sd) > 0 {
			out[0] = int(sd[0].DesignedCapacity)
		} else if err != nil {
			b.log.Debug("BatteryStaticData query failed", "err", err.Error())
		}
		var fc []batteryFullChargedCapacity
		if err := wmi.QueryNamespace("SELECT InstanceName, FullChargedCapacity FROM BatteryFullChargedCapacity", &fc, `root\wmi`); err == nil && len(fc) > 0 {
			out[1] = int(fc[0].FullChargedCapacity)
		}
		var cc []batteryCycleCount
		if err := wmi.QueryNamespace("SELECT InstanceName, CycleCount FROM BatteryCycleCount", &cc, `root\wmi`); err == nil && len(cc) > 0 {
			out[2] = int(cc[0].CycleCount)
		}
		resCh <- out
	}()
	select {
	case r := <-resCh:
		return r[0], r[1], r[2]
	case <-time.After(wmiQueryTimeout):
		b.log.Warn("battery wmi query timed out")
		return 0, 0, 0
	}
}
