import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { cn } from '@/lib/cn'

interface EmptyStateProps {
  icon?: LucideIcon
  title: string
  description?: string
  action?: ReactNode
  className?: string
}

export function EmptyState({ icon: Icon, title, description, action, className }: EmptyStateProps) {
  return (
    <div
      className={cn(
        'flex flex-col items-center justify-center rounded-[var(--radius-lg)] border border-dashed border-border-2 px-6 py-12 text-center',
        className,
      )}
    >
      {Icon && (
        <div className="mb-3 grid size-11 place-items-center rounded-full bg-glass text-text-dim">
          <Icon className="size-5" aria-hidden />
        </div>
      )}
      <p className="text-[15px] font-semibold text-text">{title}</p>
      {description && <p className="mt-1 max-w-sm text-[13px] text-text-muted">{description}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}
