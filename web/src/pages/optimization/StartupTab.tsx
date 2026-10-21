import { useMemo, useState } from 'react'
import { motion } from 'motion/react'
import { FlaskConical, Info, Power, ShieldOff } from 'lucide-react'
import { CatalogList } from '@/components/removal/CatalogList'
import { AttemptsPanel } from '@/components/removal/AttemptsPanel'
import { KpiCard } from '@/components/KpiCard'
import { useRemediationAttempts } from '@/hooks/queries'
import { rollupAttempts } from '@/lib/remediation'
import { staggerContainer } from '@/lib/motion'
import type { RemediationPostureResponse } from '@/lib/types'
import { useT } from '@/i18n'
import { ItemProgressTable } from './ItemProgressTable'
import type { PolicyEditor } from './usePolicyEditor'

interface Props {
  editor: PolicyEditor
  posture: RemediationPostureResponse | undefined
  postureLoading: boolean
}

/**
 * Logon startup entries. Disabling writes the same StartupApproved value Task
 * Manager writes and never deletes the Run value — reversible by the user. The
 * selection lives in the same policy as app removal (one mode, one scope).
 */
export function StartupTab({ editor, posture, postureLoading }: Props) {
  const t = useT()
  const { draft, data } = editor
  const [outcome, setOutcome] = useState('')
  const attemptsQuery = useRemediationAttempts({ limit: 200, outcome: outcome || undefined })
  // The audit feed has no kind filter; startup rows are picked out here.
  const attempts = useMemo(
    () => (attemptsQuery.data?.attempts ?? []).filter((a) => a.kind === 'startup'),
    [attemptsQuery.data],
  )
  const rollup = useMemo(() => rollupAttempts(attempts), [attempts])

  const win = posture?.platforms.find((p) => p.kind === 'windows')
  const reporting = win?.reporting ?? data?.posture.reporting_machines ?? 0
  const allowed = win?.allow_startup ?? 0
  const notAllowed = data?.posture.startup_not_allowed ?? 0
  const enforceOff = data?.posture.enforce_off ?? 0

  return (
    <div className="space-y-4">
      <p className="flex items-start gap-2 text-[13px] text-text-muted">
        <Info className="mt-0.5 size-4 shrink-0 text-text-dim" aria-hidden />
        <span>{t('optimization.startup.intro')}</span>
      </p>

      <motion.div
        variants={staggerContainer()}
        initial="hidden"
        animate="show"
        className="grid gap-3 sm:grid-cols-3"
      >
        <KpiCard
          label={t('optimization.startup.allowed')}
          value={allowed}
          icon={Power}
          tone={allowed > 0 ? 'ok' : 'warn'}
          footer={t('optimization.startup.allowedHint', { total: reporting })}
        />
        <KpiCard
          label={t('optimization.startup.notAllowed')}
          value={notAllowed}
          icon={ShieldOff}
          tone={notAllowed > 0 ? 'warn' : 'ok'}
          footer={t('optimization.startup.notAllowedHint')}
        />
        <KpiCard
          label={t('optimization.startup.enforceOff')}
          value={enforceOff}
          icon={FlaskConical}
          tone={enforceOff > 0 ? 'info' : 'ok'}
          footer={t('optimization.startup.enforceOffHint')}
        />
      </motion.div>

      <CatalogList
        kind="startup"
        catalog={editor.catalog}
        isSelected={draft.isSelected}
        onToggle={draft.toggle}
        onSetMany={draft.setMany}
        disabled={editor.saving}
        title={t('optimization.startup.catalogTitle')}
        description={t('optimization.startup.catalogBody')}
      />

      <ItemProgressTable
        kind="startup"
        items={posture?.items}
        index={editor.index}
        isLoading={postureLoading}
        title={t('optimization.startup.progressTitle')}
        description={t('optimization.startup.progressBody')}
      />

      <AttemptsPanel
        attempts={attempts}
        rollup={rollup}
        index={editor.index}
        isLoading={attemptsQuery.isLoading}
        outcome={outcome}
        onOutcomeChange={setOutcome}
      />
    </div>
  )
}
