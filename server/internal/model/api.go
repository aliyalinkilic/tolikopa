package model

import "time"

// ── Auth ─────────────────────────────────────────────────────────────────────
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is the body of a completed sign-in (password only, or after the
// MFA step). The session itself travels in the httpOnly cookie.
type LoginResponse struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	CSRF     string `json:"csrf"`
	// MFAEnrollmentRequired: the instance requires 2FA and this account has not
	// enrolled yet. Always present.
	MFAEnrollmentRequired bool `json:"mfa_enrollment_required"`
}

// LoginMFAChallenge is returned (HTTP 200, no cookie) when the password was
// correct but the account has two-factor enabled.
type LoginMFAChallenge struct {
	MFARequired bool   `json:"mfa_required"`
	MFAToken    string `json:"mfa_token"`
}

type LoginMFARequest struct {
	MFAToken string `json:"mfa_token"`
	Code     string `json:"code"` // 6-digit TOTP or a recovery code
}

// UserPrefs are per-user panel preferences. Lang nil = follow the instance default.
type UserPrefs struct {
	Theme *string `json:"theme"` // dark | light | system | null (never chosen)
	Lang  *string `json:"lang"`  // tr | en | null
}

type MeResponse struct {
	Username    string     `json:"username"`
	Role        string     `json:"role"`
	DisplayName string     `json:"display_name"`
	TOTPEnabled bool       `json:"totp_enabled"`
	Prefs       UserPrefs  `json:"prefs"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at"`
	// MFAEnrollmentRequired mirrors LoginResponse; always present.
	MFAEnrollmentRequired bool `json:"mfa_enrollment_required"`
}

type TOTPSetupResponse struct {
	Secret     string `json:"secret"` // base32, no padding
	OTPAuthURL string `json:"otpauth_url"`
}

type RecoveryCodesResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

// PanelUser is one admin-panel account as the management endpoints return it.
type PanelUser struct {
	ID          int64      `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	Role        string     `json:"role"` // admin | viewer
	Disabled    bool       `json:"disabled"`
	TOTPEnabled bool       `json:"totp_enabled"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at"`
}

// ── General settings ─────────────────────────────────────────────────────────
type GeneralSettings struct {
	OrgName     string `json:"org_name"`
	DefaultLang string `json:"default_lang"` // tr | en
	// Require2FA forces every panel account (admin and viewer) to enrol in
	// two-factor before it can use anything but the enrolment endpoints.
	Require2FA bool `json:"require_2fa"`
}

// PublicSettings is served without authentication (the login page needs it).
type PublicSettings struct {
	OrgName     string `json:"org_name"`
	DefaultLang string `json:"default_lang"`
	Version     string `json:"version"`
	// NeedsAdmin: no panel account exists yet (first run).
	// Require2FA is the "require 2FA" policy, exposed so a non-admin's profile
	// page can know it without probing an endpoint that answers 409.
	Require2FA bool `json:"require_2fa"`
	NeedsAdmin bool `json:"needs_admin"`
	// SetupComplete: an admin has finished the setup wizard.
	SetupComplete bool `json:"setup_complete"`
}

// ── First-run setup + rollout ────────────────────────────────────────────────

// SetupStatus is GET /api/setup/status (no auth).
type SetupStatus struct {
	NeedsAdmin    bool `json:"needs_admin"`
	SetupComplete bool `json:"setup_complete"`
}

// SetupConfig is the rollout configuration the wizard collects. It holds NO
// credentials of any kind: the server never has domain credentials.
type SetupConfig struct {
	ServerURL         string `json:"server_url"`  // https://host[:port]; the agents' backend_url
	DomainFQDN        string `json:"domain_fqdn"` // corp.example.com
	NetBIOS           string `json:"netbios"`     // optional
	TargetDN          string `json:"target_dn"`   // GPO link target; "" = domain root
	GPOName           string `json:"gpo_name"`
	DefenderExclusion bool   `json:"defender_exclusion"`
}

// SetupReadiness: what a rollout needs. None depends on a machine reporting.
type SetupReadiness struct {
	ServerURL bool `json:"server_url"`
	RootCA    bool `json:"root_ca"`
	Artifacts bool `json:"artifacts"`
	Domain    bool `json:"domain"`
}

type AgentVersionCount struct {
	Version string `json:"version"` // "" = machine never reported a version
	Count   int    `json:"count"`
}

type SetupFleet struct {
	Total          int                 `json:"total"`
	ByAgentVersion []AgentVersionCount `json:"by_agent_version"`
	Current        int                 `json:"current"`  // on the shipped agent version
	Outdated       int                 `json:"outdated"` // on any other version
}

// SetupOverview is GET /api/admin/setup. Artifacts is any so the API layer can
// pass *artifacts.Info (or nil → JSON null) without model importing it.
type SetupOverview struct {
	Config              SetupConfig    `json:"config"`
	CompletedAt         *time.Time     `json:"completed_at"`
	IngestTokenMasked   string         `json:"ingest_token_masked"`
	Readiness           SetupReadiness `json:"readiness"`
	Artifacts           any            `json:"artifacts"`
	Fleet               SetupFleet     `json:"fleet"`
	UpdateAvailable     bool           `json:"update_available"`
	ServerURLCandidates []string       `json:"server_url_candidates"`
}

// SetupCompletion is the body of POST /api/admin/setup/complete|reopen.
type SetupCompletion struct {
	CompletedAt *time.Time `json:"completed_at"`
}

// ── Fleet overview (served from machine_live only) ─────────────────────────────
type FleetMachine struct {
	MachineID      string    `json:"machine_id"`
	Hostname       string    `json:"hostname"`
	Kind           string    `json:"kind"`
	Domain         string    `json:"domain,omitempty"`
	Online         bool      `json:"online"`
	CPUPct         float64   `json:"cpu_pct"`
	MemRatio       float64   `json:"mem_ratio"`
	DiskMinFreePct float64   `json:"disk_min_free_pct"`
	LogonUser      string    `json:"logon_user"`
	SessionState   string    `json:"session_state"`
	OpenAlerts     int       `json:"open_alerts"`
	LastSeen       time.Time `json:"last_seen"`
	AgentVersion   string    `json:"agent_version,omitempty"`
	Tags           []string  `json:"tags"`
	// Windows Update posture (read-only). PendingUpdates nil = unknown/not yet reported.
	PendingUpdates   *int           `json:"pending_updates,omitempty"`
	RebootPending    bool           `json:"reboot_pending"`
	UpdatesCheckedAt *time.Time     `json:"updates_checked_at,omitempty"`
	Battery          *BatteryDTO    `json:"battery,omitempty"` // nil = never reported
	Labels           map[string]any `json:"labels,omitempty"`
}

// BatteryDTO is a machine's battery/power state. Nil pointers = unknown. Absent
// (nil Battery) means the machine never reported; Present=false means desktop/VM.
type BatteryDTO struct {
	Present            bool       `json:"present"`
	Percent            *int       `json:"percent,omitempty"`
	ACConnected        bool       `json:"ac_connected"`
	Charging           bool       `json:"charging"`
	HealthPct          *int       `json:"health_pct,omitempty"`
	DesignCapacity     *int       `json:"design_capacity,omitempty"`
	FullChargeCapacity *int       `json:"full_charge_capacity,omitempty"`
	CycleCount         *int       `json:"cycle_count,omitempty"`
	UpdatedAt          *time.Time `json:"updated_at,omitempty"`
}

// BatteryCols are the machine table's scannable battery columns (order matches
// the SELECTs in fleet/machine/ws). DTO() assembles the API shape, or nil when
// the machine has never reported battery data (UpdatedAt == nil).
type BatteryCols struct {
	Present   bool
	Percent   *int
	AC        bool
	Charging  bool
	Health    *int
	Design    *int
	Full      *int
	Cycles    *int
	UpdatedAt *time.Time
}

func (b BatteryCols) DTO() *BatteryDTO {
	if b.UpdatedAt == nil {
		return nil
	}
	return &BatteryDTO{
		Present: b.Present, ACConnected: b.AC, Charging: b.Charging,
		Percent: b.Percent, HealthPct: b.Health, DesignCapacity: b.Design,
		FullChargeCapacity: b.Full, CycleCount: b.Cycles, UpdatedAt: b.UpdatedAt,
	}
}

type FleetSummary struct {
	Total            int            `json:"total"`
	Online           int            `json:"online"`
	Offline          int            `json:"offline"`
	Active           int            `json:"active"` // machine actively in use right now
	Idle             int            `json:"idle"`   // online, logged on + unlocked, but no input >= 5 min
	AlertsBySeverity map[string]int `json:"alerts_by_severity"`
	AvgCPU           float64        `json:"avg_cpu"`
	AvgMem           float64        `json:"avg_mem"`
}

// ── Homepage dashboard: fleet overview + stats (docs/API.md §3 / §9) ───────────

// FleetOverview is the one-shot payload behind the homepage dashboard.
// Live parts come from machine_live; history from metric_5m.
type FleetOverview struct {
	GeneratedAt  time.Time            `json:"generated_at"`
	Totals       OverviewTotals       `json:"totals"`
	Health       HealthSummary        `json:"health"`
	AvgCPU       float64              `json:"avg_cpu"` // 0..100 across online machines
	AvgMem       float64              `json:"avg_mem"` // ratio 0..1 across online machines
	History      []FleetHistoryPoint  `json:"history"`
	TopCPU       []TopCPUMachine      `json:"top_cpu"`
	CriticalMem  []CriticalMemMachine `json:"critical_mem"`
	CriticalDisk []CriticalDiskMount  `json:"critical_disk"`
	RecentEvents []OverviewEvent      `json:"recent_events"`
	RecentAlerts []AlertDTO           `json:"recent_alerts"`
}

type OverviewTotals struct {
	Total   int `json:"total"`
	Online  int `json:"online"`
	Offline int `json:"offline"`
	InUse   int `json:"in_use"` // online with session_state=active
	Idle    int `json:"idle"`   // online with session_state=idle
	Locked  int `json:"locked"` // online with session_state=locked
}

// HealthSummary is a 0-100 fleet health score: 100 minus capped per-issue
// penalties (formula in docs/API.md). Issue keys are stable identifiers the
// frontend translates: offline | high_cpu | high_mem | low_disk | smart |
// reboot_pending | crit_alerts. Only issues with count > 0 are listed.
type HealthSummary struct {
	Score  int           `json:"score"`
	Issues []HealthIssue `json:"issues"`
}

type HealthIssue struct {
	Key     string `json:"key"`
	Count   int    `json:"count"`
	Penalty int    `json:"penalty"`
}

type FleetHistoryPoint struct {
	TS          time.Time `json:"ts"`
	CPUAvg      float64   `json:"cpu_avg"`
	MemRatioAvg float64   `json:"mem_ratio_avg"`
}

type TopCPUMachine struct {
	MachineID string  `json:"machine_id"`
	Hostname  string  `json:"hostname"`
	LogonUser string  `json:"logon_user"`
	CPUPct    float64 `json:"cpu_pct"`
	MemRatio  float64 `json:"mem_ratio"`
}

type CriticalMemMachine struct {
	MachineID string  `json:"machine_id"`
	Hostname  string  `json:"hostname"`
	LogonUser string  `json:"logon_user"`
	MemRatio  float64 `json:"mem_ratio"`
	MemUsed   int64   `json:"mem_used"`
	MemTotal  int64   `json:"mem_total"`
}

type CriticalDiskMount struct {
	MachineID string  `json:"machine_id"`
	Hostname  string  `json:"hostname"`
	Mount     string  `json:"mount"`
	UsedPct   float64 `json:"used_pct"`
	FreeBytes int64   `json:"free_bytes"`
}

// OverviewEvent is a slim event row for the dashboard feed (no detail payload).
type OverviewEvent struct {
	TS          time.Time `json:"ts"`
	MachineID   string    `json:"machine_id"`
	Hostname    string    `json:"hostname"`
	Kind        string    `json:"kind"`
	ProcessName string    `json:"process_name"`
	Severity    string    `json:"severity"`
}

// HourlyActivityResponse is the fleet-wide busy-hours heatmap (session_user_1h).
// active_seconds = active_samples × 15 s (nominal vitals cadence).
type HourlyActivityResponse struct {
	Days  int                  `json:"days"`
	Cells []HourlyActivityCell `json:"cells"`
}

type HourlyActivityCell struct {
	Dow           int   `json:"dow"`  // ISO: 1=Monday .. 7=Sunday (Europe/Istanbul)
	Hour          int   `json:"hour"` // 0..23 local (Europe/Istanbul)
	ActiveSeconds int64 `json:"active_seconds"`
	Samples       int64 `json:"samples"` // total vitals samples observed in the cell
}

// TopAppsResponse ranks fleet-wide application consumption (proc_name_1h,
// LEFT-joined with app-focus rollups for foreground seconds).
type TopAppsResponse struct {
	Apps []TopApp `json:"apps"`
}

type TopApp struct {
	Name         string  `json:"name"`
	Category     string  `json:"category"` // "" when unclassified
	CPUAvg       float64 `json:"cpu_avg"`
	CPUMax       float64 `json:"cpu_max"`
	MemAvg       int64   `json:"mem_avg"` // bytes
	Machines     int     `json:"machines"`
	FocusSeconds int64   `json:"focus_seconds"`
	ActiveHours  float64 `json:"active_hours"` // distinct hour buckets the app appeared in
}

// SlowMachinesResponse ranks machines by a "slowness risk" score built from the
// telemetry already collected — no new agent capability is involved. The score is
// additive (higher = worse), clamped to 100, and every contributing factor is
// returned in Reasons so the panel can explain the ranking instead of asserting it.
type SlowMachinesResponse struct {
	Days        int           `json:"days"`
	GeneratedAt time.Time     `json:"generated_at"`
	Machines    []SlowMachine `json:"machines"`
}

type SlowMachine struct {
	MachineID string    `json:"machine_id"`
	Hostname  string    `json:"hostname"`
	LogonUser string    `json:"logon_user"`
	Online    bool      `json:"online"`
	LastSeen  time.Time `json:"last_seen"`
	Score     int       `json:"score"`    // 0..100, higher = slower
	Severity  string    `json:"severity"` // ok | warn | crit

	Reasons []SlowReason `json:"reasons"`

	// Measurements backing the score (zero when not reported yet).
	MemTotal      int64   `json:"mem_total"`
	MemRatioAvg   float64 `json:"mem_ratio_avg"` // 0..1, mean over the window
	MemRatioMax   float64 `json:"mem_ratio_max"` // 0..1, peak over the window
	CPUAvg        float64 `json:"cpu_avg"`       // 0..100, mean over the window
	CPUCores      int     `json:"cpu_cores,omitempty"`
	CPUModel      string  `json:"cpu_model,omitempty"`
	DiskMount     string  `json:"disk_mount,omitempty"` // the fullest mount
	DiskUsedPct   float64 `json:"disk_used_pct"`
	DiskFreeBytes int64   `json:"disk_free_bytes"`
	UptimeDays    float64 `json:"uptime_days"`
	Crashes       int     `json:"crashes"`      // app_crash + app_hang in the window
	SMARTBad      bool    `json:"smart_bad"`    // any disk failing or predicting failure
	SampleHours   float64 `json:"sample_hours"` // metric_1h buckets backing mem/cpu averages
}

// SlowReason is one scored factor. Value carries the measurement that produced
// the points so the UI can render "%92 bellek" rather than a bare label.
type SlowReason struct {
	Key    string  `json:"key"` // mem_pressure|low_ram|disk_full|long_uptime|weak_cpu|smart|crashes
	Points int     `json:"points"`
	Value  float64 `json:"value"`
}

// ── Machine detail ─────────────────────────────────────────────────────────────
type MachineDetail struct {
	FleetMachine
	// ArchivedAt is set when the machine was retired from the panel; the detail
	// page still opens (history is kept) and offers "unarchive".
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	ArchivedBy string     `json:"archived_by,omitempty"`
	OS       string `json:"os,omitempty"`
	OSBuild  string `json:"os_build,omitempty"`
	CPUModel string `json:"cpu_model,omitempty"`
	CPUCores int    `json:"cpu_cores,omitempty"`
	// IPAddrs: agent-reported own LAN IPv4 addresses (empty for agents < 1.7).
	IPAddrs []string `json:"ip_addrs"`
	// LastIP: source address of the last ingest request — the fallback witness.
	LastIP             string          `json:"last_ip,omitempty"`
	IPUpdatedAt        *time.Time      `json:"ip_updated_at,omitempty"`
	UpdatesLastSearch  *time.Time      `json:"updates_last_search,omitempty"`
	UpdatesLastInstall *time.Time      `json:"updates_last_install,omitempty"`
	FirstSeen          time.Time       `json:"first_seen"`
	MemUsed            int64           `json:"mem_used"`
	MemTotal           int64           `json:"mem_total"`
	Disks              []DiskUsage     `json:"disks"`
	DiskHealth         []DiskHealthDTO `json:"disk_health"`
}

type DiskUsage struct {
	Mount   string  `json:"mount"`
	Used    int64   `json:"used"`
	Total   int64   `json:"total"`
	FreePct float64 `json:"free_pct"`
}

type DiskHealthDTO struct {
	Disk        string    `json:"disk"`
	SMARTOk     bool      `json:"smart_ok"`
	PredictFail bool      `json:"predict_fail"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// MetricSeries is the response for /machines/{id}/metrics.
type MetricSeries struct {
	Resolution string        `json:"resolution"` // raw | 5m | 1h
	CPU        []SeriesPoint `json:"cpu,omitempty"`
	Mem        []SeriesPoint `json:"mem,omitempty"`
	Net        []NetPoint    `json:"net,omitempty"`
}

type SeriesPoint struct {
	T   time.Time `json:"t"`
	Avg float64   `json:"avg"`
	Max float64   `json:"max"`
}

type NetPoint struct {
	T    time.Time `json:"t"`
	Sent int64     `json:"sent"`
	Recv int64     `json:"recv"`
}

type DiskSeries struct {
	Mount  string      `json:"mount"`
	Points []DiskPoint `json:"points"`
}

type DiskPoint struct {
	T     time.Time `json:"t"`
	Used  int64     `json:"used"`
	Total int64     `json:"total"`
}

type TopProcess struct {
	Name     string  `json:"name"`
	Category string  `json:"category"`
	CPUPct   float64 `json:"cpu_pct"`
	MemBytes int64   `json:"mem_bytes"`
}

type CategoryShare struct {
	Category string  `json:"category"`
	CPUSum   float64 `json:"cpu_sum"`
	Samples  int64   `json:"samples"`
	Share    float64 `json:"share"` // 0..1
}

// AppUsage is per-application foreground (active-use) time. Name is the process
// base name only — never a window title, document, or URL.
type AppUsage struct {
	Name    string `json:"name"`
	Seconds int64  `json:"seconds"`
}

// ── Users ──────────────────────────────────────────────────────────────────────
type UserSummary struct {
	LogonUser      string    `json:"logon_user"`
	DisplayName    string    `json:"display_name,omitempty"`
	Tags           []string  `json:"tags"`
	LastSeen       time.Time `json:"last_seen"`
	CurrentMachine string    `json:"current_machine,omitempty"`
	Online         bool      `json:"online"`
}

// ── Activity / productivity (category + active hours; NO window titles/URLs) ─────
type HeatmapCell struct {
	Weekday       int `json:"weekday"` // 0=Mon .. 6=Sun (Europe/Istanbul)
	Hour          int `json:"hour"`    // 0..23 local
	ActiveMinutes int `json:"active_minutes"`
}

type DailyActivity struct {
	Date            string `json:"date"` // YYYY-MM-DD (Europe/Istanbul)
	ActiveMinutes   int    `json:"active_minutes"`
	LoggedOnMinutes int    `json:"loggedon_minutes"`
}

type ActivityResponse struct {
	Heatmap            []HeatmapCell   `json:"heatmap"`
	Daily              []DailyActivity `json:"daily"`
	TotalActiveMinutes int             `json:"total_active_minutes"`
	TotalLoggedMinutes int             `json:"total_loggedon_minutes"`
}

// ── User profiles / department tags ─────────────────────────────────────────────
type UserProfileUpdate struct {
	DisplayName string   `json:"display_name"`
	Tags        []string `json:"tags"`
}

type TagInfo struct {
	Tag       string `json:"tag"`
	UserCount int    `json:"user_count"`
}

type TagUserRow struct {
	LogonUser     string `json:"logon_user"`
	DisplayName   string `json:"display_name,omitempty"`
	ActiveMinutes int    `json:"active_minutes"`
	TopCategory   string `json:"top_category,omitempty"`
}

type TagSummary struct {
	Tag            string          `json:"tag"`
	Users          []TagUserRow    `json:"users"`
	CategoryTotals []CategoryShare `json:"category_totals"`
}

type SessionItem struct {
	MachineID string     `json:"machine_id"`
	Hostname  string     `json:"hostname"`
	Kind      string     `json:"kind"` // active | logged_on
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
}

// UserLogon is one continuous stretch a user was the console user of a machine
// (GET /api/users/{user}/logons). EndedAt is nil while it is still running.
// active_seconds = active samples × 15 s (nominal vitals cadence).
type UserLogon struct {
	MachineID     string     `json:"machine_id"`
	Hostname      string     `json:"hostname"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at"`
	ActiveSeconds int64      `json:"active_seconds"`
}

// ── Alerts / events ────────────────────────────────────────────────────────────
type AlertDTO struct {
	ID         int64          `json:"id"`
	MachineID  string         `json:"machine_id"`
	Hostname   string         `json:"hostname,omitempty"`
	Rule       string         `json:"rule"`
	Severity   string         `json:"severity"`
	Status     string         `json:"status"`
	Value      *float64       `json:"value,omitempty"`
	// Message is a pre-rendered fallback (the instance default language at the
	// time the alert was last evaluated). Clients localise from Rule + Params.
	Message    string     `json:"message"`
	OpenedAt   time.Time  `json:"opened_at"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	// Params holds every value the rule's message interpolates (docs/API.md §6).
	// Always an object, never null.
	Params map[string]any `json:"params"`
	// Detail is the same object under its pre-i18n name, kept for older clients.
	Detail map[string]any `json:"detail,omitempty"`
}

type EventDTO struct {
	TS          time.Time      `json:"ts"`
	MachineID   string         `json:"machine_id"`
	Kind        string         `json:"kind"`
	ProcessName string         `json:"process_name,omitempty"`
	Severity    string         `json:"severity,omitempty"`
	Detail      map[string]any `json:"detail,omitempty"`
}

// ── Admin: classification + rules ──────────────────────────────────────────────
type CategoryRule struct {
	ID         int    `json:"id"`
	Category   string `json:"category"`
	MatchType  string `json:"match_type"`
	MatchValue string `json:"match_value"`
	Priority   int    `json:"priority"`
	Enabled    bool   `json:"enabled"`
}

type UnclassifiedProcess struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type AlertRuleDTO struct {
	Rule          string   `json:"rule"`
	Enabled       bool     `json:"enabled"`
	WarnThreshold *float64 `json:"warn_threshold"`
	CritThreshold *float64 `json:"crit_threshold"`
	ForSeconds    int      `json:"for_seconds"`
}

// ── Admin: endpoint hygiene (remediation) ──────────────────────────────────────
//
// AUTHORITY: the agent carries a COMPILED allowlist plus a local GPO kill switch.
// These DTOs let the panel SELECT FROM that vocabulary and read back what the
// fleet reports — they are a UI mirror and an audit trail, never a command
// channel (CLAUDE.md §Safety).

// RemediationSettings is the one-shot GET /api/admin/remediation payload: the
// catalog grouped by kind, the current policy, per-item fleet presence counts and
// a fleet drift summary.
type RemediationSettings struct {
	// CatalogVersion is the SERVER's embedded vocabulary hash; machines reporting
	// a different one are counted in Fleet.CatalogStale.
	CatalogVersion string                   `json:"catalog_version"`
	Apps           []RemediationCatalogItem `json:"apps"`
	Startup        []RemediationCatalogItem `json:"startup"`
	Policy         RemediationPolicyDTO     `json:"policy"`
	Fleet          RemediationFleetSummary  `json:"fleet"`
}

// RemediationCatalogItem is one entry of the closed vocabulary as the panel sees
// it: catalog metadata + whether the policy selects it + how many machines still
// have it. Deprecated items are returned FLAGGED rather than filtered, so an item
// that is still selected or still present somewhere does not vanish silently.
type RemediationCatalogItem struct {
	Kind         string   `json:"kind"`    // app | startup
	ItemID       string   `json:"item_id"` // catalog key
	Family       string   `json:"family"`
	DisplayName  string   `json:"display_name"` // Turkish
	Category     string   `json:"category,omitempty"`
	RiskTier     string   `json:"risk_tier"` // safe | review | advanced
	Scope        string   `json:"scope,omitempty"`
	SafeDefault  bool     `json:"safe_default"`
	ProcessNames []string `json:"process_names,omitempty"`
	Notes        string   `json:"notes,omitempty"`
	Deprecated   bool     `json:"deprecated"`
	Selected     bool     `json:"selected"`      // in the current policy
	PresentCount int      `json:"present_count"` // machines where it is still present
}

// RemediationPolicyDTO is the stored fleet-wide policy. Enabled=false and
// DryRun=true are the shipped defaults; a fresh install never actuates.
type RemediationPolicyDTO struct {
	Enabled      bool       `json:"enabled"`
	DryRun       bool       `json:"dry_run"`
	ExcludedTags []string   `json:"excluded_tags"`
	Apps         []string   `json:"apps"`    // selected app item_ids
	Startup      []string   `json:"startup"` // selected startup item_ids
	Revision     int64      `json:"revision"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
	UpdatedBy    string     `json:"updated_by,omitempty"`
}

// RemediationPolicyUpdate is the PUT /api/admin/remediation/policy request body.
// Item ids outside the server's catalog are rejected with 400.
type RemediationPolicyUpdate struct {
	Enabled      bool     `json:"enabled"`
	DryRun       bool     `json:"dry_run"`
	ExcludedTags []string `json:"excluded_tags"`
	Apps         []string `json:"apps"`
	Startup      []string `json:"startup"`
	// Force overrides the server-side dry-run-first interlock (409 otherwise) when
	// arming enforcement without any observed dry-run result. It is recorded in
	// remediation_policy_audit.after, so an override is never invisible.
	Force bool `json:"force,omitempty"`
}

// RemediationFleetSummary is the drift/posture roll-up across the inventory.
type RemediationFleetSummary struct {
	Machines      int `json:"machines"`        // machines in the inventory
	Reporting     int `json:"reporting"`       // ...that have ever sent a hygiene report
	NeverReported int `json:"never_reported"`  // agent too old, or never checked in
	CatalogStale  int `json:"catalog_stale"`   // reported catalog_version != server's
	PolicyCurrent int `json:"policy_current"`  // already applied the current revision
	KillSwitchOff int `json:"kill_switch_off"` // local GPO switch forbids actuation
}

// RemediationAttemptDTO is one audited action for the admin feed.
// ErrorCode is a CODE ("0x80073cfa") — never a message or a path.
type RemediationAttemptDTO struct {
	TS          time.Time `json:"ts"`
	MachineID   string    `json:"machine_id"`
	Hostname    string    `json:"hostname,omitempty"`
	Kind        string    `json:"kind"`
	ItemID      string    `json:"item_id"`
	DisplayName string    `json:"display_name,omitempty"` // "" when the id left the catalog
	Action      string    `json:"action"`
	Outcome     string    `json:"outcome"`
	ErrorCode   string    `json:"error_code,omitempty"`
	DurationMs  int       `json:"duration_ms,omitempty"`
	// Resolved is set on failed/timeout/blocked rows only: true when a LATER attempt
	// converged (ok / not_present) or the machine now reports the item absent.
	Resolved *bool `json:"resolved,omitempty"`
}

type RemediationAttemptsResponse struct {
	Attempts []RemediationAttemptDTO `json:"attempts"`
}

// RemediationFailureGroup is one (item, outcome, error_code) bucket of OPEN
// failures: machines whose latest actuation attempt for the item did not converge
// and which still report the item present (or have not reported since).
type RemediationFailureGroup struct {
	Kind        string    `json:"kind"`
	ItemID      string    `json:"item_id"`
	DisplayName string    `json:"display_name,omitempty"`
	Outcome     string    `json:"outcome"`
	ErrorCode   string    `json:"error_code,omitempty"`
	Machines    int       `json:"machines"`
	Hostnames   []string  `json:"hostnames"` // first few, for the panel; full list via /attempts
	// StaleAgents counts the group's machines whose agent is behind
	// LatestAgentVersion. A failure there is the old agent's until proven otherwise.
	StaleAgents int       `json:"stale_agents"`
	LastTS      time.Time `json:"last_ts"`
}

// RemediationFailuresResponse — GET /api/admin/remediation/failures.
type RemediationFailuresResponse struct {
	Open []RemediationFailureGroup `json:"open"`
	// ResolvedMachineItems counts (machine,item) pairs that failed at least once in
	// the window but have since converged — the "looked failed, is fine" rows.
	ResolvedMachineItems int `json:"resolved_machine_items"`
	OpenMachineItems     int `json:"open_machine_items"`
	// LatestAgentVersion is the highest agent version any active machine reports —
	// the server is never told what "current" is, so the fleet's own maximum stands
	// in for it. Empty when no machine reports a plain dotted version.
	LatestAgentVersion string `json:"latest_agent_version,omitempty"`
}

// RemediationImpactWindow is one before/after comparison of machine-level usage.
// Averages are over ACTIVE-session hours only (see GetRemediationImpact).
type RemediationImpactWindow struct {
	CPUBefore   *float64 `json:"cpu_before"`   // % (0-100)
	CPUAfter    *float64 `json:"cpu_after"`    // %
	MemBefore   *float64 `json:"mem_before"`   // ratio (0-1)
	MemAfter    *float64 `json:"mem_after"`    // ratio
	HoursBefore int      `json:"hours_before"` // active hours behind each average
	HoursAfter  int      `json:"hours_after"`
}

type RemediationImpactMachine struct {
	MachineID    string    `json:"machine_id"`
	Hostname     string    `json:"hostname,omitempty"`
	FirstRemoval time.Time `json:"first_removal"`
	LastRemoval  time.Time `json:"last_removal"`
	ItemsRemoved int       `json:"items_removed"`
	// Complete: the full after-window has elapsed; otherwise the after side is partial.
	Complete bool `json:"complete"`
	RemediationImpactWindow
}

type RemediationImpactItem struct {
	ItemID      string `json:"item_id"`
	DisplayName string `json:"display_name,omitempty"`
	Machines    int    `json:"machines"` // machines this item was removed from in the window
	// Own-process footprint (catalog process_names) on those machines. Process rows
	// are a top-N sample, so these are LOWER BOUNDS; nil = never seen in the sample.
	ProcMachinesBefore int      `json:"proc_machines_before"`
	ProcCPUBefore      *float64 `json:"proc_cpu_before"`
	ProcMemBefore      *float64 `json:"proc_mem_before"`     // bytes
	ProcMachinesAfter  int      `json:"proc_machines_after"` // should be 0 — a re-appearance
}

// RemediationImpactResponse — GET /api/admin/remediation/impact?days=.
type RemediationImpactResponse struct {
	Days        int       `json:"days"`
	GeneratedAt time.Time `json:"generated_at"`
	MinHours    int       `json:"min_hours"` // per side, for a machine to enter the fleet figure
	// Fleet is the PAIRED mean over machines with >= MinHours on both sides.
	Fleet         RemediationImpactWindow `json:"fleet"`
	FleetMachines int                     `json:"fleet_machines"`
	// Control: machines with NO successful removal, same calendar windows (anchored
	// on the median removal time) — separates the removal from a fleet-wide trend.
	Control         RemediationImpactWindow    `json:"control"`
	ControlMachines int                        `json:"control_machines"`
	Machines        []RemediationImpactMachine `json:"machines"`
	Items           []RemediationImpactItem    `json:"items"`
}

// MachineRemediation is GET /api/machines/{id}/remediation: what this machine
// still has from the ALLOWLIST (never a software inventory) plus its recent
// attempts.
type MachineRemediation struct {
	MachineID string `json:"machine_id"`
	// CatalogVersion is what the AGENT reported; ServerCatalogVersion is what this
	// server ships. CatalogStale = the two disagree (agent binary out of date).
	CatalogVersion       string                   `json:"catalog_version,omitempty"`
	ServerCatalogVersion string                   `json:"server_catalog_version"`
	CatalogStale         bool                     `json:"catalog_stale"`
	PolicyRevision       string                   `json:"policy_revision,omitempty"`
	Allowed              bool                     `json:"allowed"` // local GPO kill switch permits actuation
	ReportedAt           *time.Time               `json:"reported_at,omitempty"`
	Items                []MachineRemediationItem `json:"items"`
	Attempts             []RemediationAttemptDTO  `json:"attempts"`
}

type MachineRemediationItem struct {
	Kind          string     `json:"kind"`
	ItemID        string     `json:"item_id"`
	DisplayName   string     `json:"display_name,omitempty"`
	RiskTier      string     `json:"risk_tier,omitempty"`
	Present       bool       `json:"present"`
	Scope         string     `json:"scope,omitempty"`
	Enabled       *bool      `json:"enabled,omitempty"` // startup entries only
	Selected      bool       `json:"selected"`          // targeted by the current policy
	FirstSeen     time.Time  `json:"first_seen"`
	LastSeen      time.Time  `json:"last_seen"`
	LastPresentAt *time.Time `json:"last_present_at,omitempty"`
	RemovedAt     *time.Time `json:"removed_at,omitempty"`
}

// ── WebSocket envelope ─────────────────────────────────────────────────────────
type WSMessage struct {
	Type    string `json:"type"` // fleet.update | machine.metric | alert.opened | alert.resolved
	Payload any    `json:"payload"`
}

// MachineArchiveRequest — POST /api/admin/machines/archive and …/restore.
type MachineArchiveRequest struct {
	MachineIDs []string `json:"machine_ids"`
}

// MachineArchiveResult reports how many of the requested machines changed state;
// ids that were unknown or already in the target state are simply not counted.
type MachineArchiveResult struct {
	Changed int `json:"changed"`
}

// ArchivedMachine — one row of GET /api/admin/machines/archived.
type ArchivedMachine struct {
	MachineID    string     `json:"machine_id"`
	Hostname     string     `json:"hostname"`
	Kind         string     `json:"kind"`
	OS           string     `json:"os,omitempty"`
	AgentVersion string     `json:"agent_version,omitempty"`
	FirstSeen    time.Time  `json:"first_seen"`
	LastSeen     *time.Time `json:"last_seen,omitempty"`
	ArchivedAt   time.Time  `json:"archived_at"`
	ArchivedBy   string     `json:"archived_by,omitempty"`
}

type ArchivedMachinesResponse struct {
	Machines []ArchivedMachine `json:"machines"`
}
