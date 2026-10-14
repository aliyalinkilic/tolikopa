import type { CSSProperties, ReactNode } from 'react'
import { cn } from '@/lib/cn'
import type { StatusToken } from '@/lib/labels'

export type BadgeTone = StatusToken | 'neutral' | 'accent'

const toneClasses: Record<BadgeTone, string> = {
  ok: 'text-ok bg-ok/12 border-ok/25',
  warn: 'text-warn bg-warn/12 border-warn/25',
  crit: 'text-crit bg-crit/12 border-crit/25',
  offline: 'text-text-dim bg-wash border-border-2',
  info: 'text-info bg-info/12 border-info/25',
  neutral: 'text-text-muted bg-wash border-border-2',
  accent: 'text-accent bg-accent/12 border-accent/25',
}

const dotColor: Record<BadgeTone, string> = {
  ok: 'bg-ok',
  warn: 'bg-warn',
  crit: 'bg-crit',
  offline: 'bg-offline',
  info: 'bg-info',
  neutral: 'bg-text-dim',
  accent: 'bg-accent',
}

interface BadgeProps {
  tone?: BadgeTone
  children: ReactNode
  dot?: boolean
  pulse?: boolean
  icon?: ReactNode
  className?: string
  title?: string
}

export function Badge({ tone = 'neutral', children, dot, pulse, icon, className, title }: BadgeProps) {
  return (
    <span
      title={title}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[12px] font-medium leading-5',
        toneClasses[tone],
        className,
      )}
    >
      {dot && (
        <span
          className={cn('size-1.5 rounded-full', dotColor[tone], pulse && 'live-dot')}
          style={pulse ? ({ '--pulse-color': 'currentColor' } as CSSProperties) : undefined}
          aria-hidden
        />
      )}
      {icon}
      {children}
    </span>
  )
}
