import { CheckCircle2, RefreshCw, Wrench } from 'lucide-react'
import { Badge } from '@/components/ui/Badge'
import { Card, CardHeader } from '@/components/ui/Card'
import { DataTable } from '@/components/DataTable'
import type { Column } from '@/components/DataTable'
import { EmptyState } from '@/components/EmptyState'
import { SkeletonText } from '@/components/Skeleton'
import { itemKey } from '@/lib/remediation'
import { removalErrorMeta, removalKindLabel, removalOutcomeMeta } from '@/lib/labels'
import { relativeTime } from '@/lib/format'
import type { RemediationCatalogItem, RemediationFailuresResponse, RemediationFailureGroup } from '@/lib/types'
import { translate, useT } from '@/i18n'
import { compareText } from '@/lib/format'
import { catalogName } from '@/i18n/catalog'

interface FailuresPanelProps {
  data: RemediationFailuresResponse | undefined
  index: Map<string, RemediationCatalogItem>
  isLoading: boolean
  isError: boolean
}

/**
 * One table row: every open failure that shares a cause (outcome + code) on the
 * same machines. The server buckets per item, so a single broken machine would
 * otherwise fill the table with one identical row per selected app.
 */
interface FailureCluster {
  key: string
  kind: RemediationFailureGroup['kind']
  items: RemediationFailureGroup[]
  outcome: string
  error_code?: string
  machines: number
  hostnames: string[]
  staleAgents: number
  last_ts: string
}

function clusterFailures(groups: RemediationFailureGroup[]): FailureCluster[] {
  const byKey = new Map<string, FailureCluster>()
  for (const g of groups) {
    const hosts = [...g.hostnames].sort()
    const key = [g.kind, g.outcome, g.error_code ?? '', g.machines, hosts.join(',')].join('|')
    const c = byKey.get(key)
    if (!c) {
      byKey.set(key, {
        key,
        kind: g.kind,
        items: [g],
        outcome: g.outcome,
        error_code: g.error_code,
        machines: g.machines,
        hostnames: hosts,
        staleAgents: g.stale_agents ?? 0,
        last_ts: g.last_ts,
      })
      continue
    }
    c.items.push(g)
    c.staleAgents = Math.max(c.staleAgents, g.stale_agents ?? 0)
    if (g.last_ts > c.last_ts) c.last_ts = g.last_ts
  }
  return [...byKey.values()]
}

/** Advice for a cluster: an outdated agent first, then the error code's meaning, then the outcome's. */
function adviceFor(g: FailureCluster, latestAgent?: string): { label: string; hint: string; transient: boolean } {
  // Every machine in the cluster runs an old agent: whatever the code says, the
  // first thing to rule out is a bug a newer build already fixed.
  if (g.staleAgents > 0 && g.staleAgents >= g.machines) {
    return {
      label: translate('removal.failures.staleAgentLabel'),
      hint: latestAgent
        ? translate('removal.failures.staleAgentHintVersion', { version: latestAgent })
        : translate('removal.failures.staleAgentHint'),
      transient: false,
    }
  }
  const meta = removalErrorMeta(g.error_code)
  if (meta) return meta
  switch (g.outcome) {
    case 'timeout':
      return {
        label: translate('removal.failures.timeoutLabel'),
        hint: translate('removal.failures.timeoutHint'),
        transient: true,
      }
    case 'unknown_item':
      return {
        label: translate('removal.failures.unknownItemLabel'),
        hint: translate('removal.failures.unknownItemHint'),
        transient: false,
      }
    case 'blocked':
      return {
        label: translate('removal.failures.blockedLabel'),
        hint: translate('removal.failures.blockedHint'),
        transient: false,
      }
    default:
      return {
        label: g.error_code ? translate('removal.failures.unknownCodeLabel') : translate('removal.failures.noCodeLabel'),
        hint: translate('removal.failures.unknownCodeHint'),
        transient: false,
      }
  }
}

/**
 * Failures that are STILL open: the latest actuation attempt for the machine+item
 * failed and the machine has not since reported the item gone. Historical
 * failures that a later pass fixed are counted, not listed — they are the rows
 * that make the audit log look worse than the fleet is.
 */
export function FailuresPanel({ data, index, isLoading, isError }: FailuresPanelProps) {
  const t = useT()
  const nameOf = (g: RemediationFailureGroup) =>
    index.get(itemKey(g.kind, g.item_id))?.display_name ?? catalogName(g.item_id, g.display_name)
  const namesOf = (c: FailureCluster) => c.items.map(nameOf).sort(compareText)
  const latestAgent = data?.latest_agent_version

  const columns: Column<FailureCluster>[] = [
    {
      key: 'item',
      header: t('removal.failures.item'),
      sortable: true,
      sortValue: (c) => namesOf(c)[0],
      render: (c) => {
        const names = namesOf(c)
        if (names.length === 1) {
          return (
            <div className="min-w-0">
              <span className="block truncate font-medium text-text">{names[0]}</span>
              <span className="text-[12px] text-text-dim">{removalKindLabel(c.kind)}</span>
            </div>
          )
        }
        return (
          <div className="min-w-0">
            <span className="block font-medium text-text">
              {t('removal.failures.itemCount', { count: names.length })} <span className="font-normal text-text-dim">· {removalKindLabel(c.kind)}</span>
            </span>
            <p className="mt-0.5 line-clamp-2 max-w-[40ch] text-[12px] text-text-dim" title={names.join(', ')}>
              {names.join(', ')}
            </p>
          </div>
        )
      },
    },
    {
      key: 'cause',
      header: t('removal.failures.reason'),
      render: (g) => {
        const outcome = removalOutcomeMeta(g.outcome)
        const advice = adviceFor(g, latestAgent)
        return (
          <div className="min-w-0 space-y-1">
            <div className="flex flex-wrap items-center gap-1.5">
              <Badge tone={outcome.token} title={outcome.description} dot>
                {outcome.label}
              </Badge>
              {g.error_code && <span className="font-mono text-[12px] text-text-dim">{g.error_code}</span>}
              <span className="text-[13px] font-medium text-text">{advice.label}</span>
            </div>
            <p className="max-w-[46ch] text-[12px] leading-[1.45] text-text-muted">{advice.hint}</p>
          </div>
        )
      },
    },
    {
      key: 'machines',
      header: t('common.machine'),
      sortable: true,
      sortValue: (g) => g.machines,
      render: (g) => (
        <div className="min-w-0">
          <span className="tabular font-medium text-text">{g.machines}</span>
          <p className="mt-0.5 max-w-[28ch] truncate text-[12px] text-text-dim" title={g.hostnames.join(', ')}>
            {g.hostnames.join(', ')}
            {g.machines > g.hostnames.length && ` +${g.machines - g.hostnames.length}`}
          </p>
        </div>
      ),
    },
    {
      key: 'expect',
      header: t('removal.failures.expectation'),
      render: (g) =>
        adviceFor(g, latestAgent).transient ? (
          <Badge tone="info" icon={<RefreshCw className="size-3" aria-hidden />}>
            {t('removal.failures.selfHealing')}
          </Badge>
        ) : (
          <Badge tone="warn" icon={<Wrench className="size-3" aria-hidden />}>
            {t('removal.failures.needsAction')}
          </Badge>
        ),
    },
    {
      key: 'last',
      header: t('removal.failures.lastAttempt'),
      align: 'right',
      sortable: true,
      sortValue: (g) => g.last_ts,
      render: (g) => <span className="whitespace-nowrap text-text-dim">{relativeTime(g.last_ts)}</span>,
    },
  ]

  const open = data?.open_machine_items ?? 0
  const resolved = data?.resolved_machine_items ?? 0

  return (
    <Card padded={false}>
      <CardHeader
        title={t('removal.failures.title')}
        description={
          data
            ? t('removal.failures.summary', { total: open + resolved, resolved, open })
            : t('removal.failures.body')
        }
        className="p-5 pb-4"
      />
      <div className="px-5 pb-5">
        {isLoading ? (
          <SkeletonText lines={5} />
        ) : isError ? (
          <EmptyState
            icon={Wrench}
            title={t('removal.failures.loadFailedTitle')}
            description={t('removal.failures.notSupported')}
          />
        ) : !data || data.open.length === 0 ? (
          <EmptyState
            icon={CheckCircle2}
            title={t('removal.failures.noneTitle')}
            description={
              resolved > 0
                ? t('removal.failures.noneAllResolved', { count: resolved })
                : t('removal.failures.noneBody')
            }
          />
        ) : (
          <DataTable
            columns={columns}
            rows={clusterFailures(data.open)}
            getRowKey={(c) => c.key}
            initialSort={{ key: 'machines', dir: 'desc' }}
            className="border-0"
          />
        )}
      </div>
    </Card>
  )
}
