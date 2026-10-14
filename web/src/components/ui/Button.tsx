import { forwardRef } from 'react'
import type { ButtonHTMLAttributes, ReactNode } from 'react'
import { Loader2 } from 'lucide-react'
import { cn } from '@/lib/cn'

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger'
type Size = 'sm' | 'md' | 'lg' | 'icon'

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant
  size?: Size
  loading?: boolean
  leftIcon?: ReactNode
}

const base =
  'inline-flex items-center justify-center gap-2 font-medium rounded-[var(--radius-md)] ' +
  'select-none whitespace-nowrap transition-[background,border-color,color,transform,box-shadow] ' +
  'duration-150 active:translate-y-px disabled:opacity-50 disabled:pointer-events-none ring-focus'

const variants: Record<Variant, string> = {
  primary: 'bg-accent-grad text-on-accent shadow-[var(--shadow-primary)] hover:brightness-110',
  secondary: 'bg-surface-2 text-text border border-border-2 hover:bg-surface-3',
  ghost: 'text-text-muted hover:text-text hover:bg-glass',
  danger: 'text-crit border border-crit/30 bg-crit/10 hover:bg-crit/20',
}

const sizes: Record<Size, string> = {
  sm: 'h-8 px-3 text-[13px]',
  md: 'h-10 px-4 text-sm',
  lg: 'h-11 px-5 text-[15px]',
  icon: 'h-9 w-9 p-0',
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = 'secondary', size = 'md', loading, leftIcon, className, children, disabled, type, ...props },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type ?? 'button'}
      disabled={disabled || loading}
      className={cn(base, variants[variant], sizes[size], className)}
      {...props}
    >
      {loading ? <Loader2 className="size-4 animate-spin" aria-hidden /> : leftIcon}
      {children}
    </button>
  )
})
