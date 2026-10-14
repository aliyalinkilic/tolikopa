// Wire contract — mirrors server/internal/model/api.go (keep in sync by hand).
// Times are ISO-8601 UTC strings; byte counters are numbers (int64 on the wire).

// ── Auth ─────────────────────────────────────────────────────────────────────
export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  username: string
  role: string
  csrf: string
  /** The org requires 2FA and this account has not enrolled yet (see MeResponse). */
  mfa_enrollment_required?: boolean
}

/** POST /api/auth/login answers this (HTTP 200, no session yet) for a 2FA account. */
export interface MfaChallenge {
  mfa_required: true
  mfa_token: string
}

export type LoginResult = LoginResponse | MfaChallenge

export type ThemeSetting = 'dark' | 'light' | 'system'
export type LangSetting = 'tr' | 'en'

export interface UserPrefs {
  theme: ThemeSetting | null
  lang: LangSetting | null
}

export interface MeResponse {
  username: string
  role: string
  display_name?: string | null
  totp_enabled?: boolean
  prefs?: UserPrefs | null
  created_at?: string | null
  last_login_at?: string | null
  /**
   * True while the "require 2FA" policy is on and this account has no 2FA yet.
   * The session is valid, but everything except /api/auth/*, the 2FA setup/enable
   * calls, /api/me/prefs and /api/settings/public answers 403
   * `mfa_enrollment_required` until enrolment is finished.
   */
  mfa_enrollment_required?: boolean
  /** Present on some backends via a readable cookie; used to recover CSRF after refresh. */
  csrf?: string
}

/** GET /api/settings/public — readable without a session. */
export interface PublicSettings {
  org_name: string
  default_lang: LangSetting
  version: string
  /** The "require 2FA" policy — lets a viewer's profile know it cannot disable 2FA. */
  require_2fa?: boolean
  /** True on a fresh installation: no admin exists yet → show the setup wizard. */
  needs_admin?: boolean
  /** An admin has finished the setup wizard. */
  setup_complete?: boolean
}

// ── First-run setup / Setup page ───────────────────────────────────────────────
export interface SetupConfig {
  server_url: string
  domain_fqdn: string
  netbios: string
  /** distinguishedName of the OU the GPO is linked to; empty = domain root. */
  target_dn: string
  gpo_name: string
  defender_exclusion: boolean
}

export type ReadinessKey = 'server_url' | 'root_ca' | 'artifacts' | 'domain'

export interface SetupArtifacts {
  product_version: string
  agent_version: string
  agent_sha256: string
  built_at: string
}

export interface SetupFleet {
  total: number
  /** `version: ""` = the machine never reported one. */
  by_agent_version: { version: string; count: number }[]
  current: number
  outdated: number
}

/** GET /api/admin/setup */
export interface SetupState {
  config: SetupConfig
  completed_at: string | null
  ingest_token_masked: string
  readiness: Record<ReadinessKey, boolean>
  artifacts: SetupArtifacts | null
  fleet: SetupFleet
  update_available: boolean
  server_url_candidates: string[]
}

/** POST /api/setup/admin */
export interface SetupAdminRequest {
  setup_code: string
  username: string
  password: string
  display_name?: string
  lang?: LangSetting
}

/** GET|PUT /api/admin/settings/general */
export interface GeneralSettings {
  org_name: string
  default_lang: LangSetting
  require_2fa: boolean
}

export interface TotpSetup {
  secret: string
  otpauth_url: string
}

export interface RecoveryCodes {
  recovery_codes: string[]
}

export type SessionState = 'active' | 'idle' | 'locked' | 'disconnected' | 'none'

// ── Fleet overview (served from machine_live only) ─────────────────────────────
export interface FleetMachine {
  machine_id: string
  hostname: string
  kind: string
  domain?: string
  online: boolean
  cpu_pct: number
  mem_ratio: number
  disk_min_free_pct: number
  logon_user: string
  session_state: SessionState | string
  open_alerts: number
  last_seen: string
  agent_version?: string
  labels?: Record<string, unknown>
  /** Machine group tags set by an admin. May be absent on older payloads → treat as []. */
  tags: string[]
  /** Count of pending Windows updates the OS already knows about. null/absent = unknown (never checked / older agent). */
  pending_updates?: number | null
  /** True when a reboot is required to finish applying updates. Absent on older payloads → treat as false. */
  reboot_pending: boolean
  /** ISO-8601 timestamp of the agent's last update check; null/absent = never reported. */
  updates_checked_at?: string | null
  /** Battery / power state. Absent/null = never reported (older agent). present=false = desktop/VM. */
  battery?: BatteryDTO | null
}

/** Battery + power state. Laptops report `present: true`; desktops/VMs report `present: false`. */
export interface BatteryDTO {
  present: boolean
  /** 0-100 current charge; absent = unknown. */
  percent?: number | null
  ac_connected: boolean
  charging: boolean
  /** 0-100 = full-charge capacity / design capacity (battery wear); absent = unknown. */
  health_pct?: number | null
  /** mWh */
  design_capacity?: number | null
  /** mWh */
  full_charge_capacity?: number | null
  /** charge cycles; absent = firmware didn't report. */
  cycle_count?: number | null
  updated_at?: string | null
}

export interface FleetSummary {
  total: number
  online: number
  offline: number
  active: number
  /** Online, logged on + unlocked, no input for >= 5 min. Never counted as active. */
  idle: number
  /** Keyed by the server's severity values: `warning` | `critical`. */
  alerts_by_severity: Record<string, number>
  /** 0..100 */
  avg_cpu: number
  /** PERCENT 0..100 here — unlike FleetOverview.avg_mem, which is a 0..1 ratio. */
  avg_mem: number
}

// ── Fleet overview dashboard (GET /api/fleet/overview) ─────────────────────────
export interface OverviewTotals {
  total: number
  online: number
  offline: number
  in_use: number
  idle: number
  locked: number
}

export type HealthIssueKey =
  | 'offline'
  | 'high_cpu'
  | 'high_mem'
  | 'low_disk'
  | 'smart'
  | 'reboot_pending'
  | 'crit_alerts'

export interface HealthIssue {
  key: HealthIssueKey | string
  count: number
  penalty: number
}

export interface OverviewHealth {
  /** 0..100 fleet health score. */
  score: number
  issues: HealthIssue[]
}

/** Fleet-average sample (24h, 5-minute buckets). */
export interface OverviewHistoryPoint {
  ts: string
  /** 0..100 */
  cpu_avg: number
  /** ratio 0..1 */
  mem_ratio_avg: number
}

export interface TopCpuEntry {
  machine_id: string
  hostname: string
  logon_user: string
  cpu_pct: number
  mem_ratio: number
}

export interface CriticalMemEntry {
  machine_id: string
  hostname: string
  logon_user: string
  mem_ratio: number
  mem_used: number
  mem_total: number
}

export interface CriticalDiskEntry {
  machine_id: string
  hostname: string
  mount: string
  used_pct: number
  free_bytes: number
}

export interface OverviewEvent {
  ts: string
  machine_id: string
  hostname: string
  kind: 'app_crash' | 'app_hang' | 'hw_warning' | 'smart_fail' | string
  process_name: string
  severity: string
}

export interface FleetOverview {
  generated_at: string
  totals: OverviewTotals
  health: OverviewHealth
  /** 0..100 */
  avg_cpu: number
  /** ratio 0..1 */
  avg_mem: number
  history: OverviewHistoryPoint[]
  top_cpu: TopCpuEntry[]
  critical_mem: CriticalMemEntry[]
  critical_disk: CriticalDiskEntry[]
  recent_events: OverviewEvent[]
  recent_alerts: AlertDTO[]
}

// ── Fleet-wide stats (GET /api/stats/…) ────────────────────────────────────────
export interface HourlyActivityCell {
  /** ISO day of week: 1 = Pazartesi … 7 = Pazar. Hours are Europe/Istanbul. */
  dow: number
  hour: number
  active_seconds: number
  samples: number
}

export interface HourlyActivityResponse {
  days: number
  cells: HourlyActivityCell[]
}

export type TopAppsRange = '24h' | '7d' | '30d'
export type TopAppsBy = 'cpu' | 'mem' | 'focus'

export interface TopAppEntry {
  name: string
  category: string
  cpu_avg: number
  cpu_max: number
  mem_avg: number
  machines: number
  focus_seconds: number
  active_hours: number
}

export interface TopAppsResponse {
  apps: TopAppEntry[]
}

// ── Slow machines ──────────────────────────────────────────────────────────────
export type SlowReasonKey =
  | 'mem_pressure' | 'low_ram' | 'disk_full' | 'weak_cpu' | 'smart' | 'crashes' | 'long_uptime'

export interface SlowReason {
  key: SlowReasonKey
  points: number
  /** The measurement that produced the points (ratio, %, GB, days or a count). */
  value: number
}

export interface SlowMachine {
  machine_id: string
  hostname: string
  logon_user: string
  online: boolean
  last_seen: string
  /** 0..100, higher = slower. */
  score: number
  severity: 'ok' | 'warn' | 'crit'
  reasons: SlowReason[]
  mem_total: number
  mem_ratio_avg: number
  mem_ratio_max: number
  cpu_avg: number
  cpu_cores?: number
  cpu_model?: string
  disk_mount?: string
  disk_used_pct: number
  disk_free_bytes: number
  uptime_days: number
  crashes: number
  smart_bad: boolean
  /** metric_1h buckets backing the averages — low values mean a thin sample. */
  sample_hours: number
}

export interface SlowMachinesResponse {
  days: number
  generated_at: string
  machines: SlowMachine[]
}

// ── Machine detail ─────────────────────────────────────────────────────────────
export interface MachineDetail extends FleetMachine {
  /** Set when the machine was retired from the panel (history is kept). */
  archived_at?: string
  archived_by?: string
  os?: string
  os_build?: string
  cpu_model?: string
  cpu_cores?: number
  /** Agent-reported own LAN IPv4 addresses; empty for agents that predate the field. */
  ip_addrs?: string[]
  /** Source address the server saw on the last batch — fallback when ip_addrs is empty. */
  last_ip?: string
  ip_updated_at?: string | null
  first_seen: string
  mem_used: number
  mem_total: number
  disks: DiskUsage[]
  disk_health: DiskHealthDTO[]
  /** ISO-8601 timestamp of the last successful Windows Update scan; null/absent = unknown. */
  updates_last_search?: string | null
  /** ISO-8601 timestamp of the last successful update install; null/absent = unknown. */
  updates_last_install?: string | null
}

export interface DiskUsage {
  mount: string
  used: number
  total: number
  free_pct: number
}

export interface DiskHealthDTO {
  disk: string
  smart_ok: boolean
  predict_fail: boolean
  updated_at: string
}

export type Resolution = 'raw' | '5m' | '1h'

export interface MetricSeries {
  resolution: Resolution
  cpu?: SeriesPoint[]
  mem?: SeriesPoint[]
  net?: NetPoint[]
}

export interface SeriesPoint {
  t: string
  avg: number
  max: number
}

export interface NetPoint {
  t: string
  sent: number
  recv: number
}

export interface DiskSeries {
  mount: string
  points: DiskPoint[]
}

export interface DiskPoint {
  t: string
  used: number
  total: number
}

export interface TopProcess {
  name: string
  category: string
  cpu_pct: number
  mem_bytes: number
}

export interface CategoryShare {
  category: string
  cpu_sum: number
  samples: number
  share: number // 0..1
}

// ── Users ──────────────────────────────────────────────────────────────────────
export interface UserSummary {
  logon_user: string
  last_seen: string
  current_machine?: string
  online: boolean
  /** Friendly name set by an admin; falls back to the logon name when empty. */
  display_name?: string
  /** Department / group tags. May be absent on older payloads → treat as []. */
  tags: string[]
}

export interface SessionItem {
  machine_id: string
  hostname: string
  kind: 'active' | 'logged_on' | string
  started_at: string
  ended_at: string | null
}

/** One continuous stretch a user was the console user of a machine. */
export interface UserLogon {
  machine_id: string
  hostname: string
  started_at: string
  /** null = still running. */
  ended_at: string | null
  active_seconds: number
}

// ── User activity (privacy: category + active-hours only; no titles/URLs) ───────
export interface HeatmapCell {
  /** 0..6, 0 = Monday (Pzt). */
  weekday: number
  /** 0..23 local hour. */
  hour: number
  active_minutes: number
}

export interface DailyActivity {
  date: string // "YYYY-MM-DD"
  active_minutes: number
  loggedon_minutes: number
}

export interface ActivityResponse {
  heatmap: HeatmapCell[]
  daily: DailyActivity[]
  total_active_minutes: number
  total_loggedon_minutes: number
}

/** One application's total foreground active-use time in a range.
 *  Privacy: `name` is the application base name only — never a window title / document / URL. */
export interface AppUsage {
  name: string
  seconds: number
}

/** Body of PUT /api/admin/users/{user}. */
export interface UserProfileUpdate {
  display_name: string
  tags: string[]
}

// ── Tags / departments ──────────────────────────────────────────────────────────
export interface TagInfo {
  tag: string
  user_count: number
}

export interface TagUserSummary {
  logon_user: string
  display_name?: string
  active_minutes: number
  top_category?: string
}

export interface TagSummary {
  tag: string
  users: TagUserSummary[]
  category_totals: CategoryShare[]
}

// ── Alerts / events ────────────────────────────────────────────────────────────
export interface AlertDTO {
  id: number
  machine_id: string
  hostname?: string
  rule: string
  severity: string
  status: string
  value?: number | null
  /** Legacy, already-worded sentence. Shown only when `params` is absent. */
  message: string
  /** Structured values for the rule's sentence template (lib/alertText.ts). */
  params?: Record<string, unknown> | null
  opened_at: string
  last_seen_at: string
  resolved_at?: string | null
  detail?: Record<string, unknown>
}

export interface EventDTO {
  ts: string
  machine_id: string
  kind: string
  process_name?: string
  severity?: string
  detail?: Record<string, unknown>
}

// ── Admin: classification + rules ──────────────────────────────────────────────
export interface CategoryRule {
  id: number
  category: string
  match_type: string
  match_value: string
  priority: number
  enabled: boolean
}

export interface UnclassifiedProcess {
  name: string
  count: number
}

export interface AlertRuleDTO {
  rule: string
  enabled: boolean
  warn_threshold: number | null
  crit_threshold: number | null
  for_seconds: number
}

export interface PanelUser {
  username: string
  display_name?: string | null
  role: string
  disabled?: boolean
  totp_enabled?: boolean
  created_at?: string | null
  last_login_at?: string | null
}

export interface NewPanelUser {
  username: string
  password: string
  role: string
  display_name?: string
}

export interface PanelUserPatch {
  role?: string
  disabled?: boolean
  display_name?: string
}

// ── Endpoint hygiene: app removal / startup entries ────────────────────────────
// Mirrors agent/internal/model (RemediationReport, RemediationPolicy) as exposed
// by the admin API. The catalog is the agent's COMPILED allowlist: the panel can
// only ever select from it, so every item_id here came from a shipped agent.

export type RemediationKind = 'app' | 'startup'
export type RemediationRiskTier = 'safe' | 'review' | 'advanced'
export type RemediationScope = 'user' | 'provisioned' | 'both'
export type RemediationAction = 'remove' | 'deprovision' | 'disable' | 'enable'
export type RemediationOutcome =
  | 'ok'
  | 'failed'
  | 'dry_run'
  | 'not_present'
  | 'not_allowed'
  | 'blocked'
  | 'timeout'
  | 'unknown_item'

/** One entry of the closed vocabulary, plus the fleet presence the server joined in. */
export interface RemediationCatalogItem {
  kind: RemediationKind
  item_id: string
  /** PackageFamilyName (apps) or the registry Run VALUE NAME (startup). */
  family: string
  display_name: string
  /** Turkish words in the agent's compiled catalog (games, media, productivity,
   *  communication, system, oem); mapped to a translated label by removalCategoryMeta. */
  category: string
  risk_tier: RemediationRiskTier
  scope?: RemediationScope
  safe_default: boolean
  process_names?: string[]
  notes?: string
  /** Retired from the vocabulary but still referenced by an old policy. */
  deprecated?: boolean
  /** Machines where an agent currently reports this item present/enabled.
   *  null = no agent has reported presence yet — render "bilinmiyor", never 0. */
  present_count?: number | null
  /** Machines that reported presence for this item at all (the denominator). */
  reported_count?: number | null
}

export interface RemediationCatalogGroups {
  app: RemediationCatalogItem[]
  startup: RemediationCatalogItem[]
}

/** Saved fleet-wide policy. `revision` is the server's opaque version stamp. */
export interface RemediationPolicy {
  revision: string
  enabled: boolean
  dry_run: boolean
  /** Machines carrying any of these tags are never touched. */
  excluded_tags: string[]
  /** Catalog item_ids. */
  apps: string[]
  startup: string[]
  updated_at?: string | null
  updated_by?: string | null
}

/** Body of PUT /api/admin/remediation/policy. */
export interface RemediationPolicyUpdate {
  enabled: boolean
  dry_run: boolean
  excluded_tags: string[]
  apps: string[]
  startup: string[]
}

/** Catalog-drift summary: agents running an older compiled vocabulary. */
export interface RemediationDrift {
  /** The server's own catalog version (hash of kind:item_id pairs). */
  server_version: string
  /** Machines whose agent reports a different version. */
  stale_machines: number
  /** Versions seen in the fleet, most common first. */
  versions?: { version: string; machines: number }[]
}

/** Fleet posture — the "why is nothing happening?" signals. */
export interface RemediationPosture {
  /** Machines that have reported any hygiene posture at all. */
  reporting_machines: number
  /** Machines the current policy would reach (fleet minus excluded tags). */
  targeted_machines: number
  /** Machines that already applied the saved policy revision. */
  applied_machines: number
  /** Machines where the GPO kill switch denies enforcement (allowed.enforce=false). */
  kill_switch_off: number
  /** Kill switch detail, when the server breaks it down. */
  apps_not_allowed: number
  startup_not_allowed: number
  /** True once at least one dry-run attempt has been recorded. Gates enforce mode. */
  dry_run_observed: boolean
  last_dry_run_at?: string | null
  last_attempt_at?: string | null
  drift: RemediationDrift
}

/** GET /api/admin/remediation. */
export interface RemediationOverview {
  generated_at: string
  catalog_version: string
  catalog: RemediationCatalogGroups
  policy: RemediationPolicy
  posture: RemediationPosture
}

/** One audited action. `error_code` is a CODE only — never an OS message or path. */
export interface RemediationAttemptDTO {
  ts: string
  machine_id: string
  hostname?: string
  kind: RemediationKind
  item_id: string
  /** Catalog label (Turkish); absent when the id has left the catalog. */
  display_name?: string
  action: RemediationAction | string
  outcome: RemediationOutcome | string
  error_code?: string
  duration_ms?: number
  /** failed/timeout/blocked rows only: a later attempt converged, or the machine
   *  now reports the item absent. Absent on older servers. */
  resolved?: boolean
}

/** One (item, outcome, error code) bucket of still-open failures. */
export interface RemediationFailureGroup {
  kind: RemediationKind
  item_id: string
  display_name?: string
  outcome: RemediationOutcome | string
  error_code?: string
  machines: number
  hostnames: string[]
  /** How many of the group's machines run an agent behind the fleet's latest.
   *  Absent on older servers. */
  stale_agents?: number
  last_ts: string
}

/** GET /api/admin/remediation/failures. */
export interface RemediationFailuresResponse {
  open: RemediationFailureGroup[]
  open_machine_items: number
  resolved_machine_items: number
  /** Highest agent version any active machine reports. Absent on older servers. */
  latest_agent_version?: string
}

/** Active-hour usage before/after (cpu on 0..100, mem as a 0..1 ratio). */
export interface RemediationImpactWindow {
  cpu_before: number | null
  cpu_after: number | null
  mem_before: number | null
  mem_after: number | null
  hours_before: number
  hours_after: number
}

export interface RemediationImpactMachine extends RemediationImpactWindow {
  machine_id: string
  hostname?: string
  first_removal: string
  last_removal: string
  items_removed: number
  complete: boolean
}

export interface RemediationImpactItem {
  item_id: string
  display_name?: string
  machines: number
  proc_machines_before: number
  proc_cpu_before: number | null
  proc_mem_before: number | null
  proc_machines_after: number
}

/** GET /api/admin/remediation/impact?days=. */
export interface RemediationImpactResponse {
  days: number
  generated_at: string
  min_hours: number
  fleet: RemediationImpactWindow
  fleet_machines: number
  control: RemediationImpactWindow
  control_machines: number
  machines: RemediationImpactMachine[]
  items: RemediationImpactItem[]
}

/** Per-item outcome roll-up over the queried window. */
export interface RemediationRollup {
  kind: RemediationKind
  item_id: string
  machines: number
  /** outcome → count. */
  outcomes: Partial<Record<RemediationOutcome, number>> & Record<string, number | undefined>
  last_ts?: string
}

/** GET /api/admin/remediation/attempts. */
export interface RemediationAttemptsResponse {
  attempts: RemediationAttemptDTO[]
  /** Optional server-side roll-up; the panel computes it locally when absent. */
  rollup?: RemediationRollup[]
  generated_at?: string
}

/** One machine's hygiene state (GET /api/machines/{id}/remediation). */
export interface MachineRemediationItem {
  kind: RemediationKind
  item_id: string
  display_name?: string
  risk_tier?: RemediationRiskTier
  present: boolean
  scope?: RemediationScope
  /** startup only: whether the entry currently runs at logon. */
  enabled?: boolean | null
  /** Selected by the current policy. */
  selected?: boolean
  first_seen?: string
  last_seen?: string
  last_present_at?: string | null
  removed_at?: string | null
}

export interface MachineRemediation {
  machine_id: string
  /** What the AGENT reported. */
  catalog_version?: string
  /** What this server ships. */
  server_catalog_version: string
  /** The two disagree — the agent binary is out of date. */
  catalog_stale: boolean
  policy_revision?: string
  /** The local GPO kill switch permits actuation. The server collapses the
   *  agent's three flags (apps/startup/enforce) into this single boolean. */
  allowed: boolean
  reported_at?: string | null
  items: MachineRemediationItem[]
  attempts: RemediationAttemptDTO[]
}

// ── WebSocket envelope ─────────────────────────────────────────────────────────
export interface MachineMetricPush {
  machine_id: string
  t: string
  cpu_pct: number
  mem_ratio: number
}

export type WSEnvelope =
  | { type: 'fleet.update'; payload: { machines: FleetMachine[] } }
  | { type: 'machine.metric'; payload: MachineMetricPush }
  | { type: 'alert.opened'; payload: AlertDTO }
  | { type: 'alert.resolved'; payload: { id: number } }

// ── Machine archive (retiring machines) ────────────────────────────────────────
export interface ArchivedMachine {
  machine_id: string
  hostname: string
  kind: string
  os?: string
  agent_version?: string
  first_seen: string
  last_seen?: string
  archived_at: string
  archived_by?: string
}

export interface ArchivedMachinesResponse {
  machines: ArchivedMachine[]
}

export interface MachineArchiveResult {
  changed: number
}
