import { useId, useRef } from 'react'
import type { KeyboardEvent, ReactNode } from 'react'
import { motion, useReducedMotion } from 'motion/react'
import { cn } from '@/lib/cn'

export interface TabItem {
  key: string
  label: string
  icon?: ReactNode
  badge?: ReactNode
}

interface TabsProps {
  tabs: TabItem[]
  value: string
  onChange: (key: string) => void
  className?: string
  ariaLabel?: string
}

export function Tabs({ tabs, value, onChange, className, ariaLabel }: TabsProps) {
  const layoutId = useId()
  const reduce = useReducedMotion()
  const refs = useRef<(HTMLButtonElement | null)[]>([])

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const idx = tabs.findIndex((tab) => tab.key === value)
    let next = idx
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = (idx + 1) % tabs.length
    else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') next = (idx - 1 + tabs.length) % tabs.length
    else if (e.key === 'Home') next = 0
    else if (e.key === 'End') next = tabs.length - 1
    else return
    e.preventDefault()
    onChange(tabs[next].key)
    refs.current[next]?.focus()
  }

  return (
    <div
      role="tablist"
      aria-label={ariaLabel}
      onKeyDown={onKeyDown}
      className={cn('flex gap-1 overflow-x-auto rounded-[var(--radius-md)] bg-bg-elev/60 p-1', className)}
    >
      {tabs.map((x, i) => {
        const active = x.key === value
        return (
          <button
            key={x.key}
            ref={(el) => {
              refs.current[i] = el
            }}
            role="tab"
            type="button"
            aria-selected={active}
            // With no active tab (value matches nothing) the first one stays reachable.
            tabIndex={active || (i === 0 && !tabs.some((x) => x.key === value)) ? 0 : -1}
            onClick={() => onChange(x.key)}
            className={cn(
              'relative inline-flex shrink-0 items-center gap-2 rounded-[10px] px-3.5 py-1.5 text-[13px] font-medium transition-colors ring-focus',
              active ? 'text-text' : 'text-text-muted hover:text-text',
            )}
          >
            {active && (
              <motion.span
                layoutId={reduce ? undefined : `tab-${layoutId}`}
                className="absolute inset-0 rounded-[10px] bg-surface-2 shadow-[var(--shadow-raised)]"
                transition={{ type: 'spring', stiffness: 320, damping: 30 }}
              />
            )}
            <span className="relative z-10 inline-flex items-center gap-2">
              {x.icon}
              {x.label}
              {x.badge}
            </span>
          </button>
        )
      })}
    </div>
  )
}
