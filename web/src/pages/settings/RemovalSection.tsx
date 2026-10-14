import { useMemo, useState } from 'react'
import { motion, useReducedMotion } from 'motion/react'
import { Info, RotateCcw, Save, ServerCog } from 'lucide-react'
import { Button } from '@/components/ui/Button'
import { Card, CardHeader } from '@/components/ui/Card'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { Tabs } from '@/components/ui/Tabs'
import { EmptyState } from '@/components/EmptyState'
import { SkeletonCard, SkeletonText } from '@/components/Skeleton'
import { AttemptsPanel } from '@/components/removal/AttemptsPanel'
import { FailuresPanel } from '@/components/removal/FailuresPanel'
import { ImpactPanel } from '@/components/removal/ImpactPanel'
import { CatalogList } from '@/components/removal/CatalogList'
import { ModeControl } from '@/components/removal/ModeControl'
import { PolicyDiff } from '@/components/removal/PolicyDiff'
import { RemovalSummary } from '@/components/removal/RemovalSummary'
import { TagExclusionEditor } from '@/components/removal/TagExclusionEditor'
import {
  useFleet, useRemediationAttempts, useRemediationFailures, useRemediationImpact,
  useRemediationOverview, useUpdateRemediationPolicy,
} from '@/hooks/queries'
import { useRemovalDraft } from '@/hooks/useRemovalDraft'
import { useToast } from '@/hooks/useToast'
import { ApiError } from '@/lib/api'
import {
  blastRadius, hasDryRunEvidence, indexCatalog, modeOf, rollupAttempts, tagUniverse,
} from '@/lib/remediation'
import { removalModeMeta } from '@/lib/labels'
import type { RemediationCatalogGroups } from '@/lib/types'
import { useT } from '@/i18n'
import { Trans } from '@/i18n/rich'

const EMPTY_CATALOG: RemediationCatalogGroups = { app: [], startup: [] }

/**
 * "Apps to Remove" — the panel's only fleet-wide destructive action.
 *
 * The layout is deliberately ordered blast radius → mode → exclusions → catalog:
 * the admin sees how many machines are in scope, and which of them cannot act at
 * all (stale catalog / GPO kill switch), before ticking anything.
 */
export function RemovalSection() {
  const t = useT()
  const overview = useRemediationOverview()
  const [outcomeFilter, setOutcomeFilter] = useState('')
  const attemptsQuery = useRemediationAttempts({ limit: 200, outcome: outcomeFilter || undefined })
  const [resultsTab, setResultsTab] = useState('impact')
  const [impactDays, setImpactDays] = useState(7)
  const failuresQuery = useRemediationFailures()
  const impactQuery = useRemediationImpact(impactDays)
  const fleet = useFleet()
  const savePolicy = useUpdateRemediationPolicy()
  const { push } = useToast()
  const reduce = useReducedMotion()

  const draft = useRemovalDraft(overview.data?.policy)
  const [confirmOpen, setConfirmOpen] = useState(false)

  const catalog = overview.data?.catalog ?? EMPTY_CATALOG
  const index = useMemo(() => indexCatalog(catalog), [catalog])

  const attempts = useMemo(() => attemptsQuery.data?.attempts ?? [], [attemptsQuery.data])
  const rollup = useMemo(
    () => attemptsQuery.data?.rollup ?? rollupAttempts(attempts),
    [attemptsQuery.data, attempts],
  )

  const excludedTags = draft.draft?.excluded_tags ?? []
  const radius = useMemo(() => blastRadius(fleet.data, excludedTags), [fleet.data, excludedTags])
  const universe = useMemo(() => tagUniverse(fleet.data), [fleet.data])

  const posture = overview.data?.posture
  // The server owns this flag; a dedicated one-row probe is a second, local
  // witness so the interlock still works against a backend that does not compute
  // it yet. It must NOT read the table query above — that one is user-filtered,
  // and an outcome filter would silently unlock or relock enforce mode.
  const dryRunProbe = useRemediationAttempts({ limit: 1, outcome: 'dry_run' })
  const dryRunObserved =
    (posture?.dry_run_observed ?? false) || hasDryRunEvidence(dryRunProbe.data?.attempts)

  const savedMode = overview.data ? modeOf(overview.data.policy) : 'off'
  const enforceBlockedReason = (() => {
    if (savedMode === 'enforce') return null // already enforcing; the interlock is for the first switch
    if (!dryRunObserved) {
      // A dry-run row can only ever come from a machine whose local GPO switch is
      // on: with the switch off the agent reports inventory but writes no attempt
      // at all. Saying "wait for a result" in that state would be waiting forever,
      // so name the real blocker.
      const reporting = posture?.reporting_machines ?? 0
      const killed = posture?.kill_switch_off ?? 0
      if (reporting > 0 && killed >= reporting) {
        return t('removal.section.blockNoGpo')
      }
      if (savedMode === 'dry_run') {
        return t('removal.section.blockWaiting')
      }
      return t('removal.section.blockNeedDryRun')
    }
    if (draft.diff?.selectionChanged) return t('removal.section.blockSelectionChanged')
    return null
  })()

  const selectedApps = draft.draft?.apps.length ?? 0
  const selectedStartup = draft.draft?.startup.length ?? 0
  const willEnforce = draft.draft ? modeOf(draft.draft) === 'enforce' : false

  // Blast radius is computed from the live fleet (it knows the tags); if that list
  // is unavailable we fall back to the server's own count rather than claiming 0.
  const fleetKnown = !fleet.isLoading && !!fleet.data
  const targetedCount = fleetKnown ? radius.targeted : (posture?.targeted_machines ?? 0)

  const commit = () => {
    const d = draft.draft
    if (!d) return
    savePolicy.mutate(
      {
        enabled: d.enabled,
        dry_run: d.dry_run,
        excluded_tags: d.excluded_tags,
        apps: d.apps,
        startup: d.startup,
      },
      {
        onSuccess: (saved) => {
          setConfirmOpen(false)
          if (saved) draft.adopt(saved)
          push({
            title: t('removal.section.saved'),
            description: willEnforce
              ? t('removal.section.savedEnforce', { count: targetedCount })
              : t('removal.section.savedMode', { mode: removalModeMeta(modeOf(d)).label }),
            tone: willEnforce ? 'warn' : 'ok',
          })
        },
        onError: () => push({ title: t('removal.section.saveFailed'), tone: 'crit' }),
      },
    )
  }

  const onSaveClick = () => {
    if (willEnforce) setConfirmOpen(true)
    else commit()
  }

  // ── Unavailable / loading ────────────────────────────────────────────────────
  if (overview.isError) {
    const notDeployed = overview.error instanceof ApiError && overview.error.status === 404
    return (
      <EmptyState
        icon={ServerCog}
        title={notDeployed ? t('removal.section.notDeployedTitle') : t('removal.section.loadFailedTitle')}
        description={
          notDeployed
            ? t('removal.section.notDeployedBody')
            : t('removal.section.loadFailedBody')
        }
        action={
          <Button variant="secondary" onClick={() => overview.refetch()} loading={overview.isFetching}>
            {t('common.retry')}
          </Button>
        }
      />
    )
  }

  // `draft.draft` is null for the first render after the policy lands (the seeding
  // effect has not run yet) — keep the skeleton rather than flashing an error.
  if (!overview.data || !draft.draft) {
    return (
      <div className="space-y-4">
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <SkeletonCard key={i} />
          ))}
        </div>
        <Card>
          <SkeletonText lines={8} />
        </Card>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Announces what the FLEET is doing (saved policy), not what is drafted. */}
      <p role="status" aria-live="polite" className="sr-only">
        {savedMode === 'enforce'
          ? t('removal.section.liveEnforce', { machines: targetedCount, apps: selectedApps })
          : t('removal.section.liveMode', { mode: removalModeMeta(savedMode).label })}
      </p>

      <p className="flex items-start gap-2 text-[13px] text-text-muted">
        <Info className="mt-0.5 size-4 shrink-0 text-text-dim" aria-hidden />
        <span>
          {t('removal.section.intro')}
        </span>
      </p>

      {draft.stale && (
        <Card className="border-warn/25 bg-warn/[0.06]">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-[13px] text-text-muted">
              {t('removal.section.stale')}
            </p>
            <Button
              size="sm"
              variant="secondary"
              leftIcon={<RotateCcw className="size-4" aria-hidden />}
              onClick={draft.reset}
            >
              {t('removal.section.loadServer')}
            </Button>
          </div>
        </Card>
      )}

      <RemovalSummary
        posture={overview.data.posture}
        radius={radius}
        fleetKnown={fleetKnown}
      />

      <ModeControl
        mode={draft.mode}
        savedMode={savedMode}
        onChange={draft.setMode}
        enforceBlockedReason={enforceBlockedReason}
        lastDryRunAt={overview.data.posture.last_dry_run_at}
        disabled={savePolicy.isPending}
      />

      <TagExclusionEditor
        value={excludedTags}
        onChange={draft.setExcludedTags}
        universe={universe}
        excludedMachines={radius.excluded}
        disabled={savePolicy.isPending}
      />

      <CatalogList
        catalog={catalog}
        isSelected={draft.isSelected}
        onToggle={draft.toggle}
        onSetMany={draft.setMany}
        disabled={savePolicy.isPending}
      />

      {draft.dirty && draft.diff && (
        <motion.div
          initial={reduce ? false : { opacity: 0, y: 12 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.2, ease: [0.22, 1, 0.36, 1] }}
          className="sticky bottom-4 z-20"
        >
          <Card className="glass border-accent/30 shadow-[var(--shadow-pop)]">
            <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
              <div className="min-w-0">
                <CardHeader
                  title={t('removal.section.unsavedTitle')}
                  description={t('removal.section.unsavedSummary', { apps: selectedApps, startup: selectedStartup })}
                />
                <PolicyDiff diff={draft.diff} index={index} className="mt-3" />
              </div>
              <div className="flex shrink-0 gap-2">
                <Button
                  variant="ghost"
                  leftIcon={<RotateCcw className="size-4" aria-hidden />}
                  onClick={draft.reset}
                  disabled={savePolicy.isPending}
                >
                  {t('common.undo')}
                </Button>
                <Button
                  variant={willEnforce ? 'danger' : 'primary'}
                  leftIcon={<Save className="size-4" aria-hidden />}
                  loading={savePolicy.isPending}
                  onClick={onSaveClick}
                >
                  {willEnforce ? t('removal.section.saveAndEnforce') : t('common.save')}
                </Button>
              </div>
            </div>
          </Card>
        </motion.div>
      )}

      <div className="space-y-4 pt-2">
        <Tabs
          tabs={[
            { key: 'impact', label: t('removal.section.tabImpact') },
            {
              key: 'failures',
              label: t('removal.section.tabFailures'),
              badge: failuresQuery.data && failuresQuery.data.open_machine_items > 0
                ? <span className="tabular">{failuresQuery.data.open_machine_items}</span>
                : undefined,
            },
            { key: 'log', label: t('removal.section.tabLog') },
          ]}
          value={resultsTab}
          onChange={setResultsTab}
          ariaLabel={t('removal.section.resultsTabs')}
          className="w-full sm:w-auto"
        />
        {resultsTab === 'impact' && (
          <ImpactPanel
            data={impactQuery.data}
            isLoading={impactQuery.isLoading}
            isError={impactQuery.isError}
            days={impactDays}
            onDaysChange={setImpactDays}
          />
        )}
        {resultsTab === 'failures' && (
          <FailuresPanel
            data={failuresQuery.data}
            index={index}
            isLoading={failuresQuery.isLoading}
            isError={failuresQuery.isError}
          />
        )}
        {resultsTab === 'log' && (
          <AttemptsPanel
            attempts={attempts}
            rollup={rollup}
            index={index}
            isLoading={attemptsQuery.isLoading}
            outcome={outcomeFilter}
            onOutcomeChange={setOutcomeFilter}
          />
        )}
      </div>

      <ConfirmDialog
        open={confirmOpen}
        onClose={() => setConfirmOpen(false)}
        onConfirm={commit}
        size="lg"
        title={t('removal.section.confirmTitle')}
        confirmLabel={t('removal.section.confirmLabel')}
        loading={savePolicy.isPending}
        description={
          <Trans
            k="removal.section.confirmBody"
            values={{ machines: String(targetedCount), apps: selectedApps }}
            tags={{ b: (c) => <strong className="tabular font-semibold text-text">{c}</strong> }}
          />
        }
        requireText={String(targetedCount)}
        requireTextLabel={t('removal.section.confirmType')}
        requireTextHint={
          fleetKnown
            ? t('removal.section.confirmHintOnline', { count: radius.targetedOnline })
            : t('removal.section.confirmHintOffline')
        }
        acknowledgeLabel={t('removal.section.confirmAck')}
      >
        {draft.diff && (
          <div className="rounded-[var(--radius-md)] border border-border bg-surface-2/40 p-3.5">
            <PolicyDiff diff={draft.diff} index={index} />
          </div>
        )}
        <ul className="space-y-1.5 text-[13px] text-text-muted">
          <li className="flex items-start gap-2">
            <span className="mt-[7px] size-1.5 shrink-0 rounded-full bg-text-dim" aria-hidden />
            <span>
              {t('removal.section.confirmStartup', { count: selectedStartup })}
            </span>
          </li>
          {overview.data.posture.kill_switch_off > 0 && (
            <li className="flex items-start gap-2">
              <span className="mt-[7px] size-1.5 shrink-0 rounded-full bg-warn" aria-hidden />
              <span>
                {t('removal.section.confirmKilled', { count: overview.data.posture.kill_switch_off })}
              </span>
            </li>
          )}
          {overview.data.posture.drift.stale_machines > 0 && (
            <li className="flex items-start gap-2">
              <span className="mt-[7px] size-1.5 shrink-0 rounded-full bg-warn" aria-hidden />
              <span>
                {t('removal.section.confirmDrift', { count: overview.data.posture.drift.stale_machines })}
              </span>
            </li>
          )}
        </ul>
      </ConfirmDialog>
    </div>
  )
}
