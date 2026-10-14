import { useEffect, useId, useRef } from 'react'
import type { ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { AnimatePresence, motion } from 'motion/react'
import { X } from 'lucide-react'
import { cn } from '@/lib/cn'
import { useT } from '@/i18n'

interface ModalProps {
  open: boolean
  onClose: () => void
  title?: ReactNode
  description?: ReactNode
  children?: ReactNode
  footer?: ReactNode
  size?: 'sm' | 'md' | 'lg'
  /** false = no close button, and Escape / backdrop click do nothing. For content
   *  that is shown once and must be acknowledged (recovery codes). */
  dismissible?: boolean
}

const sizeClass: Record<'sm' | 'md' | 'lg', string> = {
  sm: 'max-w-md',
  md: 'max-w-lg',
  lg: 'max-w-2xl',
}

const FOCUSABLE =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'

function focusableIn(root: HTMLElement): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
    (el) => el.offsetParent !== null || el === document.activeElement,
  )
}

export function Modal({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  size = 'md',
  dismissible = true,
}: ModalProps) {
  const t = useT()
  const panelRef = useRef<HTMLDivElement>(null)
  const titleId = useId()

  // Kept in a ref so the effect below depends on `open` alone — an inline
  // `onClose` arrow would otherwise re-run it (and steal focus) every render.
  const onCloseRef = useRef(onClose)
  const dismissibleRef = useRef(dismissible)
  useEffect(() => {
    onCloseRef.current = onClose
    dismissibleRef.current = dismissible
  })

  useEffect(() => {
    if (!open) return
    const previouslyFocused = document.activeElement as HTMLElement | null

    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (dismissibleRef.current) onCloseRef.current()
        return
      }
      // Focus trap: Tab cycles inside the panel and never escapes to the page.
      if (e.key !== 'Tab') return
      const panel = panelRef.current
      if (!panel) return
      const items = focusableIn(panel)
      if (items.length === 0) {
        e.preventDefault()
        panel.focus()
        return
      }
      const first = items[0]
      const last = items[items.length - 1]
      const active = document.activeElement as HTMLElement | null
      if (!panel.contains(active)) {
        e.preventDefault()
        ;(e.shiftKey ? last : first).focus()
      } else if (e.shiftKey && active === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && active === last) {
        e.preventDefault()
        first.focus()
      }
    }

    document.addEventListener('keydown', onKey)
    const prevOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    // Don't fight a child's autoFocus (e.g. the type-to-confirm input).
    if (!panelRef.current?.contains(document.activeElement)) panelRef.current?.focus()

    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = prevOverflow
      // Restore focus to whatever opened the dialog, if it is still on the page.
      if (previouslyFocused && document.contains(previouslyFocused)) previouslyFocused.focus()
    }
  }, [open])

  return createPortal(
    <AnimatePresence>
      {open && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
          <motion.div
            className="absolute inset-0 bg-scrim backdrop-blur-sm"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            onClick={dismissible ? onClose : undefined}
          />
          <motion.div
            ref={panelRef}
            tabIndex={-1}
            role="dialog"
            aria-modal="true"
            aria-labelledby={title ? titleId : undefined}
            className={cn('glass relative w-full rounded-[var(--radius-xl)] border border-border-2 shadow-[var(--shadow-pop)] outline-none', sizeClass[size])}
            initial={{ opacity: 0, scale: 0.96, y: 12 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.97, y: 8 }}
            transition={{ type: 'spring', stiffness: 260, damping: 26 }}
          >
            <div className="flex items-start justify-between gap-4 p-5 pb-3">
              <div className="min-w-0">
                {title && (
                  <h2 id={titleId} className="text-[17px] font-semibold text-text">
                    {title}
                  </h2>
                )}
                {description && <p className="mt-1 text-[13px] text-text-muted">{description}</p>}
              </div>
              {dismissible && <button
                type="button"
                onClick={onClose}
                aria-label={t('common.close')}
                className="grid size-8 shrink-0 place-items-center rounded-full text-text-muted transition-colors hover:bg-wash-2 hover:text-text ring-focus"
              >
                <X className="size-4" aria-hidden />
              </button>}
            </div>
            <div className="max-h-[70vh] overflow-y-auto px-5 pb-1">{children}</div>
            {footer && <div className="flex justify-end gap-2 border-t border-border p-4">{footer}</div>}
          </motion.div>
        </div>
      )}
    </AnimatePresence>,
    document.body,
  )
}
