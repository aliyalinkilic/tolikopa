import { forwardRef, useId } from 'react'
import type { SelectHTMLAttributes } from 'react'
import { ChevronDown } from 'lucide-react'
import { cn } from '@/lib/cn'

export interface SelectOption {
  value: string
  label: string
}

export interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  label?: string
  options: SelectOption[]
  placeholder?: string
}

export const Select = forwardRef<HTMLSelectElement, SelectProps>(function Select(
  { label, options, placeholder, className, id, ...props },
  ref,
) {
  const autoId = useId()
  const selectId = id ?? autoId
  return (
    <div className="w-full">
      {label && (
        <label htmlFor={selectId} className="mb-1.5 block text-[13px] font-medium text-text-muted">
          {label}
        </label>
      )}
      <div className="relative">
        <select
          id={selectId}
          ref={ref}
          className={cn(
            'h-10 w-full appearance-none rounded-[var(--radius-md)] border border-border-2 bg-surface-2/60 pl-3 pr-9 text-sm text-text',
            'outline-none transition focus:border-accent/60 focus:ring-2 focus:ring-accent/25',
            className,
          )}
          {...props}
        >
          {placeholder && (
            <option value="" className="bg-surface text-text">
              {placeholder}
            </option>
          )}
          {options.map((o) => (
            <option key={o.value} value={o.value} className="bg-surface text-text">
              {o.label}
            </option>
          ))}
        </select>
        <ChevronDown
          className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-text-dim"
          aria-hidden
        />
      </div>
    </div>
  )
})
