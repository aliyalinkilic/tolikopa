import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { RotateCcw, Save } from 'lucide-react'
import { Button } from '@/components/ui/Button'
import { Card, CardHeader } from '@/components/ui/Card'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { PolicyDiff } from '@/components/removal/PolicyDiff'
import { useT } from '@/i18n'
import { Trans } from '@/i18n/rich'
import type { PolicyEditor } from './usePolicyEditor'

/** Concurrent-edit notice: another admin saved while this draft was open. */
export function StalePolicyNotice({ editor }: { editor: PolicyEditor }) {
  const t = useT()
  if (!editor.draft.stale) return null
  return (
    <Card className="border-warn/25 bg-warn/[0.06]">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-[13px] text-text-muted">{t('removal.section.stale')}</p>
        <Button
          size="sm"
          variant="secondary"
          leftIcon={<RotateCcw className="size-4" aria-hidden />}
          onClick={editor.draft.reset}
        >
          {t('removal.section.loadServer')}
        </Button>
      </div>
    </Card>
  )
}

/**
 * The page-level "unsaved changes" bar plus the type-to-confirm dialog for arming
 * enforce mode. Rendered once, below whichever tab is open, so a selection made
 * on "Apps" and one made on "Startup Entries" are saved together.
 */
export function PolicySaveBar({ editor }: { editor: PolicyEditor }) {
  const t = useT()
  const reduce = useReducedMotion()
  const { draft, data } = editor
  const show = draft.dirty && !!draft.diff

  return (
    <>
      <AnimatePresence>
        {show && draft.diff && (
          <motion.div
            key="save-bar"
            initial={reduce ? false : { opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            exit={reduce ? { opacity: 0 } : { opacity: 0, y: 12 }}
            transition={{ duration: 0.2, ease: [0.22, 1, 0.36, 1] }}
            className="sticky bottom-4 z-20"
          >
            <Card className="glass border-accent/30 shadow-[var(--shadow-pop)]">
              <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
                <div className="min-w-0">
                  <CardHeader
                    title={t('removal.section.unsavedTitle')}
                    description={t('removal.section.unsavedSummary', {
                      apps: editor.selectedApps,
                      startup: editor.selectedStartup,
                    })}
                  />
                  <PolicyDiff diff={draft.diff} index={editor.index} className="mt-3" />
                </div>
                <div className="flex shrink-0 gap-2">
                  <Button
                    variant="ghost"
                    leftIcon={<RotateCcw className="size-4" aria-hidden />}
                    onClick={draft.reset}
                    disabled={editor.saving}
                  >
                    {t('common.undo')}
                  </Button>
                  <Button
                    variant={editor.willEnforce ? 'danger' : 'primary'}
                    leftIcon={<Save className="size-4" aria-hidden />}
                    loading={editor.saving}
                    onClick={editor.requestSave}
                  >
                    {editor.willEnforce ? t('removal.section.saveAndEnforce') : t('common.save')}
                  </Button>
                </div>
              </div>
            </Card>
          </motion.div>
        )}
      </AnimatePresence>

      <ConfirmDialog
        open={editor.confirmOpen}
        onClose={() => editor.setConfirmOpen(false)}
        onConfirm={editor.commit}
        size="lg"
        title={t('removal.section.confirmTitle')}
        confirmLabel={t('removal.section.confirmLabel')}
        loading={editor.saving}
        description={
          <Trans
            k="removal.section.confirmBody"
            values={{ machines: String(editor.targetedCount), apps: editor.selectedApps }}
            tags={{ b: (c) => <strong className="tabular font-semibold text-text">{c}</strong> }}
          />
        }
        requireText={String(editor.targetedCount)}
        requireTextLabel={t('removal.section.confirmType')}
        requireTextHint={
          editor.fleetKnown
            ? t('removal.section.confirmHintOnline', { count: editor.radius.targetedOnline })
            : t('removal.section.confirmHintOffline')
        }
        acknowledgeLabel={t('removal.section.confirmAck')}
      >
        {draft.diff && (
          <div className="rounded-[var(--radius-md)] border border-border bg-surface-2/40 p-3.5">
            <PolicyDiff diff={draft.diff} index={editor.index} />
          </div>
        )}
        <ul className="space-y-1.5 text-[13px] text-text-muted">
          <li className="flex items-start gap-2">
            <span className="mt-[7px] size-1.5 shrink-0 rounded-full bg-text-dim" aria-hidden />
            <span>{t('removal.section.confirmStartup', { count: editor.selectedStartup })}</span>
          </li>
          {data && data.posture.kill_switch_off > 0 && (
            <li className="flex items-start gap-2">
              <span className="mt-[7px] size-1.5 shrink-0 rounded-full bg-warn" aria-hidden />
              <span>{t('removal.section.confirmKilled', { count: data.posture.kill_switch_off })}</span>
            </li>
          )}
          {data && data.posture.drift.stale_machines > 0 && (
            <li className="flex items-start gap-2">
              <span className="mt-[7px] size-1.5 shrink-0 rounded-full bg-warn" aria-hidden />
              <span>{t('removal.section.confirmDrift', { count: data.posture.drift.stale_machines })}</span>
            </li>
          )}
        </ul>
      </ConfirmDialog>
    </>
  )
}
