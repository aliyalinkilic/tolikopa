import { useMemo, useState } from 'react'
import {
  useFleet, useRemediationAttempts, useRemediationOverview, useUpdateRemediationPolicy,
} from '@/hooks/queries'
import { useRemovalDraft } from '@/hooks/useRemovalDraft'
import type { RemovalDraftApi } from '@/hooks/useRemovalDraft'
import { useToast } from '@/hooks/useToast'
import {
  blastRadius, hasDryRunEvidence, indexCatalog, modeOf, tagUniverse,
} from '@/lib/remediation'
import type { BlastRadius, TagUsage } from '@/lib/remediation'
import { removalModeMeta } from '@/lib/labels'
import type { RemovalMode } from '@/lib/labels'
import type { RemediationCatalogGroups, RemediationCatalogItem, RemediationOverview } from '@/lib/types'
import { useT } from '@/i18n'

const EMPTY_CATALOG: RemediationCatalogGroups = { app: [], startup: [] }

export interface PolicyEditor {
  overview: ReturnType<typeof useRemediationOverview>
  data: RemediationOverview | undefined
  draft: RemovalDraftApi
  catalog: RemediationCatalogGroups
  index: Map<string, RemediationCatalogItem>
  radius: BlastRadius
  universe: TagUsage[]
  excludedTags: string[]
  fleetKnown: boolean
  targetedCount: number
  savedMode: RemovalMode
  enforceBlockedReason: string | null
  selectedApps: number
  selectedStartup: number
  willEnforce: boolean
  saving: boolean
  confirmOpen: boolean
  setConfirmOpen: (open: boolean) => void
  /** Save, or open the type-to-confirm dialog first when the draft arms enforce. */
  requestSave: () => void
  commit: () => void
}

/**
 * The single fleet-wide hygiene policy, as one editing session for the whole
 * Optimization page. Apps and startup entries share it (one mode, one exclusion
 * list, one revision), so the draft lives ABOVE the tabs: switching from
 * "Apps" to "Startup Entries" must not drop what was ticked on the other tab.
 */
export function usePolicyEditor(): PolicyEditor {
  const t = useT()
  const overview = useRemediationOverview()
  const fleet = useFleet()
  const savePolicy = useUpdateRemediationPolicy()
  const { push } = useToast()
  const draft = useRemovalDraft(overview.data?.policy)
  const [confirmOpen, setConfirmOpen] = useState(false)

  const catalog = overview.data?.catalog ?? EMPTY_CATALOG
  const index = useMemo(() => indexCatalog(catalog), [catalog])

  const excludedTags = useMemo(() => draft.draft?.excluded_tags ?? [], [draft.draft])
  const radius = useMemo(() => blastRadius(fleet.data, excludedTags), [fleet.data, excludedTags])
  const universe = useMemo(() => tagUniverse(fleet.data), [fleet.data])

  const posture = overview.data?.posture
  // The server owns this flag; a dedicated one-row probe is a second, local
  // witness so the interlock still works against a backend that does not compute
  // it yet. It must NOT read a user-filtered attempts query — an outcome filter
  // would silently unlock or relock enforce mode.
  const dryRunProbe = useRemediationAttempts({ limit: 1, outcome: 'dry_run' })
  const dryRunObserved =
    (posture?.dry_run_observed ?? false) || hasDryRunEvidence(dryRunProbe.data?.attempts)

  const savedMode: RemovalMode = overview.data ? modeOf(overview.data.policy) : 'off'
  const enforceBlockedReason = (() => {
    if (savedMode === 'enforce') return null // already enforcing; the interlock is for the first switch
    if (!dryRunObserved) {
      // A dry-run row can only come from a machine whose local GPO switch is on:
      // with the switch off the agent writes no attempt at all, so "wait for a
      // result" would be waiting forever — name the real blocker.
      const reporting = posture?.reporting_machines ?? 0
      const killed = posture?.kill_switch_off ?? 0
      if (reporting > 0 && killed >= reporting) return t('removal.section.blockNoGpo')
      if (savedMode === 'dry_run') return t('removal.section.blockWaiting')
      return t('removal.section.blockNeedDryRun')
    }
    if (draft.diff?.selectionChanged) return t('removal.section.blockSelectionChanged')
    return null
  })()

  const selectedApps = draft.draft?.apps.length ?? 0
  const selectedStartup = draft.draft?.startup.length ?? 0
  const willEnforce = draft.draft ? modeOf(draft.draft) === 'enforce' : false

  // Blast radius is computed from the live fleet (it knows the tags); while that
  // list is unavailable fall back to the server's own count rather than claim 0.
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

  const requestSave = () => {
    if (willEnforce) setConfirmOpen(true)
    else commit()
  }

  return {
    overview,
    data: overview.data,
    draft,
    catalog,
    index,
    radius,
    universe,
    excludedTags,
    fleetKnown,
    targetedCount,
    savedMode,
    enforceBlockedReason,
    selectedApps,
    selectedStartup,
    willEnforce,
    saving: savePolicy.isPending,
    confirmOpen,
    setConfirmOpen,
    requestSave,
    commit,
  }
}
