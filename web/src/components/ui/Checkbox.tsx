import { useEffect, useId, useRef } from 'react'
import type { ReactNode } from 'react'
import { motion, useReducedMotion } from 'motion/react'
import { cn } from '@/lib/cn'

export interface CheckboxProps {
  checked: boolean
  /** Renders the "mixed" state — used by select-all rows. Native `indeterminate`
   *  is set on the input, so assistive tech announces "mixed". */
  indeterminate?: boolean
  onChange: (checked: boolean) => void
  /** Visible label. Omit it for row-embedded checkboxes and pass `ariaLabel`. */
  label?: ReactNode
  description?: ReactNode
  /** Translated accessible name. Required when there is no visible `label`; when both
   *  are set it wins, so it must start with the visible text (WCAG 2.5.3). */
  ariaLabel?: string
  ariaDescribedBy?: string
  disabled?: boolean
  id?: string
  className?: string
  /** `sm` is the dense variant used inside catalog rows. */
  size?: 'sm' | 'md'
}

const boxSize: Record<'sm' | 'md', string> = {
  sm: 'size-[17px] rounded-[5px]',
  md: 'size-[19px] rounded-[6px]',
}

/**
 * Tri-state checkbox. A real `<input type="checkbox">` carries the semantics and
 * the keyboard behaviour (Space toggles, form/label association); the painted box
 * is decoration on top of it.
 */
export function Checkbox({
  checked,
  indeterminate = false,
  onChange,
  label,
  description,
  ariaLabel,
  ariaDescribedBy,
  disabled,
  id,
  className,
  size = 'md',
}: CheckboxProps) {
  const autoId = useId()
  const inputId = id ?? autoId
  const descId = `${inputId}-desc`
  const inputRef = useRef<HTMLInputElement>(null)
  const reduce = useReducedMotion()

  // `indeterminate` exists only as a DOM property, never as an attribute.
  useEffect(() => {
    if (inputRef.current) inputRef.current.indeterminate = indeterminate && !checked
  }, [indeterminate, checked])

  const on = checked || indeterminate
  const transition = reduce ? { duration: 0 } : { duration: 0.14, ease: [0.22, 1, 0.36, 1] as const }

  return (
    <div className={cn('flex items-start gap-2.5', className)}>
      <span
        className={cn(
          'ring-focus-within relative grid shrink-0 place-items-center border transition-colors',
          boxSize[size],
          on ? 'border-accent bg-accent text-on-accent' : 'border-border-2 bg-surface-2/60 text-transparent',
          disabled ? 'cursor-not-allowed opacity-45' : 'cursor-pointer hover:border-accent/60',
          label || description ? 'mt-px' : undefined,
        )}
      >
        <input
          ref={inputRef}
          id={inputId}
          type="checkbox"
          checked={checked}
          disabled={disabled}
          aria-label={ariaLabel}
          aria-describedby={ariaDescribedBy ?? (description ? descId : undefined)}
          onChange={(e) => onChange(e.target.checked)}
          className={cn(
            'absolute inset-0 size-full appearance-none bg-transparent outline-none',
            disabled ? 'cursor-not-allowed' : 'cursor-pointer',
          )}
        />
        <svg viewBox="0 0 16 16" className="pointer-events-none size-full p-[2px]" aria-hidden>
          {indeterminate && !checked ? (
            <motion.line
              x1="3.5"
              y1="8"
              x2="12.5"
              y2="8"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              initial={reduce ? false : { pathLength: 0, opacity: 0 }}
              animate={{ pathLength: 1, opacity: 1 }}
              transition={transition}
            />
          ) : (
            <motion.path
              d="M3.5 8.4 L6.6 11.4 L12.6 4.8"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              initial={false}
              animate={{ pathLength: checked ? 1 : 0, opacity: checked ? 1 : 0 }}
              transition={transition}
            />
          )}
        </svg>
      </span>

      {(label || description) && (
        <label
          htmlFor={inputId}
          className={cn('min-w-0 select-none', disabled ? 'cursor-not-allowed opacity-60' : 'cursor-pointer')}
        >
          {label && <span className="block text-[13px] font-medium text-text">{label}</span>}
          {description && (
            <span id={descId} className="mt-0.5 block text-[12px] text-text-muted">
              {description}
            </span>
          )}
        </label>
      )}
    </div>
  )
}
