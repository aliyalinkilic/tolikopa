import { useMemo } from 'react'
import { motion } from 'motion/react'
import { AlertOctagon, Hourglass, ServerCog, ShieldCheck, Signal } from 'lucide-react'
import { Badge } from '@/components/ui/Badge'
import { Card, CardHeader } from '@/components/ui/Card'
import { EmptyState } from '@/components/EmptyState'
import { KpiCard } from '@/components/KpiCard'
import { SkeletonCard } from '@/components/Skeleton'
import { removalModeMeta } from '@/lib/labels'
import { fadeUp, staggerContainer } from '@/lib/motion'
import { formatNumber, relativeTime } from '@/lib/format'
import type { RemediationPlatformSummary, RemediationPostureResponse } from '@/lib/types'
import { useT } from '@/i18n'
import { CapabilityMatrix } from './CapabilityMatrix'
import { ItemProgressTable } from './ItemProgressTable'
import { MachinePostureTable, isBlocked } from './MachinePostureTable'
import type { PolicyEditor } from './usePolicyEditor'

interface Props {
  editor: PolicyEditor
  posture: RemediationPostureResponse | undefined
  isLoading: boolean
  isError: boolean
}

export function OverviewTab({ editor, posture, isLoading, isError }: Props) {
  const t = useT()
  const win = useMemo<RemediationPlatformSummary | undefined>(
    () => posture?.platforms.find((p) => p.kind === 'windows'),
    [posture],
  )
  const machines = useMemo(() => posture?.machines ?? [], [posture])
  const stats = useMemo(() => {
    let pendingPairs = 0
    let pendingMachines = 0
    let failurePairs = 0
    let failureMachines = 0
    let blocked = 0
    let excluded = 0
    for (const m of machines) {
      const p = m.pending_apps + m.pending_startup
      pendingPairs += p
      if (p > 0) pendingMachines++
      failurePairs += m.open_failures
      if (m.open_failures > 0) failureMachines++
      if (isBlocked(m)) blocked++
      if (m.excluded) excluded++
    }
    return { pendingPairs, pendingMachines, failurePairs, failureMachines, blocked, excluded }
  }, [machines])

  if (isError && !posture) {
    return (
      <EmptyState
        icon={ServerCog}
        title={t('optimization.overview.loadFailedTitle')}
        description={t('optimization.overview.loadFailedBody')}
      />
    )
  }
  if (isLoading || !posture) {
    return (
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <SkeletonCard key={i} />
        ))}
      </div>
    )
  }

  const total = win?.machines ?? 0
  const reporting = win?.reporting ?? 0
  const policy = editor.data?.policy
  const fleet = editor.data?.posture
  const modeMeta = removalModeMeta(editor.savedMode)

  return (
    <div className="space-y-4">
      <motion.div
        variants={staggerContainer()}
        initial="hidden"
        animate="show"
        className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4"
      >
        <KpiCard
          label={t('optimization.overview.kpiReporting')}
          value={reporting}
          icon={Signal}
          tone="info"
          footer={t('optimization.overview.kpiReportingFoot', { total })}
        />
        <KpiCard
          label={t('optimization.overview.kpiActuating')}
          value={win?.actuating ?? 0}
          icon={ShieldCheck}
          tone={(win?.actuating ?? 0) > 0 ? 'ok' : 'warn'}
          footer={t('optimization.overview.kpiActuatingFoot', { blocked: stats.blocked })}
        />
        <KpiCard
          label={t('optimization.overview.kpiPending')}
          value={stats.pendingPairs}
          icon={Hourglass}
          tone={stats.pendingPairs > 0 ? 'warn' : 'ok'}
          footer={t('optimization.overview.kpiPendingFoot', { machines: stats.pendingMachines })}
        />
        <KpiCard
          label={t('optimization.overview.kpiFailures')}
          value={stats.failurePairs}
          icon={AlertOctagon}
          tone={stats.failurePairs > 0 ? 'crit' : 'ok'}
          footer={t('optimization.overview.kpiFailuresFoot', { machines: stats.failureMachines })}
        />
      </motion.div>

      <motion.div
        variants={staggerContainer()}
        initial="hidden"
        animate="show"
        className="grid gap-4 lg:grid-cols-2"
      >
        <motion.div variants={fadeUp}>
          <Card className="h-full">
            <CardHeader title={t('optimization.overview.switchesTitle')} description={t('optimization.overview.switchesBody')} />
            <ul className="mt-4 space-y-3.5">
              <SwitchRow label={t('optimization.overview.switchApps')} on={win?.allow_apps ?? 0} total={reporting} />
              <SwitchRow label={t('optimization.overview.switchStartup')} on={win?.allow_startup ?? 0} total={reporting} />
              <SwitchRow label={t('optimization.overview.switchEnforce')} on={win?.enforce ?? 0} total={reporting} />
            </ul>
            <div className="mt-4 space-y-1.5 text-[12px] text-text-dim">
              {(fleet?.switches_unknown ?? 0) > 0 && (
                <p>{t('optimization.overview.switchesUnknown', { count: fleet?.switches_unknown ?? 0 })}</p>
              )}
              {total - reporting > 0 && (
                <p>{t('optimization.overview.neverReported', { count: total - reporting })}</p>
              )}
            </div>
          </Card>
        </motion.div>

        <motion.div variants={fadeUp}>
          <Card className="h-full">
            <CardHeader
              title={t('optimization.overview.policyTitle')}
              description={t('optimization.overview.policyBody')}
              action={
                <Badge tone={modeMeta.token} dot pulse={editor.savedMode === 'enforce'}>
                  {modeMeta.label}
                </Badge>
              }
            />
            <dl className="mt-4 grid grid-cols-2 gap-x-4 gap-y-3 text-[13px]">
              <Fact label={t('optimization.overview.policyRevision')} value={policy?.revision || '—'} />
              <Fact
                label={t('optimization.overview.policyApplied')}
                value={t('optimization.overview.policyAppliedValue', {
                  applied: formatNumber(fleet?.applied_machines ?? 0),
                  reporting: formatNumber(reporting),
                })}
              />
              <Fact
                label={t('optimization.overview.policySelected')}
                value={t('optimization.overview.policySelectedValue', {
                  apps: policy?.apps.length ?? 0,
                  startup: policy?.startup.length ?? 0,
                })}
              />
              <Fact label={t('optimization.overview.policyExcluded')} value={formatNumber(stats.excluded)} />
              <Fact
                label={t('optimization.overview.policyStale')}
                value={formatNumber(fleet?.drift.stale_machines ?? 0)}
                tone={(fleet?.drift.stale_machines ?? 0) > 0 ? 'warn' : undefined}
              />
              <Fact
                label={t('optimization.overview.policyUpdated')}
                value={
                  policy?.updated_at
                    ? policy.updated_by
                      ? t('optimization.overview.policyUpdatedBy', { who: policy.updated_by, when: relativeTime(policy.updated_at) })
                      : relativeTime(policy.updated_at)
                    : '—'
                }
              />
            </dl>
          </Card>
        </motion.div>
      </motion.div>

      <ItemProgressTable
        items={posture.items}
        index={editor.index}
        isLoading={false}
        title={t('optimization.items.title')}
        description={t('optimization.items.body')}
      />

      <MachinePostureTable machines={machines} isLoading={false} />

      <CapabilityMatrix />
    </div>
  )
}

function SwitchRow({ label, on, total }: { label: string; on: number; total: number }) {
  const t = useT()
  const pct = total > 0 ? Math.round((on / total) * 100) : 0
  return (
    <li>
      <div className="mb-1.5 flex items-baseline justify-between gap-3 text-[13px]">
        <span className="text-text-muted">{label}</span>
        <span className="tabular font-medium text-text">{t('optimization.overview.switchOf', { on, total })}</span>
      </div>
      <span
        className="block h-2 overflow-hidden rounded-full bg-wash-2"
        role="meter"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={total}
        aria-valuenow={on}
      >
        <motion.span
          className="block h-full rounded-full bg-accent/80"
          initial={{ width: 0 }}
          animate={{ width: `${pct}%` }}
          transition={{ duration: 0.38, ease: [0.22, 1, 0.36, 1] }}
        />
      </span>
    </li>
  )
}

function Fact({ label, value, tone }: { label: string; value: string; tone?: 'warn' }) {
  return (
    <div className="min-w-0">
      <dt className="text-[11px] font-semibold uppercase tracking-[0.08em] text-text-dim">{label}</dt>
      <dd className={tone === 'warn' ? 'tabular mt-0.5 truncate font-medium text-warn' : 'tabular mt-0.5 truncate font-medium text-text'}>
        {value}
      </dd>
    </div>
  )
}
