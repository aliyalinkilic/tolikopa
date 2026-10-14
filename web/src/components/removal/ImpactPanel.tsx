import { ArrowDownRight, ArrowUpRight, Cpu, Gauge, Info, MemoryStick, Minus } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { Link } from 'react-router-dom'
import { Badge } from '@/components/ui/Badge'
import { Card, CardHeader } from '@/components/ui/Card'
import { Select } from '@/components/ui/Select'
import { DataTable } from '@/components/DataTable'
import type { Column } from '@/components/DataTable'
import { EmptyState } from '@/components/EmptyState'
import { SkeletonCard } from '@/components/Skeleton'
import { cn } from '@/lib/cn'
import { formatBytes, formatDate, formatFixed, formatPercent } from '@/lib/format'
import type {
  RemediationImpactItem, RemediationImpactMachine, RemediationImpactResponse, RemediationImpactWindow,
} from '@/lib/types'
import { translate, useT } from '@/i18n'
import { Trans } from '@/i18n/rich'
import { catalogName } from '@/i18n/catalog'

interface ImpactPanelProps {
  data: RemediationImpactResponse | undefined
  isLoading: boolean
  isError: boolean
  days: number
  onDaysChange: (days: number) => void
}

const DAY_CHOICES = [3, 7, 14] as const

/** Change below this many percentage points is reported as "no change". */
const NOISE_PP = 0.5

type Metric = 'cpu' | 'mem'

/** Both metrics in percent (cpu is 0..100 already, mem is a 0..1 ratio). */
function pct(w: RemediationImpactWindow, metric: Metric, side: 'before' | 'after'): number | null {
  const v = metric === 'cpu'
    ? (side === 'before' ? w.cpu_before : w.cpu_after)
    : (side === 'before' ? w.mem_before : w.mem_after)
  if (v === null || v === undefined) return null
  return metric === 'cpu' ? v : v * 100
}

function delta(w: RemediationImpactWindow, metric: Metric): number | null {
  const b = pct(w, metric, 'before')
  const a = pct(w, metric, 'after')
  return b === null || a === null ? null : a - b
}

function fmtPP(pp: number): string {
  const sign = pp > 0 ? '+' : pp < 0 ? '−' : '±'
  return translate('removal.impact.points', { value: `${sign}${formatFixed(Math.abs(pp), 1)}` })
}

/** Lower usage is the good direction for both metrics. Icon + words carry the
 *  meaning; the tone only reinforces it. */
function DeltaBadge({ pp }: { pp: number | null }) {
  if (pp === null) return <span className="text-text-dim">—</span>
  if (Math.abs(pp) < NOISE_PP) {
    return (
      <Badge tone="neutral" icon={<Minus className="size-3" aria-hidden />}>
        {fmtPP(pp)}
      </Badge>
    )
  }
  const down = pp < 0
  const Icon = down ? ArrowDownRight : ArrowUpRight
  return (
    <Badge tone={down ? 'ok' : 'warn'} icon={<Icon className="size-3" aria-hidden />}>
      {fmtPP(pp)}
    </Badge>
  )
}

/**
 * "Impact of removal" — machine usage in the N active hours-days before a
 * machine's first successful removal against the N days after its last one,
 * next to a control group of machines that had nothing removed. The honest
 * number is the difference of the two deltas; the page says so.
 */
export function ImpactPanel({ data, isLoading, isError, days, onDaysChange }: ImpactPanelProps) {
  const t = useT()
  const header = (
    <CardHeader
      title={t('removal.impact.title')}
      description={t('removal.impact.body')}
      action={
        <div className="w-52">
          <Select
            options={DAY_CHOICES.map((d) => ({ value: String(d), label: t('removal.impact.windowOption', { count: d }) }))}
            value={String(days)}
            onChange={(e) => onDaysChange(Number(e.target.value))}
            aria-label={t('removal.impact.windowLabel')}
          />
        </div>
      }
      className="p-5 pb-4"
    />
  )

  if (isLoading) {
    return (
      <Card padded={false}>
        {header}
        <div className="grid gap-3 px-5 pb-5 sm:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => <SkeletonCard key={i} />)}
        </div>
      </Card>
    )
  }
  if (isError || !data) {
    return (
      <Card padded={false}>
        {header}
        <div className="px-5 pb-5">
          <EmptyState
            icon={Gauge}
            title={t('removal.impact.loadFailedTitle')}
            description={t('removal.failures.notSupported')}
          />
        </div>
      </Card>
    )
  }
  if (data.machines.length === 0) {
    return (
      <Card padded={false}>
        {header}
        <div className="px-5 pb-5">
          <EmptyState
            icon={Gauge}
            title={t('removal.impact.noneTitle')}
            description={t('removal.impact.noneBody')}
          />
        </div>
      </Card>
    )
  }

  const incomplete = data.machines.filter((m) => !m.complete).length

  return (
    <div className="space-y-4">
      <Card padded={false}>
        {header}
        <div className="grid gap-3 px-5 pb-4 sm:grid-cols-3">
          <MetricTile icon={Cpu} label={t('removal.impact.avgCpu')} metric="cpu" data={data} />
          <MetricTile icon={MemoryStick} label={t('removal.impact.avgRam')} metric="mem" data={data} />
          <Card>
            <div className="flex items-center gap-2">
              <Gauge className="size-4 text-accent" aria-hidden />
              <p className="text-[11px] font-semibold uppercase tracking-[0.08em] text-text-dim">{t('removal.impact.coverage')}</p>
            </div>
            <p className="tabular mt-2 text-[26px] font-semibold leading-none text-text">
              {data.fleet_machines}
              <span className="text-[15px] font-medium text-text-dim"> / {data.machines.length}</span>
            </p>
            <p className="mt-2 text-[12px] leading-[1.45] text-text-muted">
              {t('removal.impact.coverageBody', { treated: data.machines.length, included: data.fleet_machines, hours: data.min_hours, control: data.control_machines })}
            </p>
          </Card>
        </div>
        <p className="mx-5 mb-5 flex items-start gap-2 rounded-[var(--radius-md)] border border-border bg-surface-2/40 px-3.5 py-2.5 text-[12px] leading-[1.5] text-text-muted">
          <Info className="mt-px size-4 shrink-0 text-text-dim" aria-hidden />
          <span>
            <Trans k="removal.impact.netNote" tags={{ b: (c) => <strong className="font-medium text-text">{c}</strong> }} />
            {incomplete > 0 && ` ${t('removal.impact.incomplete', { count: incomplete })}`}
          </span>
        </p>
      </Card>

      <ItemsTable items={data.items} />
      <MachinesTable machines={data.machines} minHours={data.min_hours} />
    </div>
  )
}

function MetricTile({
  icon: Icon, label, metric, data,
}: { icon: LucideIcon; label: string; metric: Metric; data: RemediationImpactResponse }) {
  const t = useT()
  const before = pct(data.fleet, metric, 'before')
  const after = pct(data.fleet, metric, 'after')
  const treated = delta(data.fleet, metric)
  const control = delta(data.control, metric)
  const net = treated !== null && control !== null ? treated - control : null

  return (
    <Card>
      <div className="flex items-center gap-2">
        <Icon className="size-4 text-accent" aria-hidden />
        <p className="text-[11px] font-semibold uppercase tracking-[0.08em] text-text-dim">{label}</p>
      </div>
      <p className="tabular mt-2 flex items-baseline gap-2 text-[26px] font-semibold leading-none text-text">
        <span className="text-text-muted">{before === null ? '—' : formatPercent(before, 1)}</span>
        <span className="text-[16px] text-text-dim" aria-label={t('removal.impact.after')}>→</span>
        <span>{after === null ? '—' : formatPercent(after, 1)}</span>
      </p>
      <dl className="mt-3 space-y-1.5 text-[12px]">
        <div className="flex items-center justify-between gap-2">
          <dt className="text-text-muted">{t('removal.impact.treated')}</dt>
          <dd><DeltaBadge pp={treated} /></dd>
        </div>
        <div className="flex items-center justify-between gap-2">
          <dt className="text-text-muted">{t('removal.impact.control')}</dt>
          <dd><DeltaBadge pp={control} /></dd>
        </div>
        <div className="flex items-center justify-between gap-2 border-t border-border pt-1.5">
          <dt className="font-medium text-text">{t('removal.impact.net')}</dt>
          <dd><DeltaBadge pp={net} /></dd>
        </div>
      </dl>
    </Card>
  )
}

function ItemsTable({ items }: { items: RemediationImpactItem[] }) {
  const t = useT()
  const columns: Column<RemediationImpactItem>[] = [
    {
      key: 'item',
      header: t('removal.impact.app'),
      sortable: true,
      sortValue: (r) => catalogName(r.item_id, r.display_name),
      render: (r) => <span className="font-medium text-text">{catalogName(r.item_id, r.display_name)}</span>,
    },
    {
      key: 'machines',
      header: t('removal.impact.removedFrom'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.machines,
      render: (r) => <span className="tabular text-text-muted">{r.machines}</span>,
    },
    {
      key: 'seen',
      header: t('removal.impact.seenBefore'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.proc_machines_before,
      render: (r) => <span className="tabular text-text-muted">{t('common.machines', { count: r.proc_machines_before })}</span>,
    },
    {
      key: 'cpu',
      header: t('removal.impact.ownCpu'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.proc_cpu_before ?? -1,
      render: (r) => (
        <span className="tabular text-text">{r.proc_cpu_before === null ? '—' : formatPercent(r.proc_cpu_before, 1)}</span>
      ),
    },
    {
      key: 'mem',
      header: t('removal.impact.ownMem'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.proc_mem_before ?? -1,
      render: (r) => (
        <span className="tabular text-text">{r.proc_mem_before === null ? '—' : formatBytes(r.proc_mem_before)}</span>
      ),
    },
    {
      key: 'after',
      header: t('removal.impact.afterCol'),
      align: 'right',
      render: (r) =>
        r.proc_machines_after > 0 ? (
          <Badge tone="warn" title={t('removal.impact.cameBackTitle')}>
            {t('removal.impact.cameBack', { count: r.proc_machines_after })}
          </Badge>
        ) : (
          <Badge tone="ok">{t('removal.impact.notSeen')}</Badge>
        ),
    },
  ]

  return (
    <Card padded={false}>
      <CardHeader
        title={t('removal.impact.perAppTitle')}
        description={t('removal.impact.perAppBody')}
        className="p-5 pb-4"
      />
      <div className="px-5 pb-5">
        <DataTable
          columns={columns}
          rows={items}
          getRowKey={(r) => r.item_id}
          initialSort={{ key: 'machines', dir: 'desc' }}
          className="border-0"
        />
      </div>
    </Card>
  )
}

function MachinesTable({ machines, minHours }: { machines: RemediationImpactMachine[]; minHours: number }) {
  const t = useT()
  const thin = (m: RemediationImpactMachine) => m.hours_before < minHours || m.hours_after < minHours
  const cell = (m: RemediationImpactMachine, metric: Metric) => {
    const b = pct(m, metric, 'before')
    const a = pct(m, metric, 'after')
    return (
      <div className="flex items-center justify-end gap-2">
        <span className="tabular whitespace-nowrap text-text-muted">
          {b === null ? '—' : formatPercent(b, 1)} → <span className="text-text">{a === null ? '—' : formatPercent(a, 1)}</span>
        </span>
        <DeltaBadge pp={delta(m, metric)} />
      </div>
    )
  }

  const columns: Column<RemediationImpactMachine>[] = [
    {
      key: 'machine',
      header: t('common.machine'),
      sortable: true,
      sortValue: (m) => m.hostname ?? m.machine_id,
      render: (m) => (
        <div className="min-w-0">
          <Link to={`/machines/${m.machine_id}`} className="truncate font-medium text-accent hover:underline ring-focus">
            {m.hostname || m.machine_id}
          </Link>
          <p className="text-[12px] text-text-dim">
            {t('removal.impact.appsOn', { count: m.items_removed, date: formatDate(m.last_removal) })}
          </p>
        </div>
      ),
    },
    {
      key: 'cpu',
      header: t('common.cpu'),
      align: 'right',
      sortable: true,
      sortValue: (m) => delta(m, 'cpu') ?? 0,
      render: (m) => cell(m, 'cpu'),
    },
    {
      key: 'mem',
      header: t('common.ram'),
      align: 'right',
      sortable: true,
      sortValue: (m) => delta(m, 'mem') ?? 0,
      render: (m) => cell(m, 'mem'),
    },
    {
      key: 'hours',
      header: t('removal.impact.activeHours'),
      align: 'right',
      render: (m) => (
        <div className="flex flex-col items-end gap-1">
          <span className={cn('tabular text-[12px]', thin(m) ? 'text-warn' : 'text-text-muted')}>
            {t('removal.impact.hoursBeforeAfter', { before: m.hours_before, after: m.hours_after })}
          </span>
          {!m.complete && <Badge tone="info">{t('removal.impact.windowOpen')}</Badge>}
        </div>
      ),
    },
  ]

  return (
    <Card padded={false}>
      <CardHeader
        title={t('removal.impact.perMachineTitle')}
        description={t('removal.impact.perMachineBody', { hours: minHours })}
        className="p-5 pb-4"
      />
      <div className="px-5 pb-5">
        <DataTable
          columns={columns}
          rows={machines}
          getRowKey={(m) => m.machine_id}
          initialSort={{ key: 'cpu', dir: 'asc' }}
          pageSize={15}
          className="border-0"
        />
      </div>
    </Card>
  )
}
