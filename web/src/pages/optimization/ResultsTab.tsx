import { useMemo, useState } from 'react'
import { Tabs } from '@/components/ui/Tabs'
import { AttemptsPanel } from '@/components/removal/AttemptsPanel'
import { FailuresPanel } from '@/components/removal/FailuresPanel'
import { ImpactPanel } from '@/components/removal/ImpactPanel'
import { useRemediationAttempts, useRemediationFailures, useRemediationImpact } from '@/hooks/queries'
import { rollupAttempts } from '@/lib/remediation'
import { useT } from '@/i18n'
import type { PolicyEditor } from './usePolicyEditor'

/** What the policy actually did: usage impact, still-open failures, audit log. */
export function ResultsTab({ editor }: { editor: PolicyEditor }) {
  const t = useT()
  const [view, setView] = useState('impact')
  const [impactDays, setImpactDays] = useState(7)
  const [outcome, setOutcome] = useState('')
  const failuresQuery = useRemediationFailures()
  const impactQuery = useRemediationImpact(impactDays)
  const attemptsQuery = useRemediationAttempts({ limit: 200, outcome: outcome || undefined })
  const attempts = useMemo(() => attemptsQuery.data?.attempts ?? [], [attemptsQuery.data])
  const rollup = useMemo(
    () => attemptsQuery.data?.rollup ?? rollupAttempts(attempts),
    [attemptsQuery.data, attempts],
  )
  const openCount = failuresQuery.data?.open_machine_items ?? 0

  return (
    <div className="space-y-4">
      <Tabs
        tabs={[
          { key: 'impact', label: t('removal.section.tabImpact') },
          {
            key: 'failures',
            label: t('removal.section.tabFailures'),
            badge: openCount > 0 ? <span className="tabular">{openCount}</span> : undefined,
          },
          { key: 'log', label: t('removal.section.tabLog') },
        ]}
        value={view}
        onChange={setView}
        ariaLabel={t('removal.section.resultsTabs')}
        className="w-full sm:w-auto"
      />
      {view === 'impact' && (
        <ImpactPanel
          data={impactQuery.data}
          isLoading={impactQuery.isLoading}
          isError={impactQuery.isError}
          days={impactDays}
          onDaysChange={setImpactDays}
        />
      )}
      {view === 'failures' && (
        <FailuresPanel
          data={failuresQuery.data}
          index={editor.index}
          isLoading={failuresQuery.isLoading}
          isError={failuresQuery.isError}
        />
      )}
      {view === 'log' && (
        <AttemptsPanel
          attempts={attempts}
          rollup={rollup}
          index={editor.index}
          isLoading={attemptsQuery.isLoading}
          outcome={outcome}
          onOutcomeChange={setOutcome}
        />
      )}
    </div>
  )
}
