import { Info } from 'lucide-react'
import { ModeControl } from '@/components/removal/ModeControl'
import { RemovalSummary } from '@/components/removal/RemovalSummary'
import { TagExclusionEditor } from '@/components/removal/TagExclusionEditor'
import { useT } from '@/i18n'
import type { PolicyEditor } from './usePolicyEditor'

/** Mode + scope of the one fleet-wide policy. Both apps and startup entries
 *  follow it, which is why it is a tab of its own rather than part of either. */
export function PolicyTab({ editor }: { editor: PolicyEditor }) {
  const t = useT()
  const { data, draft } = editor
  if (!data || !draft.draft) return null

  return (
    <div className="space-y-4">
      <p className="flex items-start gap-2 text-[13px] text-text-muted">
        <Info className="mt-0.5 size-4 shrink-0 text-text-dim" aria-hidden />
        <span>{t('removal.section.intro')}</span>
      </p>

      <RemovalSummary posture={data.posture} radius={editor.radius} fleetKnown={editor.fleetKnown} />

      <ModeControl
        mode={draft.mode}
        savedMode={editor.savedMode}
        onChange={draft.setMode}
        enforceBlockedReason={editor.enforceBlockedReason}
        lastDryRunAt={data.posture.last_dry_run_at}
        disabled={editor.saving}
      />

      <TagExclusionEditor
        value={editor.excludedTags}
        onChange={draft.setExcludedTags}
        universe={editor.universe}
        excludedMachines={editor.radius.excluded}
        disabled={editor.saving}
      />
    </div>
  )
}
