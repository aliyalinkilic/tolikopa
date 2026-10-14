import type { HTMLAttributes, ReactNode } from 'react'
import { cn } from '@/lib/cn'

interface CardProps extends HTMLAttributes<HTMLDivElement> {
  padded?: boolean
  interactive?: boolean
}

export function Card({ padded = true, interactive = false, className, children, ...props }: CardProps) {
  return (
    <div
      className={cn(
        'card',
        padded && 'p-5',
        interactive && 'transition-colors duration-200 hover:bg-surface-2',
        className,
      )}
      {...props}
    >
      {children}
    </div>
  )
}

interface CardHeaderProps {
  title: ReactNode
  eyebrow?: ReactNode
  description?: ReactNode
  action?: ReactNode
  className?: string
}

export function CardHeader({ title, eyebrow, description, action, className }: CardHeaderProps) {
  return (
    <div className={cn('flex items-start justify-between gap-4', className)}>
      <div className="min-w-0">
        {eyebrow && (
          <div className="mb-1 text-[11px] font-semibold uppercase tracking-[0.14em] text-text-dim">
            {eyebrow}
          </div>
        )}
        <h3 className="text-[15px] font-semibold text-text">{title}</h3>
        {description && <p className="mt-1 text-[13px] text-text-muted">{description}</p>}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  )
}
