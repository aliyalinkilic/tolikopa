import { useMemo } from 'react'
import { Link } from 'react-router-dom'
import { History } from 'lucide-react'
import { Badge } from '@/components/ui/Badge'
import { Card, CardHeader } from '@/components/ui/Card'
import { Select } from '@/components/ui/Select'
import { DataTable } from '@/components/DataTable'
import type { Column } from '@/components/DataTable'
import { EmptyState } from '@/components/EmptyState'
import { SkeletonText } from '@/components/Skeleton'
import { itemKey } from '@/lib/remediation'
import {
  REMOVAL_OUTCOME_ORDER, removalActionLabel, removalErrorMeta, removalKindLabel, removalOutcomeMeta,
} from '@/lib/labels'
import { formatDayTime, relativeTime } from '@/lib/format'
import type { RemediationAttemptDTO, RemediationCatalogItem, RemediationRollup } from '@/lib/types'
import { useT } from '@/i18n'

interface AttemptsPanelProps {
  attempts: RemediationAttemptDTO[]
  rollup: RemediationRollup[]
  index: Map<string, RemediationCatalogItem>
  isLoading: boolean
  outcome: string
  onOutcomeChange: (outcome: string) => void
}


/** Read-only audit surface: per-item roll-up first (what happened to what), then
 *  the raw attempt log (where and when). */
export function AttemptsPanel({
  attempts,
  rollup,
  index,
  isLoading,
  outcome,
  onOutcomeChange,
}: AttemptsPanelProps) {
  const t = useT()
  const nameOf = (kind: string, id: string) => index.get(itemKey(kind, id))?.display_name ?? id
  const outcomeOptions = [
    { value: '', label: t('removal.attempts.allOutcomes') },
    ...REMOVAL_OUTCOME_ORDER.map((o) => ({ value: o, label: removalOutcomeMeta(o).label })),
  ]

  const dryRunCount = useMemo(() => attempts.filter((a) => a.outcome === 'dry_run').length, [attempts])

  const rollupColumns: Column<RemediationRollup>[] = [
    {
      key: 'item',
      header: t('removal.attempts.item'),
      sortable: true,
      sortValue: (r) => nameOf(r.kind, r.item_id),
      render: (r) => (
        <div className="min-w-0">
          <span className="block truncate font-medium text-text">{nameOf(r.kind, r.item_id)}</span>
          <span className="text-[12px] text-text-dim">{removalKindLabel(r.kind)}</span>
        </div>
      ),
    },
    {
      key: 'outcomes',
      header: t('removal.attempts.outcomes'),
      render: (r) => (
        <div className="flex flex-wrap gap-1.5">
          {REMOVAL_OUTCOME_ORDER.filter((o) => (r.outcomes[o] ?? 0) > 0).map((o) => {
            const meta = removalOutcomeMeta(o)
            return (
              <Badge key={o} tone={meta.token} title={meta.description}>
                {meta.label} <span className="tabular">{r.outcomes[o]}</span>
              </Badge>
            )
          })}
        </div>
      ),
    },
    {
      key: 'machines',
      header: t('common.machine'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.machines,
      render: (r) => <span className="tabular text-text-muted">{r.machines}</span>,
    },
    {
      key: 'last',
      header: t('removal.attempts.lastAction'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.last_ts ?? '',
      render: (r) => <span className="text-text-dim">{r.last_ts ? relativeTime(r.last_ts) : '—'}</span>,
    },
  ]

  const attemptColumns: Column<RemediationAttemptDTO>[] = [
    {
      key: 'ts',
      header: t('removal.attempts.time'),
      sortable: true,
      sortValue: (r) => r.ts,
      render: (r) => <span className="tabular whitespace-nowrap text-text-muted">{formatDayTime(r.ts)}</span>,
    },
    {
      key: 'machine',
      header: t('common.machine'),
      sortable: true,
      sortValue: (r) => r.hostname ?? r.machine_id,
      render: (r) =>
        r.machine_deleted ? (
          // Purged machine: the attempt is audit trail, there is no page to link to.
          <span className="inline-flex items-center gap-1.5" title={t('removal.attempts.machineDeletedTitle')}>
            <span className="truncate font-medium text-text-muted">{r.hostname || r.machine_id}</span>
            <span className="rounded-full border border-border px-1.5 text-[11px] text-text-dim">
              {t('removal.attempts.machineDeleted')}
            </span>
          </span>
        ) : (
          <Link
            to={`/machines/${r.machine_id}`}
            className="truncate font-medium text-accent hover:underline ring-focus"
          >
            {r.hostname || r.machine_id}
          </Link>
        ),
    },
    {
      key: 'item',
      header: t('removal.attempts.item'),
      sortable: true,
      sortValue: (r) => nameOf(r.kind, r.item_id),
      render: (r) => <span className="text-text">{nameOf(r.kind, r.item_id)}</span>,
    },
    {
      key: 'action',
      header: t('removal.attempts.action'),
      render: (r) => <span className="text-text-muted">{removalActionLabel(r.action)}</span>,
    },
    {
      key: 'outcome',
      header: t('removal.attempts.outcome'),
      sortable: true,
      sortValue: (r) => removalOutcomeMeta(r.outcome).label,
      render: (r) => {
        const meta = removalOutcomeMeta(r.outcome)
        return (
          <div className="flex flex-wrap items-center gap-1.5">
            <Badge tone={meta.token} title={meta.description} dot>
              {meta.label}
            </Badge>
            {r.resolved && (
              <Badge tone="ok" title={t('removal.attempts.healedTitle')}>
                {t('removal.attempts.healed')}
              </Badge>
            )}
          </div>
        )
      },
    },
    {
      key: 'error',
      header: t('removal.attempts.errorCode'),
      align: 'right',
      render: (r) => {
        const meta = removalErrorMeta(r.error_code)
        return (
          <span className="font-mono text-[12px] text-text-dim" title={meta ? `${meta.label} — ${meta.hint}` : undefined}>
            {r.error_code || '—'}
            {meta && <span className="ml-1.5 font-sans text-text-muted">{meta.label}</span>}
          </span>
        )
      },
    },
  ]

  return (
    <div className="space-y-4">
      <Card padded={false}>
        <CardHeader
          title={t('removal.attempts.byItemTitle')}
          description={
            dryRunCount > 0
              ? t('removal.attempts.byItemDryRun', { count: dryRunCount })
              : t('removal.attempts.byItemBody')
          }
          className="p-5 pb-4"
        />
        <div className="px-5 pb-5">
          {isLoading ? (
            <SkeletonText lines={5} />
          ) : rollup.length === 0 ? (
            <EmptyState
              icon={History}
              title={t('removal.attempts.emptyTitle')}
              description={t('removal.attempts.emptyBody')}
            />
          ) : (
            <DataTable
              columns={rollupColumns}
              rows={rollup}
              getRowKey={(r) => itemKey(r.kind, r.item_id)}
              initialSort={{ key: 'last', dir: 'desc' }}
              className="border-0"
            />
          )}
        </div>
      </Card>

      <Card padded={false}>
        <CardHeader
          title={t('removal.attempts.recentTitle')}
          description={t('removal.attempts.recentBody')}
          action={
            <div className="w-48">
              <Select
                options={outcomeOptions}
                value={outcome}
                onChange={(e) => onOutcomeChange(e.target.value)}
                aria-label={t('removal.attempts.filterLabel')}
              />
            </div>
          }
          className="p-5 pb-4"
        />
        <div className="px-5 pb-5">
          {isLoading ? (
            <SkeletonText lines={6} />
          ) : attempts.length === 0 ? (
            <EmptyState icon={History} title={t('removal.attempts.noRecords')} />
          ) : (
            <DataTable
              columns={attemptColumns}
              rows={attempts}
              getRowKey={(r) => `${r.ts}-${r.machine_id}-${r.kind}-${r.item_id}`}
              initialSort={{ key: 'ts', dir: 'desc' }}
              pageSize={15}
              className="border-0"
            />
          )}
        </div>
      </Card>
    </div>
  )
}
