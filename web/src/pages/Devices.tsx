import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { motion } from 'motion/react'
import { Activity, BatteryWarning, BellRing, Download, HardDrive, MemoryStick, Monitor, Tag, Wifi, X } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { PageHeader } from '@/components/AppShell'
import { FilterChip } from '@/components/FilterChip'
import { KpiCard } from '@/components/KpiCard'
import { MachineCard } from '@/components/MachineCard'
import { SkeletonCard } from '@/components/Skeleton'
import { EmptyState } from '@/components/EmptyState'
import { useFleet, useFleetSummary } from '@/hooks/queries'
import { useLiveFleet } from '@/hooks/useLiveFleet'
import { staggerContainer } from '@/lib/motion'
import { formatPercent, displayUser } from '@/lib/format'
import { cn } from '@/lib/cn'
import type { FleetMachine } from '@/lib/types'
import { useT } from '@/i18n'
import { compareText, lowerText } from '@/lib/format'
import type { TKey } from '@/i18n'

type StatusFilter = 'all' | 'online' | 'offline'

const STATUS_TABS: { key: StatusFilter; labelKey: TKey }[] = [
  { key: 'all', labelKey: 'common.all' },
  { key: 'online', labelKey: 'common.online' },
  { key: 'offline', labelKey: 'common.offline' },
]

function matchesQuery(m: FleetMachine, q: string): boolean {
  const needle = lowerText(q)
  return (
    lowerText(m.hostname).includes(needle) ||
    lowerText(displayUser(m.logon_user)).includes(needle) ||
    lowerText(m.logon_user).includes(needle)
  )
}

/** A machine "needs update" if a reboot is pending or Windows knows of pending updates. */
function machineNeedsUpdate(m: FleetMachine): boolean {
  return m.reboot_pending === true || (m.pending_updates ?? 0) > 0
}

/** A laptop has a "weak battery" when its full-charge/design health has dropped below 80%. */
function machineWeakBattery(m: FleetMachine): boolean {
  return m.battery?.present === true && (m.battery.health_pct ?? 100) < 80
}

export function Devices() {
  const t = useT()
  const [params, setParams] = useSearchParams()
  const q = params.get('q') ?? ''
  const status = (params.get('status') as StatusFilter | null) ?? 'all'
  const tag = params.get('tag') ?? undefined
  const untagged = params.get('untagged') === '1'
  const needsUpdate = params.get('needs_update') === '1'
  const weakBatt = params.get('weak_batt') === '1'
  const memMin = Number(params.get('mem_min') ?? '0') || 0
  const diskMin = Number(params.get('disk_min') ?? '0') || 0

  const { data: summary } = useFleetSummary()
  // Unfiltered fleet powers the tag chip universe; when no tag is active this
  // dedupes to the same ['fleet', {}] query as the grid below (single poll).
  const { data: allMachines } = useFleet()
  // "Untagged" is not a server-side tag filter, so fetch all and filter client-side.
  const { data: machines, isLoading } = useFleet(tag && !untagged ? { tag } : {})
  const { pulses } = useLiveFleet()

  const setStatus = (s: StatusFilter) => {
    const next = new URLSearchParams(params)
    if (s === 'all') next.delete('status')
    else next.set('status', s)
    setParams(next, { replace: true })
  }
  const clearQuery = () => {
    const next = new URLSearchParams(params)
    next.delete('q')
    setParams(next, { replace: true })
  }
  const setTag = (x: string | undefined) => {
    const next = new URLSearchParams(params)
    next.delete('untagged') // tag and untagged are mutually exclusive
    if (!x) next.delete('tag')
    else next.set('tag', x)
    setParams(next, { replace: true })
  }
  const setUntagged = (on: boolean) => {
    const next = new URLSearchParams(params)
    next.delete('tag')
    if (on) next.set('untagged', '1')
    else next.delete('untagged')
    setParams(next, { replace: true })
  }
  const setNeedsUpdate = (on: boolean) => {
    const next = new URLSearchParams(params)
    if (on) next.set('needs_update', '1')
    else next.delete('needs_update')
    setParams(next, { replace: true })
  }
  const setWeakBatt = (on: boolean) => {
    const next = new URLSearchParams(params)
    if (on) next.set('weak_batt', '1')
    else next.delete('weak_batt')
    setParams(next, { replace: true })
  }
  const setThreshold = (key: 'mem_min' | 'disk_min', value: number) => {
    const next = new URLSearchParams(params)
    if (value > 0) next.set(key, String(value))
    else next.delete(key)
    setParams(next, { replace: true })
  }

  const fleetTags = useMemo(() => {
    const counts = new Map<string, number>()
    for (const m of allMachines ?? []) {
      for (const x of m.tags ?? []) counts.set(x, (counts.get(x) ?? 0) + 1)
    }
    return [...counts.entries()]
      .map(([name, count]) => ({ name, count }))
      .sort((a, b) => compareText(a.name, b.name))
  }, [allMachines])

  const needsUpdateCount = useMemo(
    () => (machines ?? []).filter(machineNeedsUpdate).length,
    [machines],
  )

  const weakBattCount = useMemo(
    () => (machines ?? []).filter(machineWeakBattery).length,
    [machines],
  )

  const untaggedCount = useMemo(
    () => (allMachines ?? []).filter((m) => (m.tags ?? []).length === 0).length,
    [allMachines],
  )

  const visible = useMemo(() => {
    if (!machines) return []
    const filtered = machines.filter((m) => {
      if (status === 'online' && !m.online) return false
      if (status === 'offline' && m.online) return false
      if (untagged && (m.tags ?? []).length > 0) return false
      if (needsUpdate && !machineNeedsUpdate(m)) return false
      if (weakBatt && !machineWeakBattery(m)) return false
      if (memMin > 0 && (m.mem_ratio ?? 0) * 100 < memMin) return false
      if (diskMin > 0 && 100 - (m.disk_min_free_pct ?? 100) < diskMin) return false
      if (q && !matchesQuery(m, q)) return false
      return true
    })
    return filtered.sort((a, b) => {
      if (a.online !== b.online) return a.online ? -1 : 1
      if (b.open_alerts !== a.open_alerts) return b.open_alerts - a.open_alerts
      return compareText(a.hostname, b.hostname)
    })
  }, [machines, status, q, untagged, needsUpdate, weakBatt, memMin, diskMin])

  const alertTotal = summary ? Object.values(summary.alerts_by_severity ?? {}).reduce((a, b) => a + b, 0) : 0
  const critCount = summary?.alerts_by_severity?.critical ?? summary?.alerts_by_severity?.crit ?? 0
  const alertTone = critCount > 0 ? 'crit' : alertTotal > 0 ? 'warn' : 'ok'

  return (
    <div>
      <PageHeader
        title={t('nav.devices')}
        description={t('devices.subtitle')}
        action={
          <div role="group" aria-label={t('devices.statusFilter')} className="inline-flex rounded-[var(--radius-md)] bg-bg-elev/70 p-1">
            {STATUS_TABS.map((x) => (
              <button
                key={x.key}
                type="button"
                onClick={() => setStatus(x.key)}
                aria-pressed={status === x.key}
                className={cn(
                  'rounded-[9px] px-3 py-1.5 text-[13px] font-medium transition-colors ring-focus',
                  status === x.key ? 'bg-surface-2 text-text' : 'text-text-muted hover:text-text',
                )}
              >
                {t(x.labelKey)}
              </button>
            ))}
          </div>
        }
      />

      {/* KPIs */}
      <motion.div
        variants={staggerContainer()}
        initial="hidden"
        animate="show"
        className="grid grid-cols-2 gap-4 xl:grid-cols-4"
      >
        <KpiCard
          label={t('devices.kpiTotal')}
          value={summary?.total ?? 0}
          icon={Monitor}
          tone="accent"
          footer={summary ? t('common.offlineCount', { count: summary.offline }) : undefined}
        />
        <KpiCard
          label={t('common.online')}
          value={summary?.online ?? 0}
          icon={Wifi}
          tone="ok"
          footer={summary ? t('devices.avgCpu', { value: formatPercent(summary.avg_cpu) }) : undefined}
        />
        <KpiCard
          label={t('devices.kpiActive')}
          value={summary?.active ?? 0}
          icon={Activity}
          tone="info"
          footer={summary ? t('devices.avgMem', { value: formatPercent(summary.avg_mem) }) : undefined}
        />
        <KpiCard
          label={t('devices.kpiAlerts')}
          value={alertTotal}
          icon={BellRing}
          tone={alertTone}
          footer={critCount > 0 ? t('common.criticalCount', { count: critCount }) : alertTotal > 0 ? t('common.review') : t('common.noIssues')}
        />
      </motion.div>

      {/* Active query chip */}
      {q && (
        <div className="mt-5 flex items-center gap-2 text-[13px] text-text-muted">
          <span>{t('devices.searchChip')}</span>
          <span className="inline-flex items-center gap-1.5 rounded-full border border-border-2 bg-surface/60 py-1 pl-3 pr-1.5 text-text">
            “{q}”
            <button
              type="button"
              onClick={clearQuery}
              aria-label={t('devices.clearSearch')}
              className="grid size-5 place-items-center rounded-full text-text-dim hover:bg-wash-2 hover:text-text ring-focus"
            >
              <X className="size-3" aria-hidden />
            </button>
          </span>
        </div>
      )}

      {/* Tag filter */}
      {fleetTags.length > 0 && (
        <div className="mt-5 flex items-center gap-2 overflow-x-auto pb-1" role="group" aria-label={t('devices.tagFilter')}>
          <Tag className="size-4 shrink-0 text-text-dim" aria-hidden />
          <FilterChip active={tag === undefined && !untagged} onClick={() => setTag(undefined)}>
            {t('common.all')}
          </FilterChip>
          <FilterChip active={untagged} onClick={() => setUntagged(!untagged)} count={untaggedCount}>
            {t('common.untagged')}
          </FilterChip>
          {fleetTags.map((x) => (
            <FilterChip key={x.name} active={tag === x.name && !untagged} onClick={() => setTag(x.name)} count={x.count}>
              {x.name}
            </FilterChip>
          ))}
        </div>
      )}

      {/* Update + battery health filters */}
      {(needsUpdateCount > 0 || needsUpdate || weakBattCount > 0 || weakBatt) && (
        <div className="mt-5 flex flex-wrap items-center gap-x-4 gap-y-2">
          {(needsUpdateCount > 0 || needsUpdate) && (
            <div className="flex items-center gap-2" role="group" aria-label={t('devices.updateFilter')}>
              <Download className="size-4 shrink-0 text-text-dim" aria-hidden />
              <FilterChip active={needsUpdate} onClick={() => setNeedsUpdate(!needsUpdate)} count={needsUpdateCount}>
                {t('devices.needsUpdate')}
              </FilterChip>
            </div>
          )}
          {(weakBattCount > 0 || weakBatt) && (
            <div className="flex items-center gap-2" role="group" aria-label={t('devices.batteryFilter')}>
              <BatteryWarning className="size-4 shrink-0 text-text-dim" aria-hidden />
              <FilterChip active={weakBatt} onClick={() => setWeakBatt(!weakBatt)} count={weakBattCount}>
                {t('devices.weakBattery')}
              </FilterChip>
            </div>
          )}
        </div>
      )}

      {/* Resource threshold filters */}
      <div className="mt-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:gap-8">
        <ThresholdRow
          icon={MemoryStick}
          label={t('common.ram')}
          ariaLabel={t('devices.ramThreshold')}
          value={memMin}
          onChange={(v) => setThreshold('mem_min', v)}
        />
        <ThresholdRow
          icon={HardDrive}
          label={t('common.disk')}
          ariaLabel={t('devices.diskThreshold')}
          value={diskMin}
          onChange={(v) => setThreshold('disk_min', v)}
        />
      </div>

      {/* Fleet grid */}
      <div className="mt-6">
        {isLoading ? (
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
            {Array.from({ length: 8 }).map((_, i) => (
              <SkeletonCard key={i} />
            ))}
          </div>
        ) : visible.length === 0 ? (
          <EmptyState
            icon={Monitor}
            title={t('devices.emptyTitle')}
            description={
              q || tag || untagged || status !== 'all' || needsUpdate || weakBatt || memMin > 0 || diskMin > 0
                ? t('devices.emptyFiltered')
                : t('devices.emptyFleet')
            }
          />
        ) : (
          <motion.ul
            variants={staggerContainer()}
            initial="hidden"
            animate="show"
            className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4"
          >
            {visible.map((m) => (
              <MachineCard key={m.machine_id} machine={m} pulse={pulses[m.machine_id] ?? 0} />
            ))}
          </motion.ul>
        )}
      </div>
    </div>
  )
}

const THRESHOLD_OPTIONS = [0, 70, 80, 90]

function ThresholdRow({
  icon: Icon,
  label,
  ariaLabel,
  value,
  onChange,
}: {
  icon: LucideIcon
  label: string
  ariaLabel: string
  value: number
  onChange: (value: number) => void
}) {
  const t = useT()
  return (
    <div className="flex items-center gap-2" role="group" aria-label={ariaLabel}>
      <Icon className="size-4 shrink-0 text-text-dim" aria-hidden />
      <span className="mr-0.5 text-[13px] font-medium text-text-dim">{label}</span>
      {THRESHOLD_OPTIONS.map((opt) => (
        <FilterChip key={opt} active={value === opt} onClick={() => onChange(opt)}>
          {opt === 0 ? t('devices.thresholdOff') : t('devices.thresholdAtLeast', { value: formatPercent(opt) })}
        </FilterChip>
      ))}
    </div>
  )
}
