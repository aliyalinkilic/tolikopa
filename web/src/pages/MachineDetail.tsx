import { useState } from 'react'
import type { ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  Activity, AppWindow, Archive, ArchiveRestore, Battery, BatteryCharging, BellRing, Check, ChevronLeft, Clock, Copy, Cpu, Download,
  HardDrive, Info, Pencil, PieChart as PieIcon, Plug, Plus, Power, RefreshCw, Save,
  ServerCrash, ShieldAlert, ShieldCheck, Tag, Terminal, TriangleAlert, X,
} from 'lucide-react'
import { Tabs } from '@/components/ui/Tabs'
import { Card, CardHeader } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Modal } from '@/components/ui/Modal'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { RangeSelector } from '@/components/RangeSelector'
import { RingGauge } from '@/components/RingGauge'
import { StatusBadge } from '@/components/StatusBadge'
import { TimeSeriesChart } from '@/components/TimeSeriesChart'
import type { ChartRow, SeriesDef } from '@/components/TimeSeriesChart'
import { CategoryDonut } from '@/components/CategoryDonut'
import { Timeline } from '@/components/Timeline'
import { AlertList } from '@/components/AlertList'
import { DataTable } from '@/components/DataTable'
import type { Column } from '@/components/DataTable'
import { EmptyState } from '@/components/EmptyState'
import { Skeleton, SkeletonText } from '@/components/Skeleton'
import {
  useAckAlert, useArchiveMachines, useMachine, useMachineAlerts, useMachineCategories, useMachineEvents,
  useMachineMetrics, useMachineSessions, useRestoreMachines, useTopProcesses, useUpdateMachineTags,
} from '@/hooks/queries'
import { useMachineLive } from '@/hooks/useMachineLive'
import { useIsAdmin } from '@/hooks/useAuth'
import { useToast } from '@/hooks/useToast'
import { useRange } from '@/hooks/useRange'
import {
  BATTERY_CHARGE_THRESHOLDS, BATTERY_HEALTH_THRESHOLDS, CPU_THRESHOLDS, DISK_USED_THRESHOLDS,
  RAM_THRESHOLDS, chart, metricTone, toneColor,
} from '@/lib/theme'
import { catMeta, eventKindLabel, kindLabel, severityLabel, severityToken } from '@/lib/labels'
import {
  displayUser, formatBytes, formatDate, formatDateTime, formatPercent, formatDayTime, formatWh, relativeTime,
} from '@/lib/format'
import type { EventDTO, MachineDetail as MachineDetailDTO, NetPoint, SeriesPoint, TopProcess } from '@/lib/types'
import { translate, useT } from '@/i18n'
import { Trans } from '@/i18n/rich'
import { sameText } from '@/lib/format'

type TabKey = 'metrics' | 'apps' | 'categories' | 'sessions' | 'alerts' | 'remote'

const round1 = (n: number) => Math.round(n * 10) / 10

export function MachineDetail() {
  const t = useT()
  const { id } = useParams()
  const isAdmin = useIsAdmin()
  const { data: machine, isLoading, isError } = useMachine(id)
  useMachineLive(id) // patches the ['machine', id] cache with live CPU/RAM
  const { key: rangeKey, setKey: setRange, range, win, custom, setCustom } = useRange()
  const [tab, setTab] = useState<TabKey>('metrics')
  const [editorOpen, setEditorOpen] = useState(false)

  if (isLoading) return <DetailSkeleton />
  if (isError || !machine) {
    return (
      <EmptyState
        icon={ServerCrash}
        title={t('machine.loadFailedTitle')}
        description={t('machine.loadFailedBody')}
      />
    )
  }

  const memPct = (machine.mem_ratio ?? 0) * 100
  const diskUsed = 100 - (machine.disk_min_free_pct ?? 100)
  const tags = machine.tags ?? []

  const tabs = [
    { key: 'metrics', label: t('machine.tabMetrics'), icon: <Cpu className="size-4" aria-hidden /> },
    { key: 'apps', label: t('machine.tabApps'), icon: <AppWindow className="size-4" aria-hidden /> },
    { key: 'categories', label: t('machine.tabCategories'), icon: <PieIcon className="size-4" aria-hidden /> },
    { key: 'sessions', label: t('machine.tabSessions'), icon: <Clock className="size-4" aria-hidden /> },
    { key: 'alerts', label: t('machine.tabAlerts'), icon: <BellRing className="size-4" aria-hidden /> },
    // Remote-management commands are an admin-only convenience surface.
    ...(isAdmin
      ? [{ key: 'remote', label: t('machine.tabRemote'), icon: <Terminal className="size-4" aria-hidden /> }]
      : []),
  ]

  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <Link to="/devices" className="inline-flex items-center gap-1 text-[13px] text-text-muted hover:text-accent ring-focus">
          <ChevronLeft className="size-4" aria-hidden />
          {t('nav.devices')}
        </Link>
        {isAdmin && !machine.archived_at && <ArchiveButton machine={machine} />}
      </div>

      {machine.archived_at && <ArchivedBanner machine={machine} canRestore={isAdmin} />}

      {/* Identity + live gauges */}
      <Card className="mb-6">
        <div className="flex flex-col gap-6 lg:flex-row lg:items-center lg:justify-between">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2.5">
              <h1 className="text-[24px] font-bold tracking-[-0.01em] text-text">{machine.hostname}</h1>
              <StatusBadge online={machine.online} sessionState={machine.session_state} />
              <Badge tone="neutral">{kindLabel(machine.kind)}</Badge>
            </div>
            <div className="mt-3 flex flex-wrap items-center gap-1.5">
              {tags.map((tag) => (
                <Badge key={tag} tone="accent" icon={<Tag className="size-3" aria-hidden />}>
                  {tag}
                </Badge>
              ))}
              {tags.length === 0 && !isAdmin && (
                <span className="text-[12px] text-text-dim">{t('machine.noTags')}</span>
              )}
              {isAdmin && (
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-7 px-2 text-[12px]"
                  leftIcon={<Pencil className="size-3.5" aria-hidden />}
                  onClick={() => setEditorOpen(true)}
                  aria-label={t('machine.editTags')}
                  title={t('machine.editTags')}
                >
                  {tags.length === 0 ? t('machine.addTag') : t('machine.editTagsShort')}
                </Button>
              )}
            </div>
            <dl className="mt-4 grid grid-cols-2 gap-x-8 gap-y-2 text-[13px] sm:grid-cols-3">
              <Meta label={t('common.user')} value={displayUser(machine.logon_user)} />
              <Meta label={t('machine.os')} value={machine.os ? `${machine.os}${machine.os_build ? ` (${machine.os_build})` : ''}` : '—'} />
              <Meta label={t('machine.processor')} value={machine.cpu_model ? `${machine.cpu_model}${machine.cpu_cores ? ` · ${t('common.cores', { count: machine.cpu_cores })}` : ''}` : '—'} />
              <Meta
                label={t('machine.ip')}
                value={
                  machine.ip_addrs?.length
                    ? machine.ip_addrs.join(', ')
                    : machine.last_ip
                      ? t('machine.ipSeenByServer', { ip: machine.last_ip })
                      : '—'
                }
              />
              <Meta label={t('machine.memoryRam')} value={machine.mem_total ? formatBytes(machine.mem_total) : '—'} />
              <Meta label={t('machine.storage')} value={machine.disks?.length ? formatBytes(machine.disks.reduce((a, d) => a + d.total, 0)) : '—'} />
              <Meta label={t('machine.agent')} value={machine.agent_version ?? '—'} />
              <Meta label={t('machine.domain')} value={machine.domain ?? '—'} />
              <Meta label={t('common.firstSeen')} value={machine.first_seen ? formatDateTime(machine.first_seen) : '—'} />
              <Meta
                label={t('common.lastSeen')}
                value={machine.online ? t('common.live') : relativeTime(machine.last_seen)}
                tone={machine.online ? 'ok' : undefined}
              />
            </dl>
          </div>

          <div className="flex shrink-0 items-center justify-center gap-4 sm:gap-6">
            <RingGauge value={machine.cpu_pct} label={t('common.cpu')} thresholds={CPU_THRESHOLDS} size={104} stroke={9} />
            <RingGauge value={memPct} label={t('common.ram')} thresholds={RAM_THRESHOLDS} size={104} stroke={9} />
            <RingGauge value={diskUsed} label={t('common.disk')} thresholds={DISK_USED_THRESHOLDS} size={104} stroke={9} />
          </div>
        </div>

        <DiskStrip machine={machine} />
      </Card>

      <div className="mb-6 grid gap-4 lg:grid-cols-2">
        <UpdatesCard machine={machine} />
        <BatteryCard machine={machine} />
      </div>

      {/* Tabs toolbar */}
      <div className="mb-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <Tabs
          tabs={tabs}
          value={tab}
          onChange={(k) => setTab(k as TabKey)}
          ariaLabel={t('machine.tabsLabel')}
          className="w-full sm:w-auto"
        />
        {tab !== 'alerts' && tab !== 'remote' && (
          <RangeSelector value={rangeKey} onChange={setRange} custom={custom} onCustomChange={setCustom} />
        )}
      </div>

      {tab === 'metrics' && id && <MetricsTab id={id} win={win} res={range.res} spanMs={range.ms} />}
      {tab === 'apps' && id && <AppsTab id={id} win={win} />}
      {tab === 'categories' && id && <CategoriesTab id={id} win={win} />}
      {tab === 'sessions' && id && <SessionsTab id={id} win={win} />}
      {tab === 'alerts' && id && <AlertsTab id={id} win={win} />}
      {tab === 'remote' && isAdmin && <RemoteTab hostname={machine.hostname} />}

      {isAdmin && id && (
        <MachineTagEditor
          key={editorOpen ? 'open' : 'closed'}
          open={editorOpen}
          onClose={() => setEditorOpen(false)}
          machineId={id}
          hostname={machine.hostname}
          initialTags={tags}
          labels={machine.labels}
        />
      )}
    </div>
  )
}

// ── Machine tag editor (admin) ────────────────────────────────────────────────
function MachineTagEditor({
  open,
  onClose,
  machineId,
  hostname,
  initialTags,
  labels,
}: {
  open: boolean
  onClose: () => void
  machineId: string
  hostname: string
  initialTags: string[]
  labels?: Record<string, unknown>
}) {
  const t = useT()
  const update = useUpdateMachineTags()
  const { push } = useToast()
  const [tags, setTags] = useState<string[]>(initialTags)
  const [tagInput, setTagInput] = useState('')

  const addTag = () => {
    const v = tagInput.trim()
    if (!v) return
    if (!tags.some((x) => sameText(x, v))) {
      setTags([...tags, v])
    }
    setTagInput('')
  }

  const removeTag = (v: string) => setTags((list) => list.filter((x) => x !== v))

  const save = () => {
    update.mutate(
      { id: machineId, labels: labels ?? {}, tags },
      {
        onSuccess: () => {
          push({ title: t('machine.tagsSaved'), tone: 'ok' })
          onClose()
        },
        onError: () => push({ title: t('machine.tagsSaveFailed'), tone: 'crit' }),
      },
    )
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={t('machine.tagsTitle')}
      description={hostname}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>{t('common.cancel')}</Button>
          <Button variant="primary" leftIcon={<Save className="size-4" aria-hidden />} loading={update.isPending} onClick={save}>
            {t('common.save')}
          </Button>
        </>
      }
    >
      <div className="space-y-4 pb-2">
        <div>
          <span className="mb-1.5 block text-[13px] font-medium text-text-muted">{t('machine.tags')}</span>
          <div className="flex flex-wrap items-center gap-1.5">
            {tags.length === 0 && <span className="text-[13px] text-text-dim">{t('machine.noTagsYet')}</span>}
            {tags.map((tag) => (
              <span
                key={tag}
                className="inline-flex items-center gap-1 rounded-full border border-accent/25 bg-accent/12 py-0.5 pl-2.5 pr-1 text-[12px] font-medium text-accent"
              >
                {tag}
                <button
                  type="button"
                  onClick={() => removeTag(tag)}
                  aria-label={t('machine.removeTag', { tag })}
                  className="grid size-4 place-items-center rounded-full text-accent/70 transition-colors hover:bg-accent/20 hover:text-accent ring-focus"
                >
                  <X className="size-3" aria-hidden />
                </button>
              </span>
            ))}
          </div>
          <div className="mt-2.5 flex items-center gap-2">
            <Input
              placeholder={t('machine.tagPlaceholder')}
              value={tagInput}
              onChange={(e) => setTagInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  addTag()
                }
              }}
            />
            <Button variant="secondary" leftIcon={<Plus className="size-4" aria-hidden />} onClick={addTag} disabled={!tagInput.trim()}>
              {t('common.add')}
            </Button>
          </div>
        </div>
      </div>
    </Modal>
  )
}

function Meta({ label, value, tone }: { label: string; value: string; tone?: 'ok' }) {
  return (
    <div className="min-w-0">
      <dt className="text-[11px] font-medium uppercase tracking-[0.1em] text-text-dim">{label}</dt>
      <dd className={`mt-0.5 truncate font-medium ${tone === 'ok' ? 'text-ok' : 'text-text'}`} title={value}>
        {value}
      </dd>
    </div>
  )
}

function DiskStrip({ machine }: { machine: MachineDetailDTO }) {
  const t = useT()
  if (!machine.disks || machine.disks.length === 0) return null
  const healthByDisk = new Map((machine.disk_health ?? []).map((h) => [h.disk, h]))
  return (
    <div className="mt-6 border-t border-border pt-5">
      <div className="mb-3 flex items-center gap-2 text-[12px] font-semibold uppercase tracking-[0.1em] text-text-dim">
        <HardDrive className="size-3.5" aria-hidden /> {t('machine.disks')}
      </div>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {machine.disks.map((d) => {
          const usedPct = d.total > 0 ? (d.used / d.total) * 100 : 0
          const tone = metricTone(usedPct, DISK_USED_THRESHOLDS)
          const health = healthByDisk.get(d.mount)
          return (
            <div key={d.mount} className="rounded-[var(--radius-md)] border border-border bg-bg-elev/40 p-3">
              <div className="flex items-center justify-between text-[13px]">
                <span className="font-semibold text-text">{d.mount}</span>
                {health ? (
                  health.predict_fail || !health.smart_ok ? (
                    <Badge tone="crit" icon={<TriangleAlert className="size-3" aria-hidden />}>{t('machine.smartBad')}</Badge>
                  ) : (
                    <Badge tone="ok" icon={<ShieldCheck className="size-3" aria-hidden />}>{t('machine.healthy')}</Badge>
                  )
                ) : null}
              </div>
              <div className="mt-2 h-2 overflow-hidden rounded-full bg-wash-2">
                <div className="h-full rounded-full" style={{ width: `${Math.min(100, usedPct)}%`, backgroundColor: toneColor[tone] }} />
              </div>
              <div className="mt-1.5 flex items-center justify-between text-[12px] text-text-muted">
                <span className="tabular">{formatBytes(d.used)} / {formatBytes(d.total)}</span>
                <span className="tabular">{formatPercent(usedPct)}</span>
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

// ── Windows Update status (read-only monitoring surface) ─────────────────────────
function UpdatesCard({ machine }: { machine: MachineDetailDTO }) {
  const t = useT()
  const pending = machine.pending_updates ?? null
  const rebootPending = machine.reboot_pending === true
  const checkedAt = machine.updates_checked_at ?? null

  let status: ReactNode
  if (pending == null) {
    status = (
      <span className="inline-flex items-center gap-1.5 font-medium text-text-muted">
        <Info className="size-4 text-text-dim" aria-hidden /> {t('common.unknown')}
      </span>
    )
  } else if (pending === 0) {
    status = (
      <span className="inline-flex items-center gap-1.5 font-medium text-ok">
        <ShieldCheck className="size-4" aria-hidden /> {t('machine.upToDate')}
      </span>
    )
  } else {
    status = (
      <span className="inline-flex items-center gap-1.5 font-medium text-warn">
        <Download className="size-4" aria-hidden />
        <span className="tabular">{t('machine.updatesPending', { count: pending })}</span>
      </span>
    )
  }

  return (
    <Card>
      <div className="flex items-center gap-2 text-[14px] font-semibold text-text">
        <span className="grid size-7 place-items-center rounded-[9px] bg-surface-2 text-accent">
          <Download className="size-4" aria-hidden />
        </span>
        {t('machine.updatesTitle')}
      </div>

      {checkedAt == null ? (
        <p className="mt-4 text-[13px] text-text-muted">
          {t('machine.updatesUnknown')}
        </p>
      ) : (
        <>
          <dl className="mt-4 grid grid-cols-2 gap-x-8 gap-y-3 text-[13px] sm:grid-cols-4">
            <div className="min-w-0">
              <dt className="text-[11px] font-medium uppercase tracking-[0.1em] text-text-dim">{t('common.status')}</dt>
              <dd className="mt-1">{status}</dd>
            </div>
            <div className="min-w-0">
              <dt className="text-[11px] font-medium uppercase tracking-[0.1em] text-text-dim">{t('machine.reboot')}</dt>
              <dd className="mt-1">
                {rebootPending ? (
                  <Badge tone="warn" icon={<RefreshCw className="size-3" aria-hidden />}>{t('machine.rebootPending')}</Badge>
                ) : (
                  <span className="font-medium text-text-muted">{t('machine.rebootNotNeeded')}</span>
                )}
              </dd>
            </div>
            <UpdatesMeta label={t('machine.lastInstall')} iso={machine.updates_last_install ?? null} />
            <UpdatesMeta label={t('machine.lastSearch')} iso={machine.updates_last_search ?? null} />
          </dl>
          <p className="mt-4 flex items-center gap-1.5 text-[11px] text-text-dim">
            <Clock className="size-3" aria-hidden /> {t('machine.lastChecked', { when: relativeTime(checkedAt) })}
          </p>
        </>
      )}
    </Card>
  )
}

function UpdatesMeta({ label, iso }: { label: string; iso: string | null }) {
  return (
    <div className="min-w-0">
      <dt className="text-[11px] font-medium uppercase tracking-[0.1em] text-text-dim">{label}</dt>
      <dd className="mt-1 font-medium text-text">
        {iso ? (
          <>
            <span className="tabular block truncate" title={formatDateTime(iso)}>{formatDate(iso)}</span>
            <span className="text-[11px] font-normal text-text-dim">{relativeTime(iso)}</span>
          </>
        ) : (
          '—'
        )}
      </dd>
    </div>
  )
}

// ── Battery / power status (read-only monitoring surface) ────────────────────────
function BatteryCard({ machine }: { machine: MachineDetailDTO }) {
  const t = useT()
  const bat = machine.battery ?? null

  const header = (
    <div className="flex items-center gap-2 text-[14px] font-semibold text-text">
      <span className="grid size-7 place-items-center rounded-[9px] bg-surface-2 text-accent">
        <Battery className="size-4" aria-hidden />
      </span>
      {t('machine.batteryTitle')}
    </div>
  )

  if (bat == null) {
    return (
      <Card>
        {header}
        <p className="mt-4 text-[13px] text-text-muted">
          {t('machine.batteryUnknown')}
        </p>
      </Card>
    )
  }

  if (bat.present === false) {
    return (
      <Card>
        {header}
        <p className="mt-4 text-[13px] text-text-muted">{t('machine.batteryNone')}</p>
      </Card>
    )
  }

  const percent = bat.percent ?? null
  const chargeTone: 'ok' | 'warn' | 'crit' | null =
    percent == null ? null : metricTone(percent, BATTERY_CHARGE_THRESHOLDS)
  const health = bat.health_pct ?? null
  const healthTone: 'ok' | 'warn' | 'crit' | null =
    health == null ? null : metricTone(health, BATTERY_HEALTH_THRESHOLDS)

  const StateIcon = bat.charging ? BatteryCharging : bat.ac_connected ? Plug : Battery
  const stateLabel = bat.charging ? t('machine.charging') : bat.ac_connected ? t('machine.pluggedIn') : t('machine.onBattery')
  const stateTone: 'ok' | 'accent' | 'neutral' = bat.charging ? 'ok' : bat.ac_connected ? 'accent' : 'neutral'

  const capacity =
    bat.full_charge_capacity != null && bat.design_capacity != null
      ? `${formatWh(bat.full_charge_capacity)} / ${formatWh(bat.design_capacity)}`
      : bat.full_charge_capacity != null
        ? formatWh(bat.full_charge_capacity)
        : null

  return (
    <Card>
      {header}

      {/* Charge bar + state chip */}
      <div className="mt-4">
        <div className="flex items-center justify-between text-[13px]">
          <span className="font-medium text-text-muted">{t('machine.charge')}</span>
          <Badge tone={stateTone} icon={<StateIcon className="size-3" aria-hidden />}>{stateLabel}</Badge>
        </div>
        <div className="mt-2 h-2.5 overflow-hidden rounded-full bg-wash-2">
          <div
            className="h-full rounded-full transition-[width] duration-500"
            style={{
              width: `${percent == null ? 0 : Math.min(100, Math.max(0, percent))}%`,
              backgroundColor: chargeTone ? toneColor[chargeTone] : chart.offline,
            }}
          />
        </div>
        <div className="mt-1.5 text-right text-[13px]">
          <span className="tabular font-semibold text-text">
            {percent == null ? t('common.unknown') : formatPercent(percent)}
          </span>
        </div>
      </div>

      <dl className="mt-4 grid grid-cols-2 gap-x-8 gap-y-3 text-[13px]">
        <div className="min-w-0">
          <dt className="text-[11px] font-medium uppercase tracking-[0.1em] text-text-dim">{t('machine.batteryHealth')}</dt>
          <dd className="mt-1">
            {health == null ? (
              <span className="font-medium text-text-muted">{t('common.unknown')}</span>
            ) : (
              <span
                className="tabular font-semibold"
                style={{ color: healthTone ? toneColor[healthTone] : undefined }}
              >
                {formatPercent(health)}
              </span>
            )}
          </dd>
        </div>
        {capacity && (
          <div className="min-w-0">
            <dt className="text-[11px] font-medium uppercase tracking-[0.1em] text-text-dim">{t('machine.capacity')}</dt>
            <dd className="tabular mt-1 truncate font-medium text-text" title={capacity}>{capacity}</dd>
          </div>
        )}
        {bat.cycle_count != null && (
          <div className="min-w-0">
            <dt className="text-[11px] font-medium uppercase tracking-[0.1em] text-text-dim">{t('machine.cycles')}</dt>
            <dd className="tabular mt-1 font-medium text-text">{bat.cycle_count}</dd>
          </div>
        )}
      </dl>

      {bat.updated_at && (
        <p className="mt-4 flex items-center gap-1.5 text-[11px] text-text-dim">
          <Clock className="size-3" aria-hidden /> {t('machine.lastChecked', { when: relativeTime(bat.updated_at) })}
        </p>
      )}
    </Card>
  )
}

// ── Metrics tab ─────────────────────────────────────────────────────────────────
function normalizeMem(mem?: SeriesPoint[]): number {
  const max = Math.max(0, ...(mem?.map((p) => p.avg) ?? [0]))
  return max <= 1.5 ? 100 : 1
}

function toMetricRows(cpu?: SeriesPoint[], mem?: SeriesPoint[]): ChartRow[] {
  const map = new Map<string, ChartRow>()
  cpu?.forEach((p) => map.set(p.t, { ...(map.get(p.t) ?? { t: p.t }), t: p.t, cpu: round1(p.avg) }))
  const scale = normalizeMem(mem)
  mem?.forEach((p) => map.set(p.t, { ...(map.get(p.t) ?? { t: p.t }), t: p.t, mem: round1(p.avg * scale) }))
  return [...map.values()].sort((a, b) => String(a.t).localeCompare(String(b.t)))
}

function toNetRows(net?: NetPoint[]): ChartRow[] {
  return (net ?? []).map((p) => ({ t: p.t, recv: p.recv, sent: p.sent }))
}

// Functions, not constants: series names follow the active language.
const cpuMemSeries = (): SeriesDef[] => [
  { key: 'cpu', name: translate('common.cpu'), color: chart.cpu },
  { key: 'mem', name: translate('common.ram'), color: chart.mem },
]
const netSeries = (): SeriesDef[] => [
  { key: 'recv', name: translate('machine.received'), color: chart.netDown },
  { key: 'sent', name: translate('machine.sent'), color: chart.netUp },
]

function MetricsTab({ id, win, res, spanMs }: { id: string; win: { from: string; to: string }; res: string; spanMs: number }) {
  const t = useT()
  const { data, isLoading } = useMachineMetrics(id, win, res)
  if (isLoading) return <ChartSkeleton count={2} />
  const cpuMem = toMetricRows(data?.cpu, data?.mem)
  const net = toNetRows(data?.net)
  return (
    <div className="grid gap-4 xl:grid-cols-2">
      <Card>
        <CardHeader title={t('machine.cpuMemTitle')} description={t('machine.cpuMemBody')} />
        <div className="mt-4">
          <TimeSeriesChart data={cpuMem} series={cpuMemSeries()} unit="percent" spanMs={spanMs} yDomain={[0, 100]} />
        </div>
      </Card>
      <Card>
        <CardHeader title={t('machine.netTitle')} description={t('machine.netBody')} />
        <div className="mt-4">
          <TimeSeriesChart data={net} series={netSeries()} unit="bytes" spanMs={spanMs} />
        </div>
      </Card>
    </div>
  )
}

// ── Apps tab ────────────────────────────────────────────────────────────────────
function AppsTab({ id, win }: { id: string; win: { from: string; to: string } }) {
  const t = useT()
  const [by, setBy] = useState<'cpu' | 'mem'>('cpu')
  const { data, isLoading } = useTopProcesses(id, win, by, 20)

  const columns: Column<TopProcess>[] = [
    {
      key: 'name',
      header: t('machine.process'),
      render: (p) => <span className="font-mono text-[13px] text-text">{p.name}</span>,
      sortable: true,
      sortValue: (p) => p.name,
    },
    {
      key: 'category',
      header: t('common.category'),
      render: (p) => {
        const meta = catMeta(p.category)
        const Icon = meta.icon
        return (
          <span className="inline-flex items-center gap-1.5 text-[13px] text-text-muted">
            <span className="size-2 rounded-full" style={{ backgroundColor: meta.color }} aria-hidden />
            <Icon className="size-3.5 text-text-dim" aria-hidden />
            {meta.label}
          </span>
        )
      },
      sortable: true,
      sortValue: (p) => catMeta(p.category).label,
    },
    {
      key: 'cpu',
      header: t('common.cpu'),
      align: 'right',
      render: (p) => (
        <div className="flex items-center justify-end gap-2">
          <span className="hidden h-1.5 w-16 overflow-hidden rounded-full bg-wash-2 sm:block">
            <span className="block h-full rounded-full bg-[color:var(--color-metric-cpu)]" style={{ width: `${Math.min(100, p.cpu_pct)}%` }} />
          </span>
          <span className="tabular w-12 text-right font-semibold text-text">{formatPercent(p.cpu_pct, 1)}</span>
        </div>
      ),
      sortable: true,
      sortValue: (p) => p.cpu_pct,
    },
    {
      key: 'mem',
      header: t('common.memory'),
      align: 'right',
      render: (p) => <span className="tabular text-text">{formatBytes(p.mem_bytes)}</span>,
      sortable: true,
      sortValue: (p) => p.mem_bytes,
    },
  ]

  return (
    <Card>
      <CardHeader
        title={t('machine.topAppsTitle')}
        description={t('machine.topAppsBody')}
        action={
          <div className="inline-flex rounded-[var(--radius-md)] bg-bg-elev/70 p-1">
            {(['cpu', 'mem'] as const).map((k) => (
              <button
                key={k}
                type="button"
                onClick={() => setBy(k)}
                aria-pressed={by === k}
                className={`rounded-[9px] px-3 py-1 text-[12px] font-medium transition-colors ring-focus ${by === k ? 'bg-surface-2 text-text' : 'text-text-muted hover:text-text'}`}
              >
                {k === 'cpu' ? t('machine.byCpu') : t('machine.byMem')}
              </button>
            ))}
          </div>
        }
      />
      <div className="mt-4">
        {isLoading ? (
          <SkeletonText lines={8} />
        ) : (
          <DataTable
            columns={columns}
            rows={data ?? []}
            getRowKey={(p) => p.name}
            initialSort={{ key: by, dir: 'desc' }}
            emptyLabel={t('machine.noProcessData')}
            maxHeight={520}
          />
        )}
      </div>
    </Card>
  )
}

// ── Categories tab ──────────────────────────────────────────────────────────────
function CategoriesTab({ id, win }: { id: string; win: { from: string; to: string } }) {
  const t = useT()
  const { data, isLoading } = useMachineCategories(id, win)
  return (
    <Card>
      <CardHeader title={t('machine.categoriesTitle')} description={t('machine.categoriesBody')} />
      <div className="mt-5">{isLoading ? <Skeleton className="h-44 w-full" /> : <CategoryDonut data={data ?? []} />}</div>
    </Card>
  )
}

// ── Sessions tab ────────────────────────────────────────────────────────────────
function SessionsTab({ id, win }: { id: string; win: { from: string; to: string } }) {
  const t = useT()
  const { data, isLoading } = useMachineSessions(id, win)
  const sessions = data ?? []
  return (
    <div className="space-y-4">
      <Card>
        <CardHeader title={t('machine.sessionsTitle')} description={t('machine.sessionsBody')} />
        <div className="mt-5">
          {isLoading ? <Skeleton className="h-24 w-full" /> : <Timeline sessions={sessions} from={win.from} to={win.to} groupBy="none" />}
        </div>
      </Card>
    </div>
  )
}

// ── Alerts tab ──────────────────────────────────────────────────────────────────
function AlertsTab({ id, win }: { id: string; win: { from: string; to: string } }) {
  const t = useT()
  const { data: alerts, isLoading } = useMachineAlerts(id)
  const { data: events } = useMachineEvents(id, win)
  const ack = useAckAlert()

  return (
    <div className="grid gap-4 lg:grid-cols-[1.4fr_1fr]">
      <Card>
        <CardHeader title={t('nav.alerts')} description={t('machine.alertsBody')} />
        <div className="mt-4">
          {isLoading ? (
            <SkeletonText lines={5} />
          ) : (
            <AlertList
              alerts={alerts ?? []}
              onAck={(alertId) => ack.mutate(alertId)}
              ackingId={ack.isPending ? (ack.variables ?? null) : null}
              showMachine={false}
            />
          )}
        </div>
      </Card>
      <Card>
        <CardHeader title={t('machine.eventsTitle')} description={t('machine.eventsBody')} />
        <div className="mt-4">
          <EventList events={events ?? []} />
        </div>
      </Card>
    </div>
  )
}

function EventList({ events }: { events: EventDTO[] }) {
  const t = useT()
  if (events.length === 0) {
    return <EmptyState icon={ShieldCheck} title={t('machine.noEventsTitle')} description={t('machine.noEventsBody')} />
  }
  return (
    <ul className="divide-y divide-border">
      {events.map((e, i) => {
        const tone = e.severity ? severityToken(e.severity) : 'warn'
        return (
          <li key={`${e.ts}-${i}`} className="flex items-start gap-3 py-2.5 first:pt-0">
            <div className="mt-0.5 grid size-7 shrink-0 place-items-center rounded-[9px] bg-warn/12 text-warn">
              <ServerCrash className="size-3.5" aria-hidden />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="text-[13px] font-semibold text-text">{eventKindLabel(e.kind)}</span>
                {e.severity && <Badge tone={tone}>{severityLabel(e.severity)}</Badge>}
              </div>
              <p className="text-[12px] text-text-muted">
                {e.process_name ? <span className="font-mono">{e.process_name}</span> : '—'}
                {e.detail?.exception ? <span className="tabular"> · {String(e.detail.exception)}</span> : null}
              </p>
              <p className="text-[11px] text-text-dim" title={formatDateTime(e.ts)}>{formatDayTime(e.ts)} · {relativeTime(e.ts)}</p>
            </div>
          </li>
        )
      })}
    </ul>
  )
}

// ── Remote-management tab (admin) ─────────────────────────────────────────────
// SAFETY / DESIGN NOTE: this tab renders ready-to-run, hostname-prefilled
// PowerShell one-liners that the admin COPIES and runs from their OWN elevated
// workstation (Kerberos-authenticated, WinRM, fully audited by AD). NOTHING here
// executes through the Tolikopa agent or the panel — by design the agent has no
// inbound command channel, so the panel can never be an RCE surface for the fleet.
type Cmd = { label: string; desc: string; code: string; danger?: boolean; note?: string }
type CmdGroup = { title: string; icon: ReactNode; hint?: string; cmds: Cmd[] }

function remoteGroups(host: string): CmdGroup[] {
  const h = host || translate('machine.hostPlaceholder')
  const ic = (c: string) => `Invoke-Command -ComputerName '${h}' -ScriptBlock { ${c} }`
  return [
    {
      title: translate('machine.updatesTitle'),
      icon: <Download className="size-4" aria-hidden />,
      hint: translate('machine.remote.updatesHint'),
      cmds: [
        {
          label: translate('machine.remote.listPending'),
          desc: translate('machine.remote.listPendingDesc'),
          code: ic(`(New-Object -ComObject Microsoft.Update.Session).CreateUpdateSearcher().Search("IsInstalled=0 AND IsHidden=0").Updates | Select-Object Title`),
        },
        {
          label: translate('machine.remote.scan'),
          desc: translate('machine.remote.scanDesc'),
          code: ic(`UsoClient StartInteractiveScan`),
        },
        {
          label: translate('machine.remote.install'),
          desc: translate('machine.remote.installDesc'),
          code: ic(`Install-WindowsUpdate -AcceptAll -IgnoreReboot`),
          note: translate('machine.remote.installNote'),
        },
      ],
    },
    {
      title: translate('machine.remote.gpoTitle'),
      icon: <RefreshCw className="size-4" aria-hidden />,
      hint: translate('machine.remote.gpoHint'),
      cmds: [
        {
          label: translate('machine.remote.gpupdate'),
          desc: translate('machine.remote.gpupdateDesc'),
          code: ic(`gpupdate /force`),
        },
        {
          label: translate('machine.remote.gpupdateRsat'),
          desc: translate('machine.remote.gpupdateRsatDesc'),
          code: `Invoke-GPUpdate -Computer '${h}' -Force`,
        },
      ],
    },
    {
      title: translate('machine.remote.diagTitle'),
      icon: <Activity className="size-4" aria-hidden />,
      cmds: [
        {
          label: translate('machine.remote.sysSummary'),
          desc: translate('machine.remote.sysSummaryDesc'),
          code: ic(`Get-ComputerInfo -Property CsName,OsName,OsVersion,OsLastBootUpTime`),
        },
        {
          label: translate('machine.remote.diskFree'),
          desc: translate('machine.remote.diskFreeDesc'),
          code: ic(`Get-PSDrive -PSProvider FileSystem | Select-Object Name,@{n='FreeGB';e={[math]::Round($_.Free/1GB,1)}},@{n='UsedGB';e={[math]::Round($_.Used/1GB,1)}}`),
        },
        {
          label: translate('machine.remote.agentStatus'),
          desc: translate('machine.remote.agentStatusDesc'),
          code: ic(`Get-Service Tolikopa | Select-Object Status,StartType`),
        },
      ],
    },
    {
      title: translate('machine.remote.maintTitle'),
      icon: <Power className="size-4" aria-hidden />,
      cmds: [
        {
          label: translate('machine.remote.agentRestart'),
          desc: translate('machine.remote.agentRestartDesc'),
          code: ic(`Restart-Service Tolikopa`),
        },
        {
          label: translate('machine.remote.flushDns'),
          desc: translate('machine.remote.flushDnsDesc'),
          code: ic(`Clear-DnsClientCache`),
        },
        {
          label: translate('machine.remote.restart'),
          desc: translate('machine.remote.restartDesc'),
          code: `Restart-Computer -ComputerName '${h}' -Force`,
          danger: true,
        },
        {
          label: translate('machine.remote.rdp'),
          desc: translate('machine.remote.rdpDesc'),
          code: `mstsc /v:${h}`,
        },
      ],
    },
    {
      title: translate('machine.remote.genericTitle'),
      icon: <Terminal className="size-4" aria-hidden />,
      cmds: [
        {
          label: translate('machine.remote.generic'),
          desc: translate('machine.remote.genericDesc'),
          code: ic(translate('machine.remote.genericComment')),
        },
      ],
    },
  ]
}

function useCopy() {
  const [copied, setCopied] = useState<string | null>(null)
  const copy = async (text: string, id: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(id)
      window.setTimeout(() => setCopied((c) => (c === id ? null : c)), 1600)
    } catch {
      /* clipboard unavailable */
    }
  }
  return { copied, copy }
}

function RemoteTab({ hostname }: { hostname: string }) {
  const t = useT()
  const { copied, copy } = useCopy()
  const groups = remoteGroups(hostname)
  return (
    <div className="space-y-5">
      <div className="flex items-start gap-3 rounded-[var(--radius-md)] border border-accent/20 bg-accent/[0.06] p-4">
        <Info className="mt-0.5 size-5 shrink-0 text-accent" aria-hidden />
        <div className="text-[13px] leading-relaxed text-text-muted">
          <p className="font-semibold text-text">{t('machine.remote.introTitle')}</p>
          <p className="mt-1">
            <Trans
              k="machine.remote.introBody"
              values={{ host: hostname }}
              tags={{ m: (c) => <span className="font-mono text-text">{c}</span>, b: (c) => <span className="font-medium text-text">{c}</span> }}
            />
          </p>
          <p className="mt-1.5 text-[12px] text-text-dim">
            <Trans k="machine.remote.introReq" tags={{ m: (c) => <span className="font-mono">{c}</span> }} />
          </p>
        </div>
      </div>

      {groups.map((g) => (
        <Card key={g.title}>
          <div className="flex items-center gap-2 text-[14px] font-semibold text-text">
            <span className="grid size-7 place-items-center rounded-[9px] bg-surface-2 text-accent">{g.icon}</span>
            {g.title}
          </div>
          {g.hint && <p className="mt-1.5 text-[12px] text-text-dim">{g.hint}</p>}
          <div className="mt-4 space-y-4">
            {g.cmds.map((cmd) => (
              <CommandCard key={cmd.label} cmd={cmd} copied={copied} onCopy={copy} />
            ))}
          </div>
        </Card>
      ))}
    </div>
  )
}

function CommandCard({ cmd, copied, onCopy }: { cmd: Cmd; copied: string | null; onCopy: (text: string, id: string) => void }) {
  const t = useT()
  return (
    <div>
      <div className="flex items-center gap-2">
        <span className="text-[13px] font-medium text-text">{cmd.label}</span>
        {cmd.danger && <Badge tone="crit" icon={<ShieldAlert className="size-3" aria-hidden />}>{t('machine.remote.caution')}</Badge>}
      </div>
      <p className="mt-0.5 text-[12px] text-text-muted">{cmd.desc}</p>
      <div className="relative mt-2">
        <pre className="overflow-x-auto rounded-[var(--radius-md)] border border-border bg-code p-3.5 pr-12 text-[12.5px] leading-relaxed">
          <code className="font-mono whitespace-pre text-text-muted">{cmd.code}</code>
        </pre>
        <button
          type="button"
          onClick={() => onCopy(cmd.code, cmd.label)}
          aria-label={t('machine.remote.copyCommand')}
          className="absolute right-2 top-2 grid size-8 place-items-center rounded-[9px] border border-border-2 bg-surface-2/80 text-text-muted transition-colors hover:text-text ring-focus"
        >
          {copied === cmd.label ? <Check className="size-4 text-ok" aria-hidden /> : <Copy className="size-4" aria-hidden />}
        </button>
      </div>
      {cmd.note && (
        <p className="mt-1.5 flex items-start gap-1.5 text-[11.5px] text-text-dim">
          <Info className="mt-px size-3.5 shrink-0" aria-hidden /> {cmd.note}
        </p>
      )}
    </div>
  )
}

// ── Skeletons ───────────────────────────────────────────────────────────────────
function DetailSkeleton() {
  return (
    <div>
      <Skeleton className="mb-3 h-4 w-16" />
      <Card className="mb-6">
        <div className="flex flex-col gap-6 lg:flex-row lg:justify-between">
          <div className="w-full max-w-md">
            <Skeleton className="h-7 w-52" />
            <div className="mt-4"><SkeletonText lines={4} /></div>
          </div>
          <div className="flex gap-6">
            <Skeleton className="size-[104px] rounded-full" />
            <Skeleton className="size-[104px] rounded-full" />
            <Skeleton className="size-[104px] rounded-full" />
          </div>
        </div>
      </Card>
      <ChartSkeleton count={2} />
    </div>
  )
}

function ChartSkeleton({ count }: { count: number }) {
  return (
    <div className="grid gap-4 xl:grid-cols-2">
      {Array.from({ length: count }).map((_, i) => (
        <Card key={i}>
          <Skeleton className="h-4 w-40" />
          <Skeleton className="mt-4 h-[240px] w-full" />
        </Card>
      ))}
    </div>
  )
}

/** Retires the machine from the panel. History is kept; see MachinesSection. */
function ArchiveButton({ machine }: { machine: MachineDetailDTO }) {
  const t = useT()
  const archive = useArchiveMachines()
  const { push } = useToast()
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button
        size="sm"
        variant="ghost"
        leftIcon={<Archive className="size-4" aria-hidden />}
        onClick={() => setOpen(true)}
      >
        {t('machine.retire')}
      </Button>
      <ConfirmDialog
        open={open}
        onClose={() => setOpen(false)}
        loading={archive.isPending}
        tone="primary"
        title={t('machine.retireTitle', { host: machine.hostname })}
        confirmLabel={t('machine.retireConfirm')}
        description={t('machine.retireBody')}
        onConfirm={() =>
          archive.mutate([machine.machine_id], {
            onSuccess: () => {
              setOpen(false)
              push({ title: t('machine.retired', { host: machine.hostname }), tone: 'ok' })
            },
            onError: () => push({ title: t('machine.retireFailed'), tone: 'crit' }),
          })
        }
      />
    </>
  )
}

function ArchivedBanner({ machine, canRestore }: { machine: MachineDetailDTO; canRestore: boolean }) {
  const t = useT()
  const restore = useRestoreMachines()
  const { push } = useToast()
  return (
    <Card className="mb-4 border-warn/25 bg-warn/[0.06]">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="flex items-start gap-2 text-[13px] text-text-muted">
          <Archive className="mt-0.5 size-4 shrink-0 text-warn" aria-hidden />
          <span>
            {machine.archived_by
              ? t('machine.archivedNoteBy', { when: formatDateTime(machine.archived_at!), by: machine.archived_by })
              : t('machine.archivedNote', { when: formatDateTime(machine.archived_at!) })}
          </span>
        </p>
        {canRestore && (
          <Button
            size="sm"
            variant="secondary"
            leftIcon={<ArchiveRestore className="size-4" aria-hidden />}
            loading={restore.isPending}
            onClick={() =>
              restore.mutate([machine.machine_id], {
                onSuccess: () => push({ title: t('machine.restored', { host: machine.hostname }), tone: 'ok' }),
                onError: () => push({ title: t('machine.restoreFailed'), tone: 'crit' }),
              })
            }
          >
            {t('machine.restore')}
          </Button>
        )}
      </div>
    </Card>
  )
}
