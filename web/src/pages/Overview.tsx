import { useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { motion, useReducedMotion } from 'motion/react'
import {
  Activity, AppWindow, BellRing, CheckCircle2, Clock3, Cpu, Flame, HardDrive, HeartPulse, Info,
  MemoryStick, ShieldAlert, TriangleAlert, Wifi,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { PageHeader } from '@/components/AppShell'
import { Card, CardHeader } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import type { BadgeTone } from '@/components/ui/Badge'
import { KpiCard } from '@/components/KpiCard'
import { RingGauge } from '@/components/RingGauge'
import { TimeSeriesChart } from '@/components/TimeSeriesChart'
import { ActivityHeatmap } from '@/components/ActivityHeatmap'
import { Skeleton, SkeletonCard } from '@/components/Skeleton'
import { EmptyState } from '@/components/EmptyState'
import { useFleetOverview, useFleetSummary, useHourlyActivity, useTopApps } from '@/hooks/queries'
import { fadeUp, staggerContainer } from '@/lib/motion'
import { catMeta, eventKindLabel, ruleLabel, severityLabel, severityToken } from '@/lib/labels'
import type { StatusToken } from '@/lib/labels'
import {
  CPU_THRESHOLDS, DISK_USED_THRESHOLDS, RAM_THRESHOLDS, chart, metricTone, toneColor,
} from '@/lib/theme'
import type { Thresholds } from '@/lib/theme'
import {
  displayUser, formatBytes, formatDuration, formatNumber, formatPercent, formatRatioPercent,
  formatDateTime, relativeTime,
} from '@/lib/format'
import type { TKey } from '@/i18n'
import { cn } from '@/lib/cn'
import type {
  AlertDTO, FleetOverview, HeatmapCell, OverviewEvent, TopAppEntry, TopAppsBy,
} from '@/lib/types'
import { useT } from '@/i18n'
import { translateDynamic } from '@/i18n'
import { alertMessage } from '@/lib/alertText'
import { tint } from '@/lib/theme'
import { statusColor } from '@/lib/labels'

const DAY_MS = 24 * 3600_000

// Health score bands: ≥90 ok, 70–89 warn, <70 crit (lower is worse).
const HEALTH_THRESHOLDS: Thresholds = { warn: 89, crit: 69, higherIsWorse: false }

// Issue chips under the health gauge — "3 offline" style. The count is part of
// the translated phrase (word order and plural differ per language).
const ISSUE_TONE: Record<string, BadgeTone> = {
  offline: 'offline',
  high_cpu: 'warn',
  high_mem: 'warn',
  low_disk: 'warn',
  smart: 'crit',
  reboot_pending: 'warn',
  crit_alerts: 'crit',
}

// Fallback tone per event kind when the event carries no severity.
const EVENT_TONE: Record<string, StatusToken> = {
  app_crash: 'warn',
  app_hang: 'warn',
  hw_warning: 'warn',
  smart_fail: 'crit',
}

const FEED_ICON: Record<StatusToken, LucideIcon> = {
  crit: ShieldAlert,
  warn: TriangleAlert,
  info: Info,
  ok: Info,
  offline: Info,
}

export function Overview() {
  const t = useT()
  const { data: ov, isLoading } = useFleetOverview()
  const { data: summary } = useFleetSummary()

  const alertTotal = summary
    ? Object.values(summary.alerts_by_severity ?? {}).reduce((a, b) => a + b, 0)
    : (ov?.recent_alerts ?? []).filter((a) => a.status === 'open' || a.status === 'firing').length
  const critCount = summary?.alerts_by_severity?.critical ?? summary?.alerts_by_severity?.crit ?? 0
  const alertTone = critCount > 0 ? 'crit' : alertTotal > 0 ? 'warn' : 'ok'

  const cpuSpark = useMemo(() => (ov?.history ?? []).map((p) => p.cpu_avg), [ov])
  const trendRows = useMemo(
    () => (ov?.history ?? []).map((p) => ({ t: p.ts, cpu: p.cpu_avg, mem: p.mem_ratio_avg * 100 })),
    [ov],
  )

  return (
    <div>
      <PageHeader
        title={t('nav.overview')}
        description={t('overview.subtitle')}
      />

      {isLoading ? (
        <OverviewSkeleton />
      ) : !ov ? (
        <EmptyState
          icon={HeartPulse}
          title={t('overview.noDataTitle')}
          description={t('overview.noDataBody')}
          className="mb-4"
        />
      ) : (
        <>
          {/* 1 · Hero: health gauge + KPIs */}
          <motion.div
            variants={staggerContainer()}
            initial="hidden"
            animate="show"
            className="grid gap-4 xl:grid-cols-[340px_1fr]"
          >
            <HealthCard ov={ov} />
            <div className="grid grid-cols-2 gap-4">
              <KpiCard
                label={t('overview.kpiOnline')}
                value={ov.totals.online}
                icon={Wifi}
                tone="ok"
                footer={t('overview.kpiOnlineFoot', { total: ov.totals.total, offline: ov.totals.offline, count: ov.totals.total })}
              />
              <KpiCard
                label={t('overview.kpiActive')}
                value={ov.totals.in_use}
                icon={Activity}
                tone="info"
                footer={t('overview.kpiActiveFoot', { idle: ov.totals.idle, locked: ov.totals.locked })}
              />
              <KpiCard
                label={t('overview.kpiCpu')}
                value={ov.avg_cpu}
                format={(n) => formatPercent(n)}
                icon={Cpu}
                tone="accent"
                spark={cpuSpark}
                footer={t('overview.kpiCpuFoot', { value: formatRatioPercent(ov.avg_mem) })}
              />
              <KpiCard
                label={t('overview.kpiAlerts')}
                value={alertTotal}
                icon={BellRing}
                tone={alertTone}
                footer={critCount > 0 ? t('common.criticalCount', { count: critCount }) : alertTotal > 0 ? t('common.review') : t('common.noIssues')}
              />
            </div>
          </motion.div>

          {/* 2 · Fleet trend */}
          <motion.div variants={fadeUp} initial="hidden" animate="show" className="mt-4">
            <Card>
              <CardHeader
                title={t('overview.fleetTitle')}
                description={t('overview.fleetBody')}
              />
              <div className="mt-4">
                <TimeSeriesChart
                  data={trendRows}
                  series={[
                    { key: 'cpu', name: t('common.cpu'), color: chart.cpu },
                    { key: 'mem', name: t('common.ram'), color: chart.mem },
                  ]}
                  spanMs={DAY_MS}
                  height={240}
                  unit="percent"
                  emptyLabel={t('overview.fleetEmpty')}
                />
              </div>
            </Card>
          </motion.div>

          {/* 3 · Attention row */}
          <div className="mt-4 grid gap-4 lg:grid-cols-2 xl:grid-cols-3">
            <AttentionCard title={t('overview.highCpu')} icon={Flame} isEmpty={(ov.top_cpu ?? []).length === 0}>
              {(ov.top_cpu ?? []).slice(0, 5).map((m) => (
                <MetricRow
                  key={m.machine_id}
                  to={`/machines/${m.machine_id}`}
                  title={m.hostname}
                  subtitle={displayUser(m.logon_user)}
                  pct={m.cpu_pct}
                  valueText={formatPercent(m.cpu_pct)}
                  thresholds={CPU_THRESHOLDS}
                />
              ))}
            </AttentionCard>
            <AttentionCard title={t('overview.critRam')} icon={MemoryStick} isEmpty={(ov.critical_mem ?? []).length === 0}>
              {(ov.critical_mem ?? []).slice(0, 5).map((m) => (
                <MetricRow
                  key={m.machine_id}
                  to={`/machines/${m.machine_id}`}
                  title={m.hostname}
                  subtitle={`${displayUser(m.logon_user)} · ${formatBytes(m.mem_used)} / ${formatBytes(m.mem_total)}`}
                  pct={m.mem_ratio * 100}
                  valueText={formatRatioPercent(m.mem_ratio)}
                  thresholds={RAM_THRESHOLDS}
                />
              ))}
            </AttentionCard>
            <AttentionCard title={t('overview.critDisk')} icon={HardDrive} isEmpty={(ov.critical_disk ?? []).length === 0}>
              {(ov.critical_disk ?? []).slice(0, 5).map((d) => (
                <MetricRow
                  key={`${d.machine_id}-${d.mount}`}
                  to={`/machines/${d.machine_id}`}
                  title={`${d.hostname} · ${d.mount}`}
                  subtitle={t('overview.freeSpace', { value: formatBytes(d.free_bytes) })}
                  pct={d.used_pct}
                  valueText={formatPercent(d.used_pct)}
                  thresholds={DISK_USED_THRESHOLDS}
                />
              ))}
            </AttentionCard>
          </div>

          {/* 4 · Recent alerts + events feed */}
          <motion.div variants={fadeUp} initial="hidden" animate="show" className="mt-4">
            <Card>
              <CardHeader
                title={t('overview.eventsTitle')}
                description={t('overview.eventsBody')}
                action={
                  <Link to="/alerts" className="text-[13px] font-medium text-accent hover:underline ring-focus">
                    {t('overview.allAlerts')}
                  </Link>
                }
              />
              <div className="mt-4">
                <EventFeed alerts={ov.recent_alerts ?? []} events={ov.recent_events ?? []} />
              </div>
            </Card>
          </motion.div>
        </>
      )}

      {/* 5 · Usage insights (independent endpoints — render regardless of overview) */}
      <div className="mt-4 grid gap-4 xl:grid-cols-2 [&>*]:min-w-0">
        <BusyHoursCard />
        <TopAppsCard />
      </div>
    </div>
  )
}

/* ── Hero: health ─────────────────────────────────────────────────────────── */

function HealthCard({ ov }: { ov: FleetOverview }) {
  const t = useT()
  const issues = (ov.health.issues ?? []).filter((i) => i.count > 0)
  return (
    <motion.div variants={fadeUp} className="h-full">
      <Card className="flex h-full flex-col">
        <CardHeader title={t('overview.healthTitle')} description={t('overview.healthUpdated', { when: relativeTime(ov.generated_at) })} />
        <div className="flex flex-1 flex-col items-center justify-center py-4">
          <RingGauge
            value={ov.health.score}
            label={t('overview.health')}
            thresholds={HEALTH_THRESHOLDS}
            size={150}
            stroke={12}
            displayValue={formatNumber(Math.round(ov.health.score))}
          />
          <div className="mt-5 flex flex-wrap justify-center gap-1.5">
            {issues.length === 0 ? (
              <Badge tone="ok" icon={<CheckCircle2 className="size-3" aria-hidden />}>
                {t('common.noIssues')}
              </Badge>
            ) : (
              issues.map((i) => {
                const text = translateDynamic(`overview.issue.${i.key}`, { count: i.count }) ?? `${formatNumber(i.count)} ${i.key}`
                return (
                  <Badge key={i.key} tone={ISSUE_TONE[i.key] ?? 'neutral'}>
                    <span className="tabular">{text}</span>
                  </Badge>
                )
              })
            )}
          </div>
        </div>
      </Card>
    </motion.div>
  )
}

/* ── Attention lists ──────────────────────────────────────────────────────── */

function AttentionCard({
  title,
  icon: Icon,
  isEmpty,
  children,
}: {
  title: string
  icon: LucideIcon
  isEmpty: boolean
  children: ReactNode
}) {
  return (
    <motion.div variants={fadeUp} initial="hidden" animate="show">
      <Card className="h-full">
        <div className="flex items-center gap-2">
          <Icon className="size-4 text-text-dim" aria-hidden />
          <h3 className="text-[15px] font-semibold text-text">{title}</h3>
        </div>
        <div className="mt-3">
          {isEmpty ? (
            <OkNote />
          ) : (
            <motion.ul variants={staggerContainer()} initial="hidden" animate="show" className="divide-y divide-border">
              {children}
            </motion.ul>
          )}
        </div>
      </Card>
    </motion.div>
  )
}

/** Small in-card ok state — deliberately quieter than a full EmptyState. */
function OkNote({ text }: { text?: string }) {
  const t = useT()
  return (
    <div className="flex items-center gap-2 rounded-[var(--radius-md)] bg-ok/8 px-3 py-2.5 text-[13px] font-medium text-ok">
      <CheckCircle2 className="size-4 shrink-0" aria-hidden />
      {text ?? t('common.noIssues')}
    </div>
  )
}

function MetricRow({
  to,
  title,
  subtitle,
  pct,
  valueText,
  thresholds,
}: {
  to: string
  title: string
  subtitle?: string
  pct: number
  valueText: string
  thresholds: Thresholds
}) {
  const reduce = useReducedMotion()
  const clamped = Math.max(0, Math.min(100, isFinite(pct) ? pct : 0))
  const color = toneColor[metricTone(clamped, thresholds)]
  return (
    <motion.li variants={fadeUp} className="py-2.5 first:pt-0 last:pb-0">
      <div className="flex items-baseline justify-between gap-2">
        <Link to={to} className="truncate text-[13px] font-semibold text-text hover:text-accent ring-focus">
          {title}
        </Link>
        <span className="tabular shrink-0 text-[13px] font-semibold" style={{ color }}>
          {valueText}
        </span>
      </div>
      {subtitle && <p className="mt-0.5 truncate text-[12px] text-text-dim">{subtitle}</p>}
      <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-wash-2">
        <motion.div
          className="h-full rounded-full"
          style={{ backgroundColor: color }}
          initial={reduce ? { width: `${clamped}%` } : { width: 0 }}
          animate={{ width: `${clamped}%` }}
          transition={reduce ? { duration: 0 } : { duration: 0.38, ease: [0.22, 1, 0.36, 1] }}
        />
      </div>
    </motion.li>
  )
}

/* ── Alerts + events feed ─────────────────────────────────────────────────── */

type FeedItem =
  | { type: 'alert'; ts: string; alert: AlertDTO }
  | { type: 'event'; ts: string; event: OverviewEvent }

function EventFeed({ alerts, events }: { alerts: AlertDTO[]; events: OverviewEvent[] }) {
  const t = useT()
  const items = useMemo<FeedItem[]>(() => {
    const merged: FeedItem[] = [
      ...alerts.map((a) => ({ type: 'alert' as const, ts: a.opened_at, alert: a })),
      ...events.map((e) => ({ type: 'event' as const, ts: e.ts, event: e })),
    ]
    return merged
      .sort((a, b) => new Date(b.ts).getTime() - new Date(a.ts).getTime())
      .slice(0, 10)
  }, [alerts, events])

  if (items.length === 0) return <OkNote text={t('overview.noEvents')} />

  return (
    <motion.ul variants={staggerContainer()} initial="hidden" animate="show" className="divide-y divide-border">
      {items.map((item) => {
        const tone: StatusToken =
          item.type === 'alert'
            ? severityToken(item.alert.severity)
            : item.event.severity
              ? severityToken(item.event.severity)
              : (EVENT_TONE[item.event.kind] ?? 'warn')
        const Icon = FEED_ICON[tone]
        const color = statusColor[tone]
        const title = item.type === 'alert' ? ruleLabel(item.alert.rule) : eventKindLabel(item.event.kind)
        const machineId = item.type === 'alert' ? item.alert.machine_id : item.event.machine_id
        const hostname = item.type === 'alert' ? item.alert.hostname : item.event.hostname
        return (
          <motion.li
            key={item.type === 'alert' ? `a-${item.alert.id}` : `e-${item.event.machine_id}-${item.event.ts}-${item.event.kind}`}
            variants={fadeUp}
            className="flex items-start gap-3 py-3 first:pt-0 last:pb-0"
          >
            <div
              className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-[10px]"
              style={{ backgroundColor: tint(color, 12), color }}
            >
              <Icon className="size-4" aria-hidden />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                <span className="text-[14px] font-semibold text-text">{title}</span>
                <Badge tone={tone === 'ok' || tone === 'offline' ? 'neutral' : tone}>
                  {item.type === 'alert' ? severityLabel(item.alert.severity) : t('overview.event')}
                </Badge>
              </div>
              <p className="mt-0.5 truncate text-[13px] text-text-muted">
                {item.type === 'alert' ? alertMessage(item.alert) : item.event.process_name || '—'}
              </p>
              <div className="mt-1 flex flex-wrap items-center gap-x-2 text-[12px] text-text-dim">
                {hostname && (
                  <Link to={`/machines/${machineId}`} className="font-medium text-text-muted hover:text-accent">
                    {hostname}
                  </Link>
                )}
                {hostname && <span aria-hidden>·</span>}
                <span title={formatDateTime(item.ts)}>{relativeTime(item.ts)}</span>
              </div>
            </div>
          </motion.li>
        )
      })}
    </motion.ul>
  )
}

/* ── Usage insights ───────────────────────────────────────────────────────── */

function BusyHoursCard() {
  const t = useT()
  const { data, isLoading } = useHourlyActivity(7)

  // Adapt dow×hour cells to ActivityHeatmap's shape (weekday 0 = Monday, minutes).
  const cells = useMemo<HeatmapCell[]>(
    () =>
      (data?.cells ?? [])
        .filter((c) => c.dow >= 1 && c.dow <= 7)
        .map((c) => ({ weekday: c.dow - 1, hour: c.hour, active_minutes: c.active_seconds / 60 })),
    [data],
  )

  return (
    <motion.div variants={fadeUp} initial="hidden" animate="show">
      <Card className="h-full">
        <CardHeader
          title={t('overview.busyTitle')}
          description={t('overview.busyBody', { count: data?.days ?? 7 })}
        />
        <div className="mt-4">
          {isLoading ? (
            <Skeleton className="h-48 w-full" />
          ) : cells.length === 0 ? (
            <EmptyState icon={Clock3} title={t('overview.busyEmptyTitle')} description={t('overview.busyEmptyBody')} />
          ) : (
            <ActivityHeatmap data={cells} />
          )}
        </div>
      </Card>
    </motion.div>
  )
}

const BY_TABS: { key: TopAppsBy; labelKey: TKey }[] = [
  { key: 'cpu', labelKey: 'common.cpu' },
  { key: 'mem', labelKey: 'common.ram' },
  { key: 'focus', labelKey: 'overview.byUsage' },
]

function appMetric(app: TopAppEntry, by: TopAppsBy): number {
  if (by === 'cpu') return app.cpu_avg
  if (by === 'mem') return app.mem_avg
  return app.focus_seconds
}

function appMetricText(app: TopAppEntry, by: TopAppsBy): string {
  if (by === 'cpu') return formatPercent(app.cpu_avg, 1)
  if (by === 'mem') return formatBytes(app.mem_avg)
  return formatDuration(app.focus_seconds)
}

function TopAppsCard() {
  const t = useT()
  const [by, setBy] = useState<TopAppsBy>('cpu')
  const { data, isLoading } = useTopApps('24h', by)
  const reduce = useReducedMotion()

  const apps = (data?.apps ?? []).slice(0, 8)
  const max = Math.max(...apps.map((a) => appMetric(a, by)), 1e-9)

  return (
    <motion.div variants={fadeUp} initial="hidden" animate="show">
      <Card className="h-full">
        <CardHeader
          title={t('overview.topAppsTitle')}
          description={t('overview.topAppsBody')}
          action={
            <div className="inline-flex rounded-[var(--radius-md)] bg-bg-elev/70 p-1" role="group" aria-label={t('overview.sortBy')}>
              {BY_TABS.map((x) => (
                <button
                  key={x.key}
                  type="button"
                  onClick={() => setBy(x.key)}
                  aria-pressed={by === x.key}
                  className={cn(
                    'rounded-[9px] px-2.5 py-1 text-[12px] font-medium transition-colors ring-focus',
                    by === x.key ? 'bg-surface-2 text-text' : 'text-text-muted hover:text-text',
                  )}
                >
                  {t(x.labelKey)}
                </button>
              ))}
            </div>
          }
        />
        <div className="mt-4">
          {isLoading ? (
            <div className="space-y-3">
              {Array.from({ length: 6 }).map((_, i) => (
                <Skeleton key={i} className="h-8 w-full" />
              ))}
            </div>
          ) : apps.length === 0 ? (
            <EmptyState icon={AppWindow} title={t('overview.topAppsEmptyTitle')} description={t('overview.topAppsEmptyBody')} />
          ) : (
            <motion.ul
              // key restarts the stagger when the metric changes so bars re-animate.
              key={by}
              variants={staggerContainer()}
              initial="hidden"
              animate="show"
              className="divide-y divide-border"
            >
              {apps.map((app) => {
                const meta = catMeta(app.category)
                const pct = (appMetric(app, by) / max) * 100
                return (
                  <motion.li key={app.name} variants={fadeUp} className="py-2.5 first:pt-0 last:pb-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-[13px] font-semibold text-text">{app.name}</span>
                      <span className="inline-flex shrink-0 items-center gap-1.5 text-[11px] text-text-muted">
                        <span className="size-2 rounded-full" style={{ backgroundColor: meta.color }} aria-hidden />
                        {meta.label}
                      </span>
                      <span className="tabular ml-auto shrink-0 text-[13px] font-semibold text-text">
                        {appMetricText(app, by)}
                      </span>
                    </div>
                    <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-wash-2">
                      <motion.div
                        className="h-full rounded-full"
                        style={{ backgroundColor: meta.color }}
                        initial={reduce ? { width: `${pct}%` } : { width: 0 }}
                        animate={{ width: `${pct}%` }}
                        transition={reduce ? { duration: 0 } : { duration: 0.38, ease: [0.22, 1, 0.36, 1] }}
                      />
                    </div>
                    <div className="tabular mt-1 text-[12px] text-text-dim">
                      {t('overview.appFoot', { count: app.machines, duration: formatDuration(app.focus_seconds) })}
                    </div>
                  </motion.li>
                )
              })}
            </motion.ul>
          )}
        </div>
      </Card>
    </motion.div>
  )
}

/* ── Loading skeleton ─────────────────────────────────────────────────────── */

function OverviewSkeleton() {
  return (
    <div>
      <div className="grid gap-4 xl:grid-cols-[340px_1fr]">
        <div className="card p-5">
          <Skeleton className="h-4 w-32" />
          <div className="mt-6 grid place-items-center">
            <Skeleton className="size-36 rounded-full" />
          </div>
          <Skeleton className="mx-auto mt-6 h-5 w-40" />
        </div>
        <div className="grid grid-cols-2 gap-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <SkeletonCard key={i} />
          ))}
        </div>
      </div>
      <div className="card mt-4 p-5">
        <Skeleton className="h-4 w-52" />
        <Skeleton className="mt-4 h-56 w-full" />
      </div>
      <div className="mt-4 grid gap-4 lg:grid-cols-2 xl:grid-cols-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <SkeletonCard key={i} />
        ))}
      </div>
    </div>
  )
}
