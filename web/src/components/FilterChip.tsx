import type { ReactNode } from 'react'
import { cn } from '@/lib/cn'

export interface FilterChipProps {
  active: boolean
  onClick: () => void
  /** Optional trailing count — always paired with the label, never on its own. */
  count?: number
  children: ReactNode
  disabled?: boolean
  title?: string
  /** Accessible label for chips whose text alone is not self-describing. */
  ariaLabel?: string
  className?: string
}

/** Pill-shaped toggle used for list filters. `aria-pressed` carries the state so
 *  colour is never the only signal. */
export function FilterChip({
  active,
  onClick,
  count,
  children,
  disabled,
  title,
  ariaLabel,
  className,
}: FilterChipProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      aria-label={ariaLabel}
      title={title}
      disabled={disabled}
      className={cn(
        'inline-flex shrink-0 items-center gap-1.5 rounded-full border px-3 py-1 text-[13px] font-medium transition-colors ring-focus',
        'disabled:cursor-not-allowed disabled:opacity-40',
        active
          ? 'border-accent/30 bg-accent/12 text-accent'
          : 'border-border-2 bg-surface/60 text-text-muted hover:text-text',
        className,
      )}
    >
      {children}
      {count !== undefined && (
        <span className={cn('tabular text-[11px]', active ? 'text-accent/80' : 'text-text-dim')}>{count}</span>
      )}
    </button>
  )
}
