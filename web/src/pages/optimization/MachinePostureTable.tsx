import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Badge } from '@/components/ui/Badge'
import type { BadgeTone } from '@/components/ui/Badge'
import { Card, CardHeader } from '@/components/ui/Card'
import { DataTable } from '@/components/DataTable'
import type { Column } from '@/components/DataTable'
import { FilterChip } from '@/components/FilterChip'
import { SkeletonText } from '@/components/Skeleton'
import { relativeTime } from '@/lib/format'
import type { RemediationMachinePosture } from '@/lib/types'
import { useT } from '@/i18n'
import type { TKey } from '@/i18n'

export type MachineFilter = 'all' | 'blocked' | 'pending' | 'failures' | 'stale' | 'silent' | 'excluded'

const FILTERS: MachineFilter[] = ['all', 'blocked', 'pending', 'failures', 'stale', 'silent', 'excluded']

/** "Cannot act": it reported, and its local switches do not let it actuate. */
export function isBlocked(m: RemediationMachinePosture): boolean {
  return !!m.reported_at && m.allowed !== true
}

const matches: Record<MachineFilter, (m: RemediationMachinePosture) => boolean> = {
  all: () => true,
  blocked: isBlocked,
  pending: (m) => m.pending_apps + m.pending_startup > 0,
  failures: (m) => m.open_failures > 0,
  stale: (m) => m.catalog_stale,
  silent: (m) => !m.reported_at,
  excluded: (m) => m.excluded,
}

interface Props {
  machines: RemediationMachinePosture[] | undefined
  isLoading: boolean
}

/** Every Windows machine's hygiene posture, filterable by "why is nothing happening here?". */
export function MachinePostureTable({ machines, isLoading }: Props) {
  const t = useT()
  const navigate = useNavigate()
  const [filter, setFilter] = useState<MachineFilter>('all')
  const all = useMemo(() => machines ?? [], [machines])
  const rows = useMemo(() => all.filter(matches[filter]), [all, filter])
  const counts = useMemo(() => {
    const c = {} as Record<MachineFilter, number>
    for (const f of FILTERS) c[f] = all.filter(matches[f]).length
    return c
  }, [all])

  const columns: Column<RemediationMachinePosture>[] = [
    {
      key: 'hostname',
      header: t('optimization.machines.colMachine'),
      sortable: true,
      sortValue: (r) => r.hostname,
      render: (r) => (
        <span className="flex flex-wrap items-center gap-2">
          <span className="font-medium text-text">{r.hostname || r.machine_id}</span>
          {r.excluded && <Badge tone="neutral">{t('optimization.machines.excludedBadge')}</Badge>}
        </span>
      ),
    },
    {
      key: 'switches',
      header: t('optimization.machines.colSwitches'),
      render: (r) =>
        r.reported_at ? (
          <span className="flex flex-wrap gap-1">
            <SwitchBadge name="optimization.machines.swApps" value={r.allow_apps} />
            <SwitchBadge name="optimization.machines.swStartup" value={r.allow_startup} />
            <SwitchBadge name="optimization.machines.swEnforce" value={r.enforce} />
          </span>
        ) : (
          <span className="text-[12px] text-text-dim">{t('optimization.machines.policyNone')}</span>
        ),
    },
    {
      key: 'policy',
      header: t('optimization.machines.colPolicy'),
      sortable: true,
      sortValue: (r) => (r.reported_at ? (r.policy_current ? 2 : 1) : 0),
      render: (r) =>
        !r.reported_at ? (
          <Badge tone="offline">{t('optimization.machines.policyNone')}</Badge>
        ) : (
          <span className="flex flex-wrap gap-1">
            <Badge tone={r.policy_current ? 'ok' : 'warn'}>
              {r.policy_current ? t('optimization.machines.policyCurrent') : t('optimization.machines.policyBehind')}
            </Badge>
            {r.catalog_stale && <Badge tone="warn">{t('optimization.machines.catalogStale')}</Badge>}
          </span>
        ),
    },
    {
      key: 'pending',
      header: t('optimization.machines.colPending'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.pending_apps + r.pending_startup,
      render: (r) =>
        r.pending_apps + r.pending_startup === 0 ? (
          <span className="tabular text-text-dim">0</span>
        ) : (
          <span className="tabular text-warn">
            {t('optimization.machines.pendingValue', { apps: r.pending_apps, startup: r.pending_startup })}
          </span>
        ),
    },
    {
      key: 'failures',
      header: t('optimization.machines.colFailures'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.open_failures,
      render: (r) => (
        <span className={r.open_failures > 0 ? 'tabular font-medium text-crit' : 'tabular text-text-dim'}>
          {r.open_failures}
        </span>
      ),
    },
    {
      key: 'reported',
      header: t('optimization.machines.colLastReport'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.reported_at ?? '',
      render: (r) => (
        <span className="tabular text-[12px] text-text-muted">{r.reported_at ? relativeTime(r.reported_at) : '—'}</span>
      ),
    },
  ]

  return (
    <Card padded={false}>
      <CardHeader
        title={t('optimization.machines.title')}
        description={t('optimization.machines.body')}
        className="p-5 pb-3"
      />
      <div className="flex flex-wrap gap-1.5 px-5 pb-3" role="group" aria-label={t('optimization.machines.filterLabel')}>
        {FILTERS.map((f) => (
          <FilterChip key={f} active={filter === f} onClick={() => setFilter(f)} count={counts[f]}>
            {t(`optimization.machines.filter.${f}` as TKey)}
          </FilterChip>
        ))}
      </div>
      <div className="px-5 pb-5">
        {isLoading ? (
          <SkeletonText lines={6} />
        ) : (
          <DataTable
            columns={columns}
            rows={rows}
            getRowKey={(r) => r.machine_id}
            onRowClick={(r) => navigate(`/machines/${encodeURIComponent(r.machine_id)}`)}
            initialSort={{ key: 'pending', dir: 'desc' }}
            emptyLabel={t('optimization.machines.empty')}
            pageSize={15}
            className="border-0"
          />
        )}
      </div>
    </Card>
  )
}

function SwitchBadge({ name, value }: { name: TKey; value: boolean | null }) {
  const t = useT()
  const label = t(name)
  const tone: BadgeTone = value === null ? 'offline' : value ? 'ok' : 'warn'
  const title =
    value === null
      ? t('optimization.machines.swUnknown', { name: label })
      : value
        ? t('optimization.machines.swOn', { name: label })
        : t('optimization.machines.swOff', { name: label })
  return (
    <Badge tone={tone} dot title={title}>
      <span className="sr-only">{title}</span>
      <span aria-hidden>{label}</span>
    </Badge>
  )
}
