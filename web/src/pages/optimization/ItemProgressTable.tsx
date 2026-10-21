import { useMemo } from 'react'
import { ListChecks } from 'lucide-react'
import { Badge } from '@/components/ui/Badge'
import { Card, CardHeader } from '@/components/ui/Card'
import { DataTable } from '@/components/DataTable'
import type { Column } from '@/components/DataTable'
import { SkeletonText } from '@/components/Skeleton'
import { itemKey } from '@/lib/remediation'
import { removalKindLabel } from '@/lib/labels'
import { formatNumber, formatPercent } from '@/lib/format'
import type { RemediationCatalogItem, RemediationItemProgress, RemediationKind } from '@/lib/types'
import { useT } from '@/i18n'

interface ItemProgressTableProps {
  items: RemediationItemProgress[] | undefined
  index: Map<string, RemediationCatalogItem>
  isLoading: boolean
  /** Restrict to one kind (the Apps / Startup tabs); omitted = both, with a kind column. */
  kind?: RemediationKind
  title: string
  description: string
}

/** How far the fleet is from the saved policy, item by item. Converged vs pending
 *  is drawn as one bar so "almost done" and "nothing happened" read at a glance. */
export function ItemProgressTable({ items, index, isLoading, kind, title, description }: ItemProgressTableProps) {
  const t = useT()
  const rows = useMemo(
    () => (items ?? []).filter((it) => !kind || it.kind === kind),
    [items, kind],
  )
  const nameOf = (it: RemediationItemProgress) =>
    index.get(itemKey(it.kind, it.item_id))?.display_name ?? it.display_name ?? it.item_id

  const columns: Column<RemediationItemProgress>[] = [
    {
      key: 'item',
      header: t('optimization.items.colItem'),
      sortable: true,
      sortValue: (r) => nameOf(r),
      render: (r) => (
        <span className="flex flex-wrap items-center gap-2">
          <span className="font-medium text-text">{nameOf(r)}</span>
          {!r.selected && <Badge tone="neutral">{t('optimization.items.notSelected')}</Badge>}
        </span>
      ),
    },
    ...(kind
      ? []
      : [
          {
            key: 'kind',
            header: t('optimization.items.colKind'),
            render: (r: RemediationItemProgress) => <span className="text-text-muted">{removalKindLabel(r.kind)}</span>,
          },
        ]),
    {
      key: 'pending',
      header: t('optimization.items.colPending'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.pending,
      render: (r) => (
        <span className={r.pending > 0 ? 'tabular font-medium text-warn' : 'tabular text-text-dim'}>
          {formatNumber(r.pending)}
        </span>
      ),
    },
    {
      key: 'converged',
      header: t('optimization.items.colConverged'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.converged,
      render: (r) => <span className="tabular text-text">{formatNumber(r.converged)}</span>,
    },
    {
      key: 'dry_run',
      header: t('optimization.items.colDryRun'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.dry_run,
      render: (r) => <span className="tabular text-text-muted">{formatNumber(r.dry_run)}</span>,
    },
    {
      key: 'failures',
      header: t('optimization.items.colFailures'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.open_failures,
      render: (r) => (
        <span className={r.open_failures > 0 ? 'tabular font-medium text-crit' : 'tabular text-text-dim'}>
          {formatNumber(r.open_failures)}
        </span>
      ),
    },
    {
      key: 'progress',
      header: t('optimization.items.colProgress'),
      className: 'w-[160px]',
      render: (r) => <ProgressBar done={r.converged} pending={r.pending} />,
    },
  ]

  return (
    <Card padded={false}>
      <CardHeader title={title} description={description} className="p-5 pb-3" />
      <div className="px-5 pb-5">
        {isLoading ? (
          <SkeletonText lines={5} />
        ) : (
          <DataTable
            columns={columns}
            rows={rows}
            getRowKey={(r) => itemKey(r.kind, r.item_id)}
            initialSort={{ key: 'pending', dir: 'desc' }}
            emptyLabel={t('optimization.items.empty')}
            pageSize={10}
            className="border-0"
          />
        )}
        <p className="mt-3 flex items-start gap-2 text-[12px] text-text-dim">
          <ListChecks className="mt-px size-3.5 shrink-0" aria-hidden />
          <span>{t('optimization.items.footnote')}</span>
        </p>
      </div>
    </Card>
  )
}

function ProgressBar({ done, pending }: { done: number; pending: number }) {
  const t = useT()
  const total = done + pending
  const pct = total > 0 ? Math.round((done / total) * 100) : 0
  const label = total > 0 ? t('optimization.items.progressLabel', { done, total }) : t('optimization.items.progressNone')
  return (
    <div className="flex items-center gap-2" title={label}>
      <span
        className="block h-1.5 flex-1 overflow-hidden rounded-full bg-wash-2"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={pct}
        aria-label={label}
      >
        <span className="block h-full rounded-full bg-ok/80 transition-[width] duration-[380ms]" style={{ width: `${pct}%` }} />
      </span>
      <span className="tabular w-10 text-right text-[12px] text-text-muted">{total > 0 ? formatPercent(pct) : '—'}</span>
    </div>
  )
}
