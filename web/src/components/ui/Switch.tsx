import { useId } from 'react'
import { cn } from '@/lib/cn'

interface SwitchProps {
  checked: boolean
  onChange: (value: boolean) => void
  label?: string
  disabled?: boolean
  id?: string
}

export function Switch({ checked, onChange, label, disabled, id }: SwitchProps) {
  const autoId = useId()
  return (
    <button
      id={id ?? autoId}
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cn(
        'relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors ring-focus',
        checked ? 'bg-accent' : 'bg-wash-3',
        disabled && 'opacity-50',
      )}
    >
      <span
        className={cn(
          'inline-block size-4 transform rounded-full bg-knob shadow transition-transform',
          checked ? 'translate-x-[18px]' : 'translate-x-0.5',
        )}
      />
    </button>
  )
}
