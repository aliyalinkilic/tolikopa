// Package collector gathers the allowlisted system metrics and assembles
// model.Sample / model.Machine values.
//
// PRIVACY ALLOWLIST (enforced, see docs/AGENT.md): on gopsutil process objects
// this package calls ONLY .Name(), .Times(), .MemoryInfo(), .CreateTime().
// It never calls .Cmdline(), .Exe(), .Environ(), .OpenFiles(), .Connections()
// or .Cwd(), and never reads window titles, URLs, keystrokes, screen content or
// file paths. There is no code path — and no model field — for such data.
package collector

import (
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	psnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"

	"github.com/aliyalinkilic/tolikopa/agent/internal/config"
	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// Session is the resolved console-session state at collection time.
type Session struct {
	User  string // "DOMAIN\\user" of the console session, or "" when none
	State string // active | locked | disconnected | none
}

// HealthCollector reads disk SMART health (Windows: WMI; Linux: stub).
type HealthCollector interface {
	Collect() []model.DiskHealth
}

// EventCollector reads app-crash/hang events (Windows: wevtapi; Linux: stub).
// It returns only events observed since the previous call.
type EventCollector interface {
	Collect() []model.Event
}

// UpdateCollector reads the machine's Windows Update posture (Windows: WUA COM +
// registry; Linux: stub). It returns nil when unavailable and is intended for a
// low cadence. It reads COUNTS and TIMESTAMPS only — never update titles, KB
// numbers, or history — so it stays within the privacy allowlist.
type UpdateCollector interface {
	Collect() *model.UpdateStatus
}

// BatteryCollector reads power/battery state (Windows: GetSystemPowerStatus + WMI
// capacities; Linux: stub). Returns nil when unavailable; low cadence.
type BatteryCollector interface {
	Collect() *model.Battery
}

// AppInventoryCollector reports the presence of ALLOWLISTED catalog apps only
// (Windows: the AppxAllUserStore registry view; elsewhere: stub).
//
// PRIVACY: this is NOT a software inventory. Only packages named in the compiled
// internal/appcatalog can ever appear, and only as (item_id, present, scope) —
// no publisher, no version, no install path, no SID, no user name.
type AppInventoryCollector interface {
	Collect() []model.RemediationItem
}

// StartupCollector reports the presence and enabled-state of ALLOWLISTED catalog
// logon entries only (Windows: the Run/RunOnce registry value NAMES plus their
// StartupApproved state; elsewhere: stub).
//
// PRIVACY: only the value NAME is ever read and matched. The value DATA is a file
// path — a forbidden data class — and is never read, logged or transmitted.
type StartupCollector interface {
	Collect() []model.RemediationItem
}

// NOTE: apps have no AppTarget counterpart to StartupTarget. There used to be
// one, carrying the PackageFullNames seen in the AppxAllUserStore registry view.
// That view is a deployment JOURNAL — one subkey per version ever deployed, kept
// after removal, and frequently missing the version actually registered — so
// every name handed to the removal worker answered 0x80070490 (ERROR_NOT_FOUND).
// The removal path resolves the family against the deployment API instead; see
// actuator/appx_resolve_windows.go. Do not reintroduce a registry-derived
// removal target here.

// StartupTarget is the collector-internal actuation detail for one catalog logon
// entry: exactly which registry value, in which hive, and which StartupApproved
// subkey governs it.
//
// PRIVACY: UserSID is used only to address the local registry hive when
// disabling/enabling the entry. It is never emitted, logged or transmitted.
type StartupTarget struct {
	ItemID    string
	ValueName string // the exact Run value name observed (never its data)
	UserSID   string // "" = HKLM, otherwise the loaded HKU\<SID> hive
	Approved  string // StartupApproved subkey: "Run" | "Run32" | "" (RunOnce: none)
	Enabled   bool
}

// StartupTargeter is implemented by startup collectors that can hand the actuator
// the exact registry values observed during the last Collect.
type StartupTargeter interface {
	Targets() []StartupTarget
}

// procCPU is the last-seen cumulative busy time for one PID, used to derive cpu%.
type procCPU struct {
	total float64
	at    time.Time
}

// Collector holds sampling state (per-PID CPU baselines) between ticks.
type Collector struct {
	cfg      *config.Config
	log      *slog.Logger
	lastProc map[int32]procCPU
}

// New returns a Collector. Call Prime once before the first full sample so that
// per-process and total CPU percentages have a baseline.
func New(cfg *config.Config, log *slog.Logger) *Collector {
	return &Collector{
		cfg:      cfg,
		log:      log,
		lastProc: make(map[int32]procCPU),
	}
}

// Prime warms the total-CPU sampler and seeds per-process CPU baselines so the
// first reported sample is meaningful rather than zero.
func (c *Collector) Prime() {
	_, _ = cpu.Percent(0, false) // establishes the total-CPU baseline
	c.snapshotProcCPU()
}

// Vitals builds a light sample: total CPU%, memory, cumulative NIC counters and
// the current session. Disks and processes are attached separately by Full.
func (c *Collector) Vitals(sess Session) model.Sample {
	s := model.Sample{
		TS:           time.Now().UTC(),
		LogonUser:    sess.User,
		SessionState: sess.State,
	}
	if pcts, err := cpu.Percent(0, false); err == nil && len(pcts) > 0 {
		s.CPUPct = clampPct(pcts[0])
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		s.MemUsed = int64(vm.Used)
		s.MemTotal = int64(vm.Total)
	}
	sent, recv := cumulativeNet()
	s.NetSentBytes = sent
	s.NetRecvBytes = recv
	return s
}

// Full augments a vitals sample with fixed-disk usage and the top-N processes.
func (c *Collector) Full(s *model.Sample) {
	s.Disks = collectDisks()
	s.Processes = c.topProcesses()
}

// cumulativeNet sums per-interface cumulative byte counters for PHYSICAL adapters
// only. Virtual switch / VM / WSL / container adapters are skipped so they don't
// inflate usage, and tunnel/VPN adapters are skipped so their traffic isn't
// DOUBLE-COUNTED with the physical NIC it also traverses.
func cumulativeNet() (sent, recv int64) {
	counters, err := psnet.IOCounters(true)
	if err != nil {
		return 0, 0
	}
	for _, ic := range counters {
		if skipNIC(ic.Name) {
			continue
		}
		sent += int64(ic.BytesSent)
		recv += int64(ic.BytesRecv)
	}
	return sent, recv
}

// skipNIC reports whether an interface is loopback or a virtual/VM/tunnel/VPN
// adapter that should be excluded from real-traffic accounting (matched by
// well-known name fragments on Windows and Linux).
func skipNIC(name string) bool {
	n := strings.ToLower(name)
	if n == "lo" || strings.HasPrefix(n, "lo0") {
		return true
	}
	for _, frag := range []string{
		"loopback", "veth", "vmware", "virtualbox", "vbox", "hyper-v",
		"tap", "openvpn", "wireguard", "wintun", "tunnel", "isatap",
		"teredo", "6to4", "bluetooth", "wan miniport", "wsl", "docker",
		"npcap", "pptp", "l2tp", "vpn", "wi-fi direct", "virbr", "br-", "tailscale",
	} {
		if strings.Contains(n, frag) {
			return true
		}
	}
	return false
}

// collectDisks reports usage for fixed drives only.
func collectDisks() []model.Disk {
	parts, err := disk.Partitions(false) // physical partitions only
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []model.Disk
	for _, p := range parts {
		if !isFixedDisk(p.Mountpoint, p.Fstype, strings.Join(p.Opts, ",")) {
			continue
		}
		if seen[p.Mountpoint] {
			continue
		}
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil || usage.Total == 0 {
			continue
		}
		seen[p.Mountpoint] = true
		out = append(out, model.Disk{
			Mount: normalizeMount(p.Mountpoint),
			Used:  int64(usage.Used),
			Total: int64(usage.Total),
		})
	}
	return out
}

// topProcesses returns the union of the top-N processes by CPU% and by memory,
// aggregated per (normalized) process name. NAME ONLY — never command line/path.
func (c *Collector) topProcesses() []model.Process {
	procs, err := process.Processes()
	if err != nil {
		return nil
	}
	now := time.Now()
	next := make(map[int32]procCPU, len(procs))
	agg := make(map[string]*model.Process)

	for _, p := range procs {
		name, err := p.Name() // ALLOWLISTED
		if err != nil || name == "" {
			continue
		}
		name = normalizeProcName(name)

		var cpuPct float64
		if times, err := p.Times(); err == nil { // ALLOWLISTED
			total := times.User + times.System
			next[p.Pid] = procCPU{total: total, at: now}
			if prev, ok := c.lastProc[p.Pid]; ok {
				if el := now.Sub(prev.at).Seconds(); el > 0 {
					cpuPct = clampPct((total - prev.total) / el * 100)
				}
			}
		}

		var memBytes int64
		if mi, err := p.MemoryInfo(); err == nil && mi != nil { // ALLOWLISTED
			memBytes = int64(mi.RSS)
		}

		e := agg[name]
		if e == nil {
			e = &model.Process{Name: name}
			agg[name] = e
		}
		e.CPUPct += cpuPct
		e.MemBytes += memBytes
	}
	c.lastProc = next // prune dead PIDs

	list := make([]model.Process, 0, len(agg))
	for _, e := range agg {
		list = append(list, *e)
	}
	return topUnion(list, c.cfg.TopN)
}

// snapshotProcCPU seeds the per-PID CPU baseline without emitting a sample.
func (c *Collector) snapshotProcCPU() {
	procs, err := process.Processes()
	if err != nil {
		return
	}
	now := time.Now()
	next := make(map[int32]procCPU, len(procs))
	for _, p := range procs {
		if times, err := p.Times(); err == nil { // ALLOWLISTED
			next[p.Pid] = procCPU{total: times.User + times.System, at: now}
		}
	}
	c.lastProc = next
}

// topUnion returns the union of the top-n entries by CPU and the top-n by memory.
func topUnion(list []model.Process, n int) []model.Process {
	if n <= 0 {
		n = 10
	}
	byCPU := make([]model.Process, len(list))
	copy(byCPU, list)
	sort.Slice(byCPU, func(i, j int) bool { return byCPU[i].CPUPct > byCPU[j].CPUPct })

	byMem := make([]model.Process, len(list))
	copy(byMem, list)
	sort.Slice(byMem, func(i, j int) bool { return byMem[i].MemBytes > byMem[j].MemBytes })

	picked := make(map[string]bool)
	out := make([]model.Process, 0, 2*n)
	take := func(src []model.Process) {
		c := 0
		for _, p := range src {
			if c >= n {
				break
			}
			if !picked[p.Name] {
				picked[p.Name] = true
				out = append(out, p)
			}
			c++
		}
	}
	take(byCPU)
	take(byMem)
	return out
}

// MachineInfo assembles stable machine identity. OS/build/domain/kind come from
// the platform-specific platformMachine; hostname and boot time from gopsutil.
func MachineInfo(agentID, version string) model.Machine {
	m := model.Machine{
		MachineID:    agentID,
		AgentVersion: version,
	}
	if info, err := host.Info(); err == nil {
		m.Hostname = info.Hostname
		m.BootTime = time.Unix(int64(info.BootTime), 0).UTC()
	}
	osName, osBuild, domain, kind := platformMachine()
	if osName != "" {
		m.OS = osName
	}
	m.OSBuild = osBuild
	m.Domain = domain
	m.Kind = kind
	// Static hardware identity (collected once at startup). Model name only —
	// no serial numbers or other identifying hardware IDs.
	if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
		m.CPUModel = strings.TrimSpace(infos[0].ModelName)
	}
	if n, err := cpu.Counts(true); err == nil {
		m.CPUCores = n
	}
	return m
}

func clampPct(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// normalizeProcName lowercases and strips a trailing ".exe".
func normalizeProcName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimSuffix(name, ".exe")
	return name
}

// MaxAppNameLen mirrors the server's app_focus name cap (bytes, after
// normalisation). A longer name would be silently dropped server-side.
const MaxAppNameLen = 128

// ValidAppName normalises an application base name exactly like process names
// (lowercase, no ".exe") and reports whether the result is acceptable as an
// app_focus key. It rejects: empty names, names over MaxAppNameLen bytes,
// invalid UTF-8, anything containing a path separator (a path is never an app
// name, and a path is forbidden data), control and format characters (bidi
// overrides would spoof the panel), and the "__" prefix reserved for helper
// control fields. The in-session helper runs as the (untrusted) user, so every
// name it relays passes through here before it reaches the sender.
func ValidAppName(name string) (string, bool) {
	// Check UTF-8 on the raw input: strings.ToLower would silently turn invalid
	// bytes into U+FFFD.
	if !utf8.ValidString(name) {
		return "", false
	}
	n := normalizeProcName(name)
	if n == "" || len(n) > MaxAppNameLen || strings.HasPrefix(n, "__") {
		return "", false
	}
	for _, r := range n {
		if r == '/' || r == '\\' || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", false
		}
	}
	return n, true
}
