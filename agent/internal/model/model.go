// Package model defines the wire contract the agent sends to the server.
//
// PRIVACY ALLOWLIST: these structs are the *only* shape of data that ever leaves a
// machine. There is deliberately no field for window titles, URLs, command lines,
// file paths, or screen content. Do not add one. See docs/AGENT.md.
//
// This file is duplicated (identical JSON tags) in server/internal/model/ingest.go.
// The two modules do not import each other; keep them in sync by hand.
package model

import "time"

// Envelope is one machine's batch, POSTed to /api/v1/ingest as gzip JSON.
type Envelope struct {
	BatchID      string             `json:"batch_id"`
	Machine      Machine            `json:"machine"`
	Samples      []Sample           `json:"samples"`
	Events       []Event            `json:"events,omitempty"`
	DiskHealth   []DiskHealth       `json:"disk_health,omitempty"`
	UpdateStatus *UpdateStatus      `json:"update_status,omitempty"`
	AppFocus     []AppFocus         `json:"app_focus,omitempty"`
	Battery      *Battery           `json:"battery,omitempty"`
	Remediation  *RemediationReport `json:"remediation,omitempty"`
	SentAt       time.Time          `json:"sent_at"`
}

// Machine is stable identity + boot time (boot_time drives net counter-reset detection).
type Machine struct {
	MachineID    string    `json:"machine_id"`
	Hostname     string    `json:"hostname"`
	OS           string    `json:"os,omitempty"`
	OSBuild      string    `json:"os_build,omitempty"`
	AgentVersion string    `json:"agent_version,omitempty"`
	Domain       string    `json:"domain,omitempty"`
	Kind         string    `json:"kind,omitempty"` // windows | linux
	CPUModel     string    `json:"cpu_model,omitempty"`
	CPUCores     int       `json:"cpu_cores,omitempty"`
	// IPAddrs are the machine's OWN IPv4 addresses on up, non-loopback interfaces
	// (LAN identity like hostname — never per-process connections or peers).
	// Refreshed on every batch because DHCP / Wi-Fi↔Ethernet changes them.
	IPAddrs  []string  `json:"ip_addrs,omitempty"`
	BootTime time.Time `json:"boot_time"`
}

// Sample is a point-in-time snapshot of a machine's vitals.
type Sample struct {
	TS           time.Time `json:"ts"`
	CPUPct       float64   `json:"cpu_pct"`
	MemUsed      int64     `json:"mem_used"`
	MemTotal     int64     `json:"mem_total"`
	NetSentBytes int64     `json:"net_sent_bytes"` // cumulative NIC counter (server computes deltas)
	NetRecvBytes int64     `json:"net_recv_bytes"`
	LogonUser    string    `json:"logon_user"`     // domain\user of console session, or ""
	SessionState string    `json:"session_state"`  // active | idle | locked | disconnected | none
	Disks        []Disk    `json:"disks,omitempty"`
	Processes    []Process `json:"processes,omitempty"`
}

// Disk is per-mount usage (fixed drives only).
type Disk struct {
	Mount string `json:"mount"`
	Used  int64  `json:"used"`
	Total int64  `json:"total"`
}

// Process is a top-N resource consumer. NAME ONLY — never command line / path.
type Process struct {
	Name     string  `json:"name"`     // normalized basename, lowercase, no ".exe"
	CPUPct   float64 `json:"cpu_pct"`
	MemBytes int64   `json:"mem_bytes"`
}

// Event is a discrete occurrence (app crash/hang, hardware warning).
type Event struct {
	TS          time.Time         `json:"ts"`
	Kind        string            `json:"kind"` // app_crash | app_hang | hw_warning | smart_fail
	ProcessName string            `json:"process_name,omitempty"`
	Severity    string            `json:"severity,omitempty"`
	Detail      map[string]string `json:"detail,omitempty"` // e.g. {"exception":"0xc0000005"} — never paths/messages
}

// DiskHealth is the current SMART state of one physical disk.
type DiskHealth struct {
	Disk        string `json:"disk"`
	SMARTOk     bool   `json:"smart_ok"`
	PredictFail bool   `json:"predict_fail"`
}

// AppFocus is per-application foreground (active-use) time, produced by the
// in-session helper for the "time in apps" view.
//
// PRIVACY: NAME ONLY. This is the normalized process base name of whatever window
// was in the foreground (e.g. "chrome", "excel", "vlc") — NEVER the window title,
// document name, browser tab, URL, or page/video being viewed. Seconds accrue only
// while the interactive user is actually active (not idle/locked).
type AppFocus struct {
	Name    string `json:"name"`
	Seconds int    `json:"seconds"`
}

// Battery is the machine's power/battery state, gathered on a low cadence.
// Present is false on desktops / VMs (no battery). Percent/HealthPct/CycleCount
// are -1 when unknown. HealthPct = FullChargeCapacity/DesignCapacity*100 (battery
// wear). This is plain hardware telemetry — no privacy concerns.
type Battery struct {
	Present            bool      `json:"present"`
	Percent            int       `json:"percent"`              // 0-100 current charge; -1 unknown
	ACConnected        bool      `json:"ac_connected"`         // plugged into mains
	Charging           bool      `json:"charging"`             // actively charging
	DesignCapacity     int       `json:"design_capacity"`      // mWh design capacity (0 unknown)
	FullChargeCapacity int       `json:"full_charge_capacity"` // mWh current full-charge capacity (0 unknown)
	HealthPct          int       `json:"health_pct"`           // 0-100 (full/design); -1 unknown
	CycleCount         int       `json:"cycle_count"`          // charge cycles; -1 unknown/unavailable
	CheckedAt          time.Time `json:"checked_at"`
}

// ── Endpoint hygiene (bloatware removal / startup control) ────────────────────
//
// PRIVACY: RemediationReport carries presence ONLY for items in the agent's
// COMPILED catalog (internal/appcatalog). It is deliberately NOT a general
// installed-software inventory — software the user installed themselves never
// appears here and never leaves the machine. See docs/AGENT.md.
//
// AUTHORITY: the compiled catalog is the closed vocabulary. The server may only
// SELECT FROM it; any item_id the agent does not recognise is refused locally and
// reported back as outcome "unknown_item". This is desired-state reconciliation,
// not a command channel — see CLAUDE.md §Safety.

// RemediationReport is the agent's hygiene posture, sent on a low cadence.
type RemediationReport struct {
	// CatalogVersion is the hash of the compiled vocabulary (kind:item_id pairs
	// only, so Turkish label edits do not force an agent rebuild).
	CatalogVersion string `json:"catalog_version"`
	// PolicyRevision is the revision the agent has actually applied. The server
	// omits the policy payload from its response when this already matches.
	PolicyRevision string               `json:"policy_revision"`
	Allowed        RemediationAllowed   `json:"allowed"`
	Present        []RemediationItem    `json:"present,omitempty"`
	Attempts       []RemediationAttempt `json:"attempts,omitempty"`
	CheckedAt      time.Time            `json:"checked_at"`
}

// RemediationAllowed mirrors the GPO-provisioned kill switches in config.json.
// The server cannot change these — they are the local veto. Enforce=false means
// the agent evaluates the policy and reports what it *would* do, changing nothing.
type RemediationAllowed struct {
	Apps    bool `json:"apps"`
	Startup bool `json:"startup"`
	Enforce bool `json:"enforce"`
}

// RemediationItem is the presence of one ALLOWLISTED catalog item.
type RemediationItem struct {
	Kind    string `json:"kind"`    // app | startup
	ItemID  string `json:"item_id"` // catalog key
	Present bool   `json:"present"`
	// Scope (apps): user | provisioned | both — where the package still exists.
	Scope string `json:"scope,omitempty"`
	// Enabled (startup): whether the entry currently runs at logon. nil = n/a.
	Enabled *bool `json:"enabled,omitempty"`
}

// RemediationAttempt is one audited action. ErrorCode is a CODE only
// ("0x80073cfa") — never an OS message or a path, both of which would leak
// filesystem detail and violate the privacy allowlist.
type RemediationAttempt struct {
	TS        time.Time `json:"ts"`
	Kind      string    `json:"kind"`   // app | startup
	ItemID    string    `json:"item_id"`
	Action    string    `json:"action"` // remove | deprovision | disable | enable
	// Outcome: ok | failed | dry_run | not_present | not_allowed | blocked |
	//          timeout | unknown_item
	Outcome    string `json:"outcome"`
	ErrorCode  string `json:"error_code,omitempty"`
	DurationMs int    `json:"duration_ms,omitempty"`
}

// ── Server → agent (the 202 ingest response) ──────────────────────────────────

// IngestResponse is the body the server returns from POST /api/v1/ingest. The
// agent decodes it ONLY on the live send path — a spooled replay must never act
// on a directive that may be hours stale (see sender.post / spool.drainSpool).
type IngestResponse struct {
	Accepted int `json:"accepted"`
	// Policy is present only when the server's revision differs from the one the
	// agent reported, keeping the steady-state response tiny.
	Policy *RemediationPolicy `json:"policy,omitempty"`
}

// RemediationPolicy is the desired state for this machine, already resolved
// server-side against fleet-wide selection minus tag exclusions.
type RemediationPolicy struct {
	Revision string `json:"revision"`
	// Enabled=false is a full stop: the agent reconciles nothing.
	Enabled bool `json:"enabled"`
	// DryRun=true means evaluate and report, never actuate. Independent of the
	// local Enforce kill switch — either one being restrictive wins.
	DryRun bool `json:"dry_run"`
	// Apps/Startup are catalog item_ids. Anything unrecognised is refused locally.
	Apps    []string `json:"apps,omitempty"`
	Startup []string `json:"startup,omitempty"`
}

// UpdateStatus is the machine's read-only Windows Update posture, gathered on a
// low cadence. It carries COUNTS and TIMESTAMPS only — never update titles, KB
// numbers, or install history. Non-Windows agents never send it. Counts are -1
// when the agent could not determine them.
type UpdateStatus struct {
	PendingCount  int       `json:"pending_count"`  // updates Windows knows are not installed; -1 = unknown
	RebootPending bool      `json:"reboot_pending"` // a reboot is required to finish updates
	LastSearch    time.Time `json:"last_search"`    // last successful update scan (zero = unknown)
	LastInstall   time.Time `json:"last_install"`   // last successful update install (zero = unknown)
	CheckedAt     time.Time `json:"checked_at"`     // when the agent gathered this snapshot
}
