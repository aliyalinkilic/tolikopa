import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { ChevronDown, Clock, Info, ShieldAlert, TriangleAlert } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { PageHeader } from '@/components/AppShell'
import { Card } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Select } from '@/components/ui/Select'
import { RangeSelector } from '@/components/RangeSelector'
import { DataTable } from '@/components/DataTable'
import type { Column } from '@/components/DataTable'
import { EmptyState } from '@/components/EmptyState'
import { SkeletonText } from '@/components/Skeleton'
import { useAckAlert, useAlerts, useFleet, useFleetSummary } from '@/hooks/queries'
import { useRange } from '@/hooks/useRange'
import { useUrlState } from '@/hooks/useUrlState'
import { alertStatusLabel, ruleLabel, severityLabel, severityToken, SEVERITY_ORDER } from '@/lib/labels'
import type { StatusToken } from '@/lib/labels'
import { formatDateTime, formatSpan, relativeTime } from '@/lib/format'
import type { AlertDTO } from '@/lib/types'
import { useT } from '@/i18n'
import { alertMessage } from '@/lib/alertText'
import { chart, tint } from '@/lib/theme'
import { cn } from '@/lib/cn'

const severityIcon: Record<StatusToken, LucideIcon> = {
  crit: ShieldAlert,
  warn: TriangleAlert,
  info: Info,
  ok: Info,
  offline: Info,
}

/** Every rule the engine evaluates (server/internal/alerts.Rules). */
const RULES = ['offline', 'cpu_high', 'mem_high', 'disk_low', 'smart_fail', 'crash_storm'] as const

type GroupBy = '' | 'rule' | 'machine'

function severityRank(sev: string): number {
  const x = severityToken(sev)
  const idx = SEVERITY_ORDER.indexOf(x)
  return idx === -1 ? 99 : idx
}

const isOpenStatus = (s: string) => s === 'open' || s === 'firing'
const isActive = (a: AlertDTO) => a.status !== 'resolved'

/** The server stamps detail.resolved_reason when it closes an alert itself. */
function resolvedReason(a: AlertDTO): string | null {
  const r = a.params?.resolved_reason ?? a.detail?.resolved_reason
  return typeof r === 'string' ? r : null
}

export function Alerts() {
  const t = useT()
  const { key: rangeKey, setKey: setRange, range, win, custom, setCustom } = useRange('7d')
  const [status, setStatus] = useUrlState('status', '')
  const [severity, setSeverity] = useUrlState('sev', '')
  const [rule, setRule] = useUrlState('rule', '')
  const [tag, setTag] = useUrlState('tag', '')
  const [groupBy, setGroupBy] = useUrlState('group', '')
  const statusOptions = [
    { value: '', label: t('alertsPage.allStatuses') },
    { value: 'open', label: t('alertStatus.open') },
    { value: 'ack', label: t('alertStatus.ack') },
    { value: 'resolved', label: t('alertStatus.resolved') },
  ]
  const severityOptions = [
    { value: '', label: t('alertsPage.allSeverities') },
    { value: 'crit', label: t('severity.crit') },
    { value: 'warn', label: t('severity.warn') },
    { value: 'info', label: t('severity.info') },
  ]
  const ruleOptions = [{ value: '', label: t('alertsPage.allRules') }, ...RULES.map((r) => ({ value: r, label: ruleLabel(r) }))]
  const groupOptions = [
    { value: '', label: t('alertsPage.groupNone') },
    { value: 'rule', label: t('alertsPage.groupRule') },
    { value: 'machine', label: t('alertsPage.groupMachine') },
  ]

  const { data: fleet } = useFleet()
  const tagOptions = useMemo(() => {
    const seen = new Map<string, string>()
    for (const m of fleet ?? []) for (const x of m.tags ?? []) if (!seen.has(x.toLowerCase())) seen.set(x.toLowerCase(), x)
    const list = [...seen.values()].sort((a, b) => a.localeCompare(b))
    return [{ value: '', label: t('alertsPage.allTags') }, ...list.map((x) => ({ value: x, label: x }))]
  }, [fleet, t])

  const { data: summary } = useFleetSummary()
  const { data, isLoading } = useAlerts({
    status: status || undefined,
    rule: rule || undefined,
    tag: tag || undefined,
    from: win.from,
    to: win.to,
  })
  const ack = useAckAlert()

  const rows = useMemo(() => {
    let list = data ?? []
    if (severity) list = list.filter((a) => severityToken(a.severity) === severity)
    return list
  }, [data, severity])

  const bySev = summary?.alerts_by_severity ?? {}
  const critCount = bySev.critical ?? bySev.crit ?? 0
  const warnCount = bySev.warning ?? bySev.warn ?? 0
  const infoCount = bySev.info ?? bySev.bilgi ?? 0

  const columns: Column<AlertDTO>[] = [
    {
      key: 'severity',
      header: t('alertsPage.severity'),
      sortable: true,
      sortValue: (a) => severityRank(a.severity),
      render: (a) => {
        const tone = severityToken(a.severity)
        const Icon = severityIcon[tone]
        return (
          <Badge tone={tone} icon={<Icon className="size-3" aria-hidden />}>
            {severityLabel(a.severity)}
          </Badge>
        )
      },
    },
    {
      key: 'rule',
      header: t('alertsPage.rule'),
      sortable: true,
      sortValue: (a) => ruleLabel(a.rule),
      render: (a) => <span className="font-medium text-text">{ruleLabel(a.rule)}</span>,
    },
    {
      key: 'machine',
      header: t('common.machine'),
      sortable: true,
      sortValue: (a) => a.hostname ?? a.machine_id,
      render: (a) => (
        <span className="inline-flex flex-wrap items-center gap-1.5">
          <Link to={`/machines/${a.machine_id}`} className="text-text-muted hover:text-accent ring-focus">
            {a.hostname ?? a.machine_id}
          </Link>
          {a.work_hours && a.rule === 'offline' && <WorkHoursBadge />}
        </span>
      ),
    },
    {
      key: 'message',
      header: t('alertsPage.message'),
      render: (a) => <span className="block max-w-[320px] truncate text-text-muted" title={alertMessage(a)}>{alertMessage(a)}</span>,
    },
    {
      key: 'opened',
      header: t('alertsPage.opened'),
      align: 'right',
      sortable: true,
      sortValue: (a) => new Date(a.opened_at).getTime(),
      render: (a) => (
        <span className="tabular whitespace-nowrap text-text-muted" title={formatDateTime(a.opened_at)}>
          {relativeTime(a.opened_at)}
        </span>
      ),
    },
    {
      key: 'duration',
      header: t('alertsPage.duration'),
      align: 'right',
      sortable: true,
      sortValue: (a) => (a.resolved_at ? new Date(a.resolved_at).getTime() : Date.now()) - new Date(a.opened_at).getTime(),
      render: (a) => (
        <span
          className="tabular whitespace-nowrap text-text-muted"
          title={a.resolved_at ? `${t('alertsPage.resolvedAt')}: ${formatDateTime(a.resolved_at)}` : undefined}
        >
          {formatSpan(a.opened_at, a.resolved_at ?? null)}
        </span>
      ),
    },
    {
      key: 'status',
      header: t('common.status'),
      align: 'right',
      render: (a) => {
        if (isOpenStatus(a.status)) {
          return (
            <Button size="sm" variant="secondary" loading={ack.isPending && ack.variables === a.id} onClick={() => ack.mutate(a.id)}>
              {t('ui.alertList.ack')}
            </Button>
          )
        }
        if (a.status === 'resolved' && resolvedReason(a) === 'work_hours_end') {
          return (
            <Badge tone="neutral" icon={<Clock className="size-3" aria-hidden />} title={t('alertsPage.resolvedWorkHoursTitle')}>
              {t('alertsPage.resolvedWorkHours')}
            </Badge>
          )
        }
        return <Badge tone="neutral">{alertStatusLabel(a.status)}</Badge>
      },
    },
  ]

  return (
    <div>
      <PageHeader title={t('nav.alerts')} description={t('alertsPage.subtitle')} />

      {/* Severity summary strip (open alerts) */}
      <div className="mb-6 grid grid-cols-3 gap-4">
        <SummaryTile tone="crit" icon={ShieldAlert} label={t('severity.crit')} count={critCount} />
        <SummaryTile tone="warn" icon={TriangleAlert} label={t('severity.warn')} count={warnCount} />
        <SummaryTile tone="info" icon={Info} label={t('severity.info')} count={infoCount} />
      </div>

      <Card padded={false}>
        <div className="flex flex-col gap-3 border-b border-border p-4 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex flex-wrap gap-2">
            <div className="w-40">
              <Select options={statusOptions} value={status} onChange={(e) => setStatus(e.target.value)} aria-label={t('common.status')} />
            </div>
            <div className="w-44">
              <Select options={severityOptions} value={severity} onChange={(e) => setSeverity(e.target.value)} aria-label={t('alertsPage.severity')} />
            </div>
            <div className="w-44">
              <Select options={ruleOptions} value={rule} onChange={(e) => setRule(e.target.value)} aria-label={t('alertsPage.rule')} />
            </div>
            <div className="w-44">
              <Select options={tagOptions} value={tag} onChange={(e) => setTag(e.target.value)} aria-label={t('alertsPage.tag')} />
            </div>
            <div className="w-40">
              <Select options={groupOptions} value={groupBy} onChange={(e) => setGroupBy(e.target.value)} aria-label={t('alertsPage.groupBy')} />
            </div>
          </div>
          <RangeSelector value={rangeKey} onChange={setRange} custom={custom} onCustomChange={setCustom} />
        </div>
        <div className="p-4">
          {isLoading ? (
            <SkeletonText lines={8} />
          ) : groupBy === 'rule' || groupBy === 'machine' ? (
            <GroupedAlerts
              rows={rows}
              groupBy={groupBy as GroupBy}
              columns={columns}
              emptyLabel={t('alertsPage.empty', { range: range.label })}
            />
          ) : (
            <DataTable
              columns={columns}
              rows={rows}
              getRowKey={(a) => String(a.id)}
              initialSort={{ key: 'opened', dir: 'desc' }}
              emptyLabel={t('alertsPage.empty', { range: range.label })}
              className="border-0"
              maxHeight={640}
              pageSize={20}
            />
          )}
        </div>
      </Card>
    </div>
  )
}

function WorkHoursBadge() {
  const t = useT()
  return (
    <Badge tone="info" icon={<Clock className="size-3" aria-hidden />} title={t('alertsPage.workHoursTitle')}>
      {t('alertsPage.workHoursBadge')}
    </Badge>
  )
}

interface Group {
  key: string
  label: string
  rows: AlertDTO[]
  active: number
  worst: number
  workHours: boolean
}

/** Alerts grouped by rule or machine: groups with the most active alerts first. */
function GroupedAlerts({
  rows,
  groupBy,
  columns,
  emptyLabel,
}: {
  rows: AlertDTO[]
  groupBy: GroupBy
  columns: Column<AlertDTO>[]
  emptyLabel: string
}) {
  const t = useT()
  const [open, setOpen] = useState<Set<string>>(new Set())

  const groups = useMemo(() => {
    const m = new Map<string, Group>()
    for (const a of rows) {
      const key = groupBy === 'rule' ? a.rule : a.machine_id
      const label = groupBy === 'rule' ? ruleLabel(a.rule) : (a.hostname ?? a.machine_id)
      let g = m.get(key)
      if (!g) {
        g = { key, label, rows: [], active: 0, worst: 99, workHours: false }
        m.set(key, g)
      }
      g.rows.push(a)
      if (isActive(a)) {
        g.active++
        g.worst = Math.min(g.worst, severityRank(a.severity))
      }
      if (a.work_hours) g.workHours = true
    }
    return [...m.values()].sort((a, b) => b.active - a.active || a.worst - b.worst || b.rows.length - a.rows.length)
  }, [rows, groupBy])

  if (groups.length === 0) return <EmptyState icon={Info} title={emptyLabel} />

  // Small result sets open fully; otherwise the admin expands what they need.
  const expandedByDefault = groups.length <= 3
  const isExpanded = (k: string) => (expandedByDefault ? !open.has(k) : open.has(k))
  const toggle = (k: string) =>
    setOpen((s) => {
      const n = new Set(s)
      if (n.has(k)) n.delete(k)
      else n.add(k)
      return n
    })
  const cols = columns.filter((c) => c.key !== (groupBy === 'rule' ? 'rule' : 'machine'))

  return (
    <ul className="space-y-3">
      {groups.map((g) => {
        const expanded = isExpanded(g.key)
        const worst = SEVERITY_ORDER[g.worst]
        return (
          <li key={g.key} className="rounded-[var(--radius-md)] border border-border">
            <button
              type="button"
              onClick={() => toggle(g.key)}
              aria-expanded={expanded}
              className="flex w-full flex-wrap items-center gap-2 px-4 py-3 text-left ring-focus"
            >
              <ChevronDown className={cn('size-4 shrink-0 text-text-dim transition-transform', !expanded && '-rotate-90')} aria-hidden />
              <span className="font-semibold text-text">{g.label}</span>
              {groupBy === 'machine' && g.workHours && <WorkHoursBadge />}
              <span className="ml-auto flex flex-wrap items-center gap-2">
                {g.active > 0 && worst && <Badge tone={worst}>{t('alertsPage.openCount', { count: g.active })}</Badge>}
                <Badge tone="neutral">{t('alertsPage.groupCount', { count: g.rows.length })}</Badge>
              </span>
            </button>
            {expanded && (
              <div className="border-t border-border p-3">
                <DataTable
                  columns={cols}
                  rows={g.rows}
                  getRowKey={(a) => String(a.id)}
                  initialSort={{ key: 'opened', dir: 'desc' }}
                  className="border-0"
                  pageSize={10}
                />
              </div>
            )}
          </li>
        )
      })}
    </ul>
  )
}

function SummaryTile({ tone, icon: Icon, label, count }: { tone: StatusToken; icon: LucideIcon; label: string; count: number }) {
  const color = tone === 'crit' ? chart.crit : tone === 'warn' ? chart.warn : chart.info
  return (
    <Card className="relative overflow-hidden">
      <span className="absolute inset-y-0 left-0 w-1" style={{ backgroundColor: color }} aria-hidden />
      <div className="flex items-center justify-between">
        <div>
          <p className="text-[12px] font-medium uppercase tracking-[0.1em] text-text-dim">{label}</p>
          <p className="tabular mt-1 text-[26px] font-bold text-text">{count}</p>
        </div>
        <div className="grid size-10 place-items-center rounded-[12px]" style={{ backgroundColor: tint(color, 12), color }}>
          <Icon className="size-5" aria-hidden />
        </div>
      </div>
    </Card>
  )
}
