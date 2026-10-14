import { AnimatePresence, motion } from 'motion/react'
import { useNavigate } from 'react-router-dom'
import { CheckCircle2, Info, ShieldAlert, TriangleAlert, X } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { useToast } from '@/hooks/useToast'
import { statusColor } from '@/lib/labels'
import type { StatusToken } from '@/lib/labels'
import { tint } from '@/lib/theme'
import { cn } from '@/lib/cn'
import { useT } from '@/i18n'

const toastIcon: Record<StatusToken, LucideIcon> = {
  crit: ShieldAlert,
  warn: TriangleAlert,
  info: Info,
  ok: CheckCircle2,
  offline: Info,
}

export function ToastHost() {
  const t = useT()
  const { toasts, dismiss } = useToast()
  const navigate = useNavigate()

  return (
    <div className="pointer-events-none fixed bottom-4 right-4 z-[60] flex w-[min(92vw,360px)] flex-col gap-2">
      <AnimatePresence initial={false}>
        {toasts.map((toast) => {
          const Icon = toastIcon[toast.tone]
          const color = statusColor[toast.tone]
          const href = toast.href
          const open = href
            ? () => {
                navigate(href)
                dismiss(toast.id)
              }
            : undefined
          return (
            <motion.div
              key={toast.id}
              layout
              initial={{ opacity: 0, x: 40, scale: 0.98 }}
              animate={{ opacity: 1, x: 0, scale: 1 }}
              exit={{ opacity: 0, x: 40, scale: 0.98 }}
              transition={{ type: 'spring', stiffness: 300, damping: 30 }}
              onClick={open}
              role={open ? 'button' : 'status'}
              tabIndex={open ? 0 : undefined}
              onKeyDown={
                open
                  ? (e) => {
                      if (e.key === 'Enter') open()
                    }
                  : undefined
              }
              className={cn(
                'glass pointer-events-auto relative flex items-start gap-3 overflow-hidden rounded-[var(--radius-lg)] border border-border-2 py-3 pl-4 pr-3 shadow-[var(--shadow-pop)]',
                open && 'cursor-pointer ring-focus',
              )}
            >
              <span className="absolute inset-y-0 left-0 w-1 rounded-l-[var(--radius-lg)]" style={{ backgroundColor: color }} aria-hidden />
              <div className="grid size-8 shrink-0 place-items-center rounded-[10px]" style={{ backgroundColor: tint(color, 14), color }}>
                <Icon className="size-4" aria-hidden />
              </div>
              <div className="min-w-0 flex-1">
                <p className="truncate text-[13px] font-semibold text-text">{toast.title}</p>
                {toast.description && <p className="mt-0.5 line-clamp-2 text-[12px] text-text-muted">{toast.description}</p>}
              </div>
              <button
                type="button"
                aria-label={t('shell.dismissToast')}
                onClick={(e) => {
                  e.stopPropagation()
                  dismiss(toast.id)
                }}
                className="grid size-6 shrink-0 place-items-center rounded-full text-text-dim transition-colors hover:bg-wash-2 hover:text-text ring-focus"
              >
                <X className="size-3.5" aria-hidden />
              </button>
            </motion.div>
          )
        })}
      </AnimatePresence>
    </div>
  )
}
