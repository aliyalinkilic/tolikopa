// Package model holds the ingest wire contract (mirror of agent/internal/model)
// and the dashboard API DTOs. The agent and server modules do not import each
// other; keep ingest.go in sync with agent/internal/model/model.go by hand.
package model

import "time"

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

// Battery mirrors agent/internal/model.Battery (charge + wear health).
type Battery struct {
	Present            bool      `json:"present"`
	Percent            int       `json:"percent"`
	ACConnected        bool      `json:"ac_connected"`
	Charging           bool      `json:"charging"`
	DesignCapacity     int       `json:"design_capacity"`
	FullChargeCapacity int       `json:"full_charge_capacity"`
	HealthPct          int       `json:"health_pct"`
	CycleCount         int       `json:"cycle_count"`
	CheckedAt          time.Time `json:"checked_at"`
}

// AppFocus mirrors agent/internal/model.AppFocus — per-app foreground seconds,
// NAME ONLY (no titles/URLs). Attributed to the batch's current logon user.
type AppFocus struct {
	Name    string `json:"name"`
	Seconds int    `json:"seconds"`
}

type Machine struct {
	MachineID    string    `json:"machine_id"`
	Hostname     string    `json:"hostname"`
	OS           string    `json:"os,omitempty"`
	OSBuild      string    `json:"os_build,omitempty"`
	AgentVersion string    `json:"agent_version,omitempty"`
	Domain       string    `json:"domain,omitempty"`
	Kind         string    `json:"kind,omitempty"`
	CPUModel     string    `json:"cpu_model,omitempty"`
	CPUCores     int       `json:"cpu_cores,omitempty"`
	// IPAddrs: the machine's own IPv4 addresses (agent-reported, validated at ingest).
	IPAddrs  []string  `json:"ip_addrs,omitempty"`
	BootTime time.Time `json:"boot_time"`
}

type Sample struct {
	TS           time.Time `json:"ts"`
	CPUPct       float64   `json:"cpu_pct"`
	MemUsed      int64     `json:"mem_used"`
	MemTotal     int64     `json:"mem_total"`
	NetSentBytes int64     `json:"net_sent_bytes"`
	NetRecvBytes int64     `json:"net_recv_bytes"`
	LogonUser    string    `json:"logon_user"`     // domain\user of console session, or ""
	SessionState string    `json:"session_state"`  // active | idle | locked | disconnected | none
	Disks        []Disk    `json:"disks,omitempty"`
	Processes    []Process `json:"processes,omitempty"`
}

type Disk struct {
	Mount string `json:"mount"`
	Used  int64  `json:"used"`
	Total int64  `json:"total"`
}

type Process struct {
	Name     string  `json:"name"`
	CPUPct   float64 `json:"cpu_pct"`
	MemBytes int64   `json:"mem_bytes"`
}

type Event struct {
	TS          time.Time         `json:"ts"`
	Kind        string            `json:"kind"`
	ProcessName string            `json:"process_name,omitempty"`
	Severity    string            `json:"severity,omitempty"`
	Detail      map[string]string `json:"detail,omitempty"`
}

type DiskHealth struct {
	Disk        string `json:"disk"`
	SMARTOk     bool   `json:"smart_ok"`
	PredictFail bool   `json:"predict_fail"`
}

// UpdateStatus mirrors agent/internal/model.UpdateStatus (read-only Windows
// Update posture: counts + timestamps only).
type UpdateStatus struct {
	PendingCount  int       `json:"pending_count"`
	RebootPending bool      `json:"reboot_pending"`
	LastSearch    time.Time `json:"last_search"`
	LastInstall   time.Time `json:"last_install"`
	CheckedAt     time.Time `json:"checked_at"`
}

// ── Endpoint hygiene (bloatware removal / startup control) ────────────────────
//
// PRIVACY: RemediationReport carries presence ONLY for items in the agent's
// COMPILED catalog (agent/internal/appcatalog, mirrored read-only in
// server/internal/appcatalog). It is deliberately NOT a general installed-software
// inventory — software the user installed themselves never appears here.
//
// AUTHORITY: the compiled catalog is the closed vocabulary. The server may only
// SELECT FROM it; any item_id the agent does not recognise is refused locally and
// reported back as outcome "unknown_item". This is desired-state reconciliation,
// not a command channel — see CLAUDE.md §Safety.

// RemediationReport mirrors agent/internal/model.RemediationReport — the agent's
// hygiene posture, sent on a low cadence.
type RemediationReport struct {
	// CatalogVersion is the hash of the agent's compiled vocabulary (kind:item_id
	// pairs only, so Turkish label edits do not force an agent rebuild).
	CatalogVersion string `json:"catalog_version"`
	// PolicyRevision is the revision the agent has actually applied. The server
	// omits the policy payload from its response when this already matches.
	PolicyRevision string               `json:"policy_revision"`
	Allowed        RemediationAllowed   `json:"allowed"`
	Present        []RemediationItem    `json:"present,omitempty"`
	Attempts       []RemediationAttempt `json:"attempts,omitempty"`
	CheckedAt      time.Time            `json:"checked_at"`
}

// RemediationAllowed mirrors the GPO-provisioned kill switches in the agent's
// config.json. The server cannot change these — they are the local veto.
// Enforce=false means the agent evaluates the policy and reports what it *would*
// do, changing nothing.
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
	TS     time.Time `json:"ts"`
	Kind   string    `json:"kind"` // app | startup
	ItemID string    `json:"item_id"`
	Action string    `json:"action"` // remove | deprovision | disable | enable
	// Outcome: ok | failed | dry_run | not_present | not_allowed | blocked |
	//          timeout | unknown_item
	Outcome    string `json:"outcome"`
	ErrorCode  string `json:"error_code,omitempty"`
	DurationMs int    `json:"duration_ms,omitempty"`
}

// ── Server → agent (the 202 ingest response) ──────────────────────────────────

// IngestResponse is the body returned from POST /api/v1/ingest (HTTP 202).
//
// It mirrors agent/internal/model.IngestResponse with ONE deliberate addition:
// `rejected`, which predates the remediation work and is still documented in
// docs/API.md §1. The agent's mirror does not declare it because the agent never
// consumes it, and encoding/json ignores unknown fields — so old agents (which
// only ever read `accepted`) and new agents both keep working with no lockstep
// deploy.
type IngestResponse struct {
	Accepted int              `json:"accepted"`
	Rejected []RejectedSample `json:"rejected,omitempty"`
	// Policy is present only when the server's resolved revision differs from the
	// one the agent reported, keeping the steady-state response tiny.
	Policy *RemediationPolicy `json:"policy,omitempty"`
}

// RemediationPolicy is the desired state for one machine, already resolved
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

// IngestResult is the pre-remediation response shape.
//
// Deprecated: use IngestResponse. Kept so nothing that still references the old
// name breaks; the wire shape of the two is identical apart from `policy`.
type IngestResult struct {
	Accepted int              `json:"accepted"`
	Rejected []RejectedSample `json:"rejected,omitempty"`
}

type RejectedSample struct {
	TS     time.Time `json:"ts"`
	Reason string    `json:"reason"`
}
