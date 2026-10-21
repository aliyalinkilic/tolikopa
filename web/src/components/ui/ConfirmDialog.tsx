import { useEffect, useId, useState } from 'react'
import type { ReactNode } from 'react'
import { AlertTriangle } from 'lucide-react'
import { Modal } from './Modal'
import { Button } from './Button'
import { Checkbox } from './Checkbox'
import { cn } from '@/lib/cn'
import { useT } from '@/i18n'

export interface ConfirmDialogProps {
  open: boolean
  onClose: () => void
  onConfirm: () => void
  title: string
  description?: ReactNode
  /** Extra body content — e.g. the policy diff / blast-radius breakdown. */
  children?: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  loading?: boolean
  tone?: 'danger' | 'primary'
  size?: 'sm' | 'md' | 'lg'
  /** Type-to-confirm. The admin must type this exact string (we use the number of
   *  affected machines) before the confirm button unlocks. */
  requireText?: string
  requireTextLabel?: string
  requireTextHint?: string
  /** A labelled acknowledgement tick, on its own or on top of `requireText`. */
  acknowledgeLabel?: ReactNode
  /** Extra gate from the caller, e.g. a form inside `children` that is not valid yet. */
  confirmDisabled?: boolean
}

/**
 * Destructive-action gate. Nothing here fires on a single click: the caller
 * decides which acknowledgement is proportionate — a tick for a local delete, the
 * typed machine count for anything that reaches the fleet.
 */
export function ConfirmDialog({
  open,
  onClose,
  onConfirm,
  title,
  description,
  children,
  confirmLabel,
  cancelLabel,
  loading,
  tone = 'danger',
  size = 'md',
  requireText,
  requireTextLabel,
  requireTextHint,
  acknowledgeLabel,
  confirmDisabled = false,
}: ConfirmDialogProps) {
  const t = useT()
  const [typed, setTyped] = useState('')
  const [acked, setAcked] = useState(false)
  const inputId = useId()

  // Every opening starts from a clean acknowledgement.
  useEffect(() => {
    if (open) {
      setTyped('')
      setAcked(false)
    }
  }, [open])

  const textOk = !requireText || typed.trim() === requireText
  const ackOk = !acknowledgeLabel || acked
  const ready = textOk && ackOk && !confirmDisabled

  const confirm = () => {
    if (!ready || loading) return
    onConfirm()
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={title}
      size={size}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={loading}>
            {cancelLabel ?? t('common.dismiss')}
          </Button>
          <Button
            variant={tone === 'danger' ? 'danger' : 'primary'}
            loading={loading}
            disabled={!ready}
            onClick={confirm}
          >
            {confirmLabel ?? t('common.confirm')}
          </Button>
        </>
      }
    >
      <div className="space-y-4 pb-2">
        <div
          className={cn(
            'flex items-start gap-3 rounded-[var(--radius-md)] border p-3.5',
            tone === 'danger' ? 'border-crit/25 bg-crit/[0.07]' : 'border-accent/25 bg-accent/[0.07]',
          )}
        >
          <AlertTriangle
            className={cn('mt-0.5 size-4 shrink-0', tone === 'danger' ? 'text-crit' : 'text-accent')}
            aria-hidden
          />
          <div className="min-w-0 text-[13px] text-text-muted">{description}</div>
        </div>

        {children}

        {requireText && (
          <div>
            <label htmlFor={inputId} className="mb-1.5 block text-[13px] font-medium text-text-muted">
              {requireTextLabel ?? t('ui.confirm.typeNumber')}:{' '}
              <span className="tabular font-semibold text-text">{requireText}</span>
            </label>
            <input
              id={inputId}
              value={typed}
              autoFocus
              autoComplete="off"
              inputMode={/^\d+$/.test(requireText) ? 'numeric' : 'text'}
              onChange={(e) => setTyped(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  confirm()
                }
              }}
              aria-describedby={requireTextHint ? `${inputId}-hint` : undefined}
              className={cn(
                'tabular h-10 w-full rounded-[var(--radius-md)] border bg-surface-2/60 px-3 text-sm text-text',
                'outline-none transition placeholder:text-text-dim focus:border-accent/60 focus:ring-2 focus:ring-accent/25',
                typed && !textOk ? 'border-crit/60' : 'border-border-2',
              )}
              placeholder={requireText}
            />
            {requireTextHint && (
              <p id={`${inputId}-hint`} className="mt-1 text-[12px] text-text-dim">
                {requireTextHint}
              </p>
            )}
          </div>
        )}

        {acknowledgeLabel && (
          <Checkbox checked={acked} onChange={setAcked} label={acknowledgeLabel} />
        )}
      </div>
    </Modal>
  )
}
