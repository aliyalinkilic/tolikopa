import { useCallback } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useLang } from '@/i18n'
import { api } from '@/lib/api'
import type { QueryParams } from '@/lib/api'
import { localizeOverview, normalizeOverview } from '@/lib/remediation'
import type { RemediationOverviewWire } from '@/lib/remediation'
import type {
  ActivityResponse, AlertDTO, AlertRuleDTO, AppUsage, CategoryRule, CategoryShare, DiskSeries,
  EventDTO, FleetMachine, FleetOverview, FleetSummary, HourlyActivityResponse,
  GeneralSettings, MachineDetail, MachineRemediation, MetricSeries, NetPoint, NewPanelUser, PanelUser, PanelUserPatch,
  RecoveryCodes, SetupConfig, SetupState, TotpSetup,
  ArchivedMachinesResponse, MachineArchiveResult, RemediationOverview,
  RemediationAttemptsResponse, RemediationFailuresResponse, RemediationImpactResponse, RemediationPolicy, RemediationPolicyUpdate, SessionItem,
  SlowMachinesResponse, TagInfo, TagSummary, TopAppsBy, TopAppsRange, TopAppsResponse, TopProcess,
  UnclassifiedProcess, UserLogon, UserProfileUpdate, UserSummary,
} from '@/lib/types'

const LIVE_REFETCH = 20_000

// ── Fleet ──────────────────────────────────────────────────────────────────────
export interface FleetFilters {
  status?: 'online' | 'offline'
  user?: string
  q?: string
  tag?: string
}

export function useFleet(filters: FleetFilters = {}) {
  return useQuery({
    queryKey: ['fleet', filters],
    queryFn: () => api.get<FleetMachine[]>('/api/fleet', filters as QueryParams),
    refetchInterval: LIVE_REFETCH,
  })
}

export function useFleetSummary() {
  return useQuery({
    queryKey: ['fleet-summary'],
    queryFn: () => api.get<FleetSummary>('/api/fleet/summary'),
    refetchInterval: LIVE_REFETCH,
  })
}

/** Panel homepage aggregate. Keeps polling even while the endpoint 404s
 *  (pre-deploy backend) so the page fills in as soon as the API lands. */
export function useFleetOverview() {
  return useQuery({
    queryKey: ['fleet-overview'],
    queryFn: () => api.get<FleetOverview>('/api/fleet/overview'),
    refetchInterval: 15_000,
  })
}

// ── Fleet-wide stats ───────────────────────────────────────────────────────────
const STATS_REFETCH = 5 * 60_000

export function useHourlyActivity(days = 7) {
  return useQuery({
    queryKey: ['stats', 'hourly-activity', days],
    queryFn: () => api.get<HourlyActivityResponse>('/api/stats/hourly-activity', { days }),
    refetchInterval: STATS_REFETCH,
  })
}

export function useTopApps(range: TopAppsRange = '24h', by: TopAppsBy = 'cpu') {
  return useQuery({
    queryKey: ['stats', 'top-apps', range, by],
    queryFn: () => api.get<TopAppsResponse>('/api/stats/top-apps', { range, by }),
    refetchInterval: STATS_REFETCH,
  })
}

/** Slowness ranking. Retries are disabled on 404 by the shared client config, so
 *  a panel deployed ahead of the backend degrades to the client-side fallback
 *  (lib/slowness.ts) instead of hammering a missing endpoint. */
export function useSlowMachines(days = 7, limit = 20) {
  return useQuery({
    queryKey: ['stats', 'slow-machines', days, limit],
    queryFn: () => api.get<SlowMachinesResponse>('/api/stats/slow-machines', { days, limit }),
    refetchInterval: STATS_REFETCH,
  })
}

// ── Machine detail ─────────────────────────────────────────────────────────────
export function useMachine(id: string | undefined) {
  return useQuery({
    queryKey: ['machine', id],
    queryFn: () => api.get<MachineDetail>(`/api/machines/${id}`),
    enabled: !!id,
    refetchInterval: LIVE_REFETCH,
  })
}

/**
 * Sets a machine's group tags (admin). Sends BOTH labels and tags so the labels
 * blob is never clobbered — pass the machine's current labels (or {} if unknown).
 */
export function useUpdateMachineTags() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, labels, tags }: { id: string; labels: Record<string, unknown>; tags: string[] }) =>
      api.put<void>(`/api/admin/machines/${encodeURIComponent(id)}/labels`, { labels, tags }),
    onSuccess: (_data, { id }) => {
      qc.invalidateQueries({ queryKey: ['fleet'] })
      qc.invalidateQueries({ queryKey: ['machine', id] })
    },
  })
}

type TimeWindow = {
  from: string
  to: string
}

export function useMachineMetrics(
  id: string | undefined,
  win: TimeWindow,
  res: string,
  metrics = 'cpu,mem,net',
) {
  return useQuery({
    queryKey: ['machine', id, 'metrics', win.from, win.to, res, metrics],
    queryFn: () =>
      api.get<MetricSeries>(`/api/machines/${id}/metrics`, { ...win, res, metrics }),
    enabled: !!id,
  })
}

export function useMachineDisks(id: string | undefined, win: TimeWindow) {
  return useQuery({
    queryKey: ['machine', id, 'disks', win.from, win.to],
    queryFn: () => api.get<DiskSeries[]>(`/api/machines/${id}/disks`, win),
    enabled: !!id,
  })
}

export function useTopProcesses(
  id: string | undefined,
  win: TimeWindow,
  by: 'cpu' | 'mem' = 'cpu',
  limit = 12,
) {
  return useQuery({
    queryKey: ['machine', id, 'top-processes', win.from, win.to, by, limit],
    queryFn: () =>
      api.get<TopProcess[]>(`/api/machines/${id}/top-processes`, { ...win, by, limit }),
    enabled: !!id,
  })
}

export function useMachineCategories(id: string | undefined, win: TimeWindow) {
  return useQuery({
    queryKey: ['machine', id, 'categories', win.from, win.to],
    queryFn: () => api.get<CategoryShare[]>(`/api/machines/${id}/categories`, win),
    enabled: !!id,
  })
}

export function useMachineSessions(id: string | undefined, win: TimeWindow) {
  return useQuery({
    queryKey: ['machine', id, 'sessions', win.from, win.to],
    queryFn: () => api.get<SessionItem[]>(`/api/machines/${id}/sessions`, win),
    enabled: !!id,
  })
}

export function useMachineEvents(id: string | undefined, win: TimeWindow, kind?: string) {
  return useQuery({
    queryKey: ['machine', id, 'events', win.from, win.to, kind ?? 'all'],
    queryFn: () => api.get<EventDTO[]>(`/api/machines/${id}/events`, { ...win, kind }),
    enabled: !!id,
  })
}

export function useMachineAlerts(id: string | undefined, status?: string) {
  return useQuery({
    queryKey: ['machine', id, 'alerts', status ?? 'all'],
    queryFn: () => api.get<AlertDTO[]>(`/api/machines/${id}/alerts`, { status }),
    enabled: !!id,
  })
}

// ── Users ──────────────────────────────────────────────────────────────────────
export function useUsers(tag?: string) {
  return useQuery({
    queryKey: ['users', tag ?? 'all'],
    queryFn: () => api.get<UserSummary[]>('/api/users', { tag }),
    refetchInterval: LIVE_REFETCH,
  })
}

function userPath(user: string, suffix: string): string {
  return `/api/users/${encodeURIComponent(user)}/${suffix}`
}

export function useUserActivity(user: string | undefined, win: TimeWindow) {
  return useQuery({
    queryKey: ['user', user, 'activity', win.from, win.to],
    queryFn: () => api.get<ActivityResponse>(userPath(user!, 'activity'), win),
    enabled: !!user,
  })
}

export function useUpdateUserProfile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ user, profile }: { user: string; profile: UserProfileUpdate }) =>
      api.put<UserSummary>(`/api/admin/users/${encodeURIComponent(user)}`, profile),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['users'] })
      qc.invalidateQueries({ queryKey: ['admin', 'tags'] })
    },
  })
}

// ── Tags / departments ──────────────────────────────────────────────────────────
export function useTags() {
  return useQuery({
    queryKey: ['admin', 'tags'],
    queryFn: () => api.get<TagInfo[]>('/api/admin/tags'),
  })
}

export function useTagSummary(tag: string | undefined, win: TimeWindow) {
  return useQuery({
    queryKey: ['tag', tag, 'summary', win.from, win.to],
    queryFn: () => api.get<TagSummary>(`/api/tags/${encodeURIComponent(tag!)}/summary`, win),
    enabled: !!tag,
  })
}

export function useUserTimeline(user: string | undefined, win: TimeWindow) {
  return useQuery({
    queryKey: ['user', user, 'timeline', win.from, win.to],
    queryFn: () => api.get<SessionItem[]>(userPath(user!, 'timeline'), win),
    enabled: !!user,
  })
}

/** Logon records over the server's full look-back (90 days) — deliberately not range-bound. */
export function useUserLogons(user: string | undefined) {
  return useQuery({
    queryKey: ['user', user, 'logons'],
    queryFn: () => api.get<UserLogon[]>(userPath(user!, 'logons')),
    enabled: !!user,
    staleTime: 60_000,
  })
}

export function useUserNetwork(user: string | undefined, win: TimeWindow) {
  return useQuery({
    queryKey: ['user', user, 'network', win.from, win.to],
    queryFn: () => api.get<NetPoint[]>(userPath(user!, 'network'), { ...win, res: '1h' }),
    enabled: !!user,
  })
}

export function useUserCategories(user: string | undefined, win: TimeWindow) {
  return useQuery({
    queryKey: ['user', user, 'categories', win.from, win.to],
    queryFn: () => api.get<CategoryShare[]>(userPath(user!, 'categories'), win),
    enabled: !!user,
  })
}

export function useUserApps(user: string | undefined, win: TimeWindow) {
  return useQuery({
    queryKey: ['user', user, 'apps', win.from, win.to],
    queryFn: () => api.get<AppUsage[]>(userPath(user!, 'apps'), win),
    enabled: !!user,
  })
}

// ── Alerts ─────────────────────────────────────────────────────────────────────
export interface AlertFilters {
  status?: string
  severity?: string
  machine?: string
  from?: string
  to?: string
}

export function useAlerts(filters: AlertFilters = {}) {
  return useQuery({
    queryKey: ['alerts', filters],
    queryFn: () => api.get<AlertDTO[]>('/api/alerts', filters as QueryParams),
    refetchInterval: LIVE_REFETCH,
  })
}

export function useAckAlert() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api.post<void>(`/api/alerts/${id}/ack`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['alerts'] })
      qc.invalidateQueries({ queryKey: ['fleet'] })
      qc.invalidateQueries({ queryKey: ['fleet-summary'] })
    },
  })
}

// ── Admin: categories ───────────────────────────────────────────────────────────
export function useCategories() {
  return useQuery({
    queryKey: ['admin', 'categories'],
    queryFn: () => api.get<CategoryRule[]>('/api/admin/categories'),
  })
}

export function useCreateCategory() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (rule: Omit<CategoryRule, 'id'>) => api.post<CategoryRule>('/api/admin/categories', rule),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'categories'] }),
  })
}

export function useUpdateCategory() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (rule: CategoryRule) => api.put<CategoryRule>('/api/admin/categories', rule),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'categories'] }),
  })
}

export function useDeleteCategory() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api.del<void>('/api/admin/categories', { id }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'categories'] }),
  })
}

export function useUnclassified(win: TimeWindow) {
  return useQuery({
    queryKey: ['admin', 'unclassified', win.from, win.to],
    queryFn: () => api.get<UnclassifiedProcess[]>('/api/admin/unclassified', win),
  })
}

export function useReclassify() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (window: string) => api.post<void>('/api/admin/reclassify', { window }),
    // Narrow: reclassification only moves processes between categories. A blanket
    // ['admin'] invalidation would also refetch — and visually clobber — unsaved
    // drafts on the other settings tabs.
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin', 'categories'] })
      qc.invalidateQueries({ queryKey: ['admin', 'unclassified'] })
    },
  })
}

// ── Admin: alert rules ──────────────────────────────────────────────────────────
export function useAlertRules() {
  return useQuery({
    queryKey: ['admin', 'alert-rules'],
    queryFn: () => api.get<AlertRuleDTO[]>('/api/admin/alert-rules'),
  })
}

export function useUpdateAlertRules() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (rules: AlertRuleDTO[]) => api.put<AlertRuleDTO[]>('/api/admin/alert-rules', rules),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'alert-rules'] }),
  })
}

// ── Admin: panel users + ingest token ─────────────────────────────────────────
const PANEL_USERS = '/api/admin/panel-users'
const panelUserPath = (username: string, suffix = '') => `${PANEL_USERS}/${encodeURIComponent(username)}${suffix}`

export function usePanelUsers() {
  return useQuery({
    queryKey: ['admin', 'panel-users'],
    queryFn: () => api.get<PanelUser[]>(PANEL_USERS),
  })
}

/** Every panel-user write invalidates the list; the caller owns the toast. */
function usePanelUserWrite<V>(fn: (vars: V) => Promise<unknown>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'panel-users'] }),
  })
}

export function useCreatePanelUser() {
  return usePanelUserWrite((u: NewPanelUser) => api.post<PanelUser>(PANEL_USERS, u))
}

export function useUpdatePanelUser() {
  return usePanelUserWrite(({ username, patch }: { username: string; patch: PanelUserPatch }) =>
    api.patch<PanelUser>(panelUserPath(username), patch),
  )
}

export function useResetPanelUserPassword() {
  return usePanelUserWrite(({ username, new_password }: { username: string; new_password: string }) =>
    api.post<void>(panelUserPath(username, '/reset-password'), { new_password }),
  )
}

export function useResetPanelUser2fa() {
  return usePanelUserWrite((username: string) => api.post<void>(panelUserPath(username, '/reset-2fa')))
}

export function useDeletePanelUser() {
  return usePanelUserWrite((username: string) => api.del<void>(panelUserPath(username)))
}

// ── Admin: general (server-wide) settings ─────────────────────────────────────
export function useGeneralSettings(enabled = true) {
  return useQuery({
    queryKey: ['admin', 'general-settings'],
    queryFn: () => api.get<GeneralSettings>('/api/admin/settings/general'),
    enabled,
  })
}

export function useUpdateGeneralSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (s: GeneralSettings) => api.put<GeneralSettings | void>('/api/admin/settings/general', s),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin', 'general-settings'] })
      // org name / default language / policy are mirrored in the public settings.
      qc.invalidateQueries({ queryKey: ['public-settings'] })
    },
  })
}

// ── Setup (first-run wizard + Setup page) ─────────────────────────────────────
const SETUP_KEY = ['admin', 'setup'] as const

/** Config, readiness, agent build and fleet-by-version. `poll` (ms) is used while
 *  the wizard waits for the first PC. */
export function useSetupState(options: { enabled?: boolean; poll?: number | false } = {}) {
  return useQuery({
    queryKey: SETUP_KEY,
    queryFn: () => api.get<SetupState>('/api/admin/setup'),
    enabled: options.enabled ?? true,
    refetchInterval: options.poll || false,
    retry: false,
  })
}

/** Partial update; the server answers with the full config. */
export function useSaveSetupConfig() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (config: Partial<SetupConfig>) => api.put<SetupConfig>('/api/admin/setup/config', { config }),
    onSuccess: () => qc.invalidateQueries({ queryKey: SETUP_KEY }),
  })
}

export function useCompleteSetup() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api.post<{ completed_at: string }>('/api/admin/setup/complete'),
    onSuccess: (res) => {
      // Patch the cache first so the banner / wizard react before the refetch lands.
      qc.setQueryData<SetupState>(SETUP_KEY, (d) => (d ? { ...d, completed_at: res?.completed_at ?? new Date().toISOString() } : d))
      qc.invalidateQueries({ queryKey: SETUP_KEY })
    },
  })
}

export function useReopenSetup() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api.post<void>('/api/admin/setup/reopen'),
    onSuccess: () => {
      qc.setQueryData<SetupState>(SETUP_KEY, (d) => (d ? { ...d, completed_at: null } : d))
      qc.invalidateQueries({ queryKey: SETUP_KEY })
    },
  })
}

// ── Own account ───────────────────────────────────────────────────────────────
// These change what GET /api/auth/me returns; callers refresh the auth context.
export function useUpdateProfile() {
  return useMutation({
    mutationFn: (display_name: string) => api.put<void>('/api/me/profile', { display_name }),
  })
}

export function useChangePassword() {
  return useMutation({
    mutationFn: (body: { current_password: string; new_password: string }) =>
      api.put<void>('/api/me/password', body),
  })
}

export function useTotpSetup() {
  return useMutation({ mutationFn: (password: string) => api.post<TotpSetup>('/api/me/2fa/setup', { password }) })
}

export function useTotpEnable() {
  return useMutation({ mutationFn: (code: string) => api.post<RecoveryCodes>('/api/me/2fa/enable', { code }) })
}

export function useTotpDisable() {
  return useMutation({
    mutationFn: (body: { password: string; code: string }) => api.post<void>('/api/me/2fa/disable', body),
  })
}

export function useRegenerateRecoveryCodes() {
  return useMutation({
    mutationFn: (body: { password: string; code: string }) =>
      api.post<RecoveryCodes>('/api/me/2fa/recovery-codes', body),
  })
}

// ── Admin: endpoint hygiene (app removal) ─────────────────────────────────────
// Everything lives under ['admin','remediation',…] so a save invalidates exactly
// this feature and nothing else on the settings page.

const REMEDIATION_REFETCH = 60_000

/** Catalog + saved policy + fleet posture. The payload is normalized so a backend
 *  that omits posture fields degrades to "unknown" instead of a fake zero. */
export function useRemediationOverview() {
  const lang = useLang()
  // The agent's catalog text is Turkish; `select` swaps in the client-side
  // translation. Its identity changes with the language, so the result follows a
  // language switch without refetching.
  const select = useCallback((data: RemediationOverview) => localizeOverview(data), [lang]) // eslint-disable-line react-hooks/exhaustive-deps
  return useQuery({
    queryKey: ['admin', 'remediation', 'overview'],
    queryFn: async () =>
      normalizeOverview(await api.get<RemediationOverviewWire>('/api/admin/remediation')),
    refetchInterval: REMEDIATION_REFETCH,
    select,
  })
}

export interface RemediationAttemptFilters {
  limit?: number
  outcome?: string
  item_id?: string
}

export function useRemediationAttempts(filters: RemediationAttemptFilters = {}) {
  return useQuery({
    queryKey: ['admin', 'remediation', 'attempts', filters],
    queryFn: () =>
      api.get<RemediationAttemptsResponse>('/api/admin/remediation/attempts', filters as QueryParams),
    refetchInterval: REMEDIATION_REFETCH,
  })
}

/** Still-open failures, grouped by item + error code. */
export function useRemediationFailures() {
  return useQuery({
    queryKey: ['admin', 'remediation', 'failures'],
    queryFn: () => api.get<RemediationFailuresResponse>('/api/admin/remediation/failures'),
    refetchInterval: REMEDIATION_REFETCH,
  })
}

/** Usage before/after successful removals. Hourly aggregates — no need to poll
 *  faster than they refresh. */
export function useRemediationImpact(days: number) {
  return useQuery({
    queryKey: ['admin', 'remediation', 'impact', days],
    queryFn: () => api.get<RemediationImpactResponse>('/api/admin/remediation/impact', { days }),
    refetchInterval: 15 * 60_000,
  })
}

/** Saves the fleet-wide policy. The mutation owns invalidation; the caller owns
 *  the toast (see useCreatePanelUser for the same split). */
export function useUpdateRemediationPolicy() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (policy: RemediationPolicyUpdate) =>
      api.put<RemediationPolicy>('/api/admin/remediation/policy', policy),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin', 'remediation'] })
    },
  })
}

/** One machine's hygiene state (presence + local kill switch + its attempts). */
export function useMachineRemediation(id: string | undefined) {
  return useQuery({
    queryKey: ['machine', id, 'remediation'],
    queryFn: () => api.get<MachineRemediation>(`/api/machines/${encodeURIComponent(id!)}/remediation`),
    enabled: !!id,
  })
}

// ── Admin: machine archive ────────────────────────────────────────────────────
// Archiving changes what every current-state view shows, so a write invalidates
// the fleet, the overview, the machine page and the archive list together.

export function useArchivedMachines() {
  return useQuery({
    queryKey: ['admin', 'machines', 'archived'],
    queryFn: () => api.get<ArchivedMachinesResponse>('/api/admin/machines/archived'),
  })
}

function useArchiveWrite(path: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (machineIds: string[]) =>
      api.post<MachineArchiveResult>(path, { machine_ids: machineIds }),
    onSuccess: (_res, ids) => {
      qc.invalidateQueries({ queryKey: ['admin', 'machines', 'archived'] })
      qc.invalidateQueries({ queryKey: ['fleet'] })
      qc.invalidateQueries({ queryKey: ['fleet-summary'] })
      qc.invalidateQueries({ queryKey: ['fleet-overview'] })
      qc.invalidateQueries({ queryKey: ['alerts'] })
      qc.invalidateQueries({ queryKey: ['admin', 'remediation'] })
      for (const id of ids) qc.invalidateQueries({ queryKey: ['machine', id] })
    },
  })
}

export function useArchiveMachines() {
  return useArchiveWrite('/api/admin/machines/archive')
}

export function useRestoreMachines() {
  return useArchiveWrite('/api/admin/machines/restore')
}
