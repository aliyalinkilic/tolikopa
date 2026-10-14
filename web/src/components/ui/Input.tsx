import { forwardRef, useId } from 'react'
import type { InputHTMLAttributes, ReactNode } from 'react'
import { cn } from '@/lib/cn'

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string
  hint?: string
  error?: string
  leftIcon?: ReactNode
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { label, hint, error, leftIcon, className, id, ...props },
  ref,
) {
  const autoId = useId()
  const inputId = id ?? autoId
  return (
    <div className="w-full">
      {label && (
        <label htmlFor={inputId} className="mb-1.5 block text-[13px] font-medium text-text-muted">
          {label}
        </label>
      )}
      <div className="relative">
        {leftIcon && (
          <span className="pointer-events-none absolute inset-y-0 left-3 flex items-center text-text-dim">
            {leftIcon}
          </span>
        )}
        <input
          id={inputId}
          ref={ref}
          aria-invalid={error ? true : undefined}
          className={cn(
            'h-10 w-full rounded-[var(--radius-md)] border border-border-2 bg-surface-2/60 px-3 text-sm text-text',
            'placeholder:text-text-dim outline-none transition',
            'focus:border-accent/60 focus:ring-2 focus:ring-accent/25',
            leftIcon ? 'pl-9' : undefined,
            error ? 'border-crit/60' : undefined,
            className,
          )}
          {...props}
        />
      </div>
      {error ? (
        <p className="mt-1 text-[12px] text-crit">{error}</p>
      ) : hint ? (
        <p className="mt-1 text-[12px] text-text-dim">{hint}</p>
      ) : null}
    </div>
  )
})
