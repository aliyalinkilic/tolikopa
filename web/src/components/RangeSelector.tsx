import { useEffect, useRef, useState } from 'react'
import { CalendarRange } from 'lucide-react'
import { CUSTOM_RANGE, RANGES } from '@/lib/ranges'
import type { CustomWindow } from '@/hooks/useRange'
import { Button } from '@/components/ui/Button'
import { cn } from '@/lib/cn'
import { useT } from '@/i18n'

interface RangeSelectorProps {
  value: string
  onChange: (key: string) => void
  /** Current custom window (RFC3339 UTC). Enables the "Custom" option when paired with onCustomChange. */
  custom?: CustomWindow | null
  /** Called with a validated {from,to} (RFC3339 UTC) when the user applies a custom range. */
  onCustomChange?: (win: CustomWindow) => void
  className?: string
}

/** RFC3339 UTC → value for <input type="datetime-local"> in the browser's local zone. */
function toLocalInput(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** <input type="datetime-local"> local value → RFC3339 UTC. */
function fromLocalInput(local: string): string {
  const d = new Date(local)
  return Number.isNaN(d.getTime()) ? '' : d.toISOString()
}

export function RangeSelector({ value, onChange, custom, onCustomChange, className }: RangeSelectorProps) {
  const t = useT()
  const customEnabled = typeof onCustomChange === 'function'
  const isCustom = value === CUSTOM_RANGE
  const [open, setOpen] = useState(false)
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)

  // Seed the inputs each time the panel opens: reuse the active custom window,
  // otherwise default to the last 24 hours.
  useEffect(() => {
    if (!open) return
    if (custom) {
      setFrom(toLocalInput(custom.from))
      setTo(toLocalInput(custom.to))
    } else {
      const now = new Date()
      setTo(toLocalInput(now.toISOString()))
      setFrom(toLocalInput(new Date(now.getTime() - 24 * 3600_000).toISOString()))
    }
  }, [open, custom])

  // Close the popover on outside click.
  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  const valid = from !== '' && to !== '' && new Date(from).getTime() < new Date(to).getTime()

  const apply = () => {
    if (!valid || !onCustomChange) return
    onCustomChange({ from: fromLocalInput(from), to: fromLocalInput(to) })
    setOpen(false)
  }

  return (
    <div ref={rootRef} className={cn('relative inline-flex', className)}>
      <div
        role="group"
        aria-label={t('ui.range.group')}
        className="inline-flex rounded-[var(--radius-md)] bg-bg-elev/70 p-1"
      >
        {RANGES.map((r) => {
          const active = r.key === value
          return (
            <button
              key={r.key}
              type="button"
              onClick={() => onChange(r.key)}
              aria-pressed={active}
              title={r.label}
              className={cn(
                'tabular rounded-[9px] px-2.5 py-1 text-[12px] font-semibold transition-colors ring-focus',
                active ? 'bg-surface-2 text-text shadow-[var(--shadow-raised)]' : 'text-text-muted hover:text-text',
              )}
            >
              {r.short}
            </button>
          )
        })}
        {customEnabled && (
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            aria-pressed={isCustom}
            aria-expanded={open}
            aria-haspopup="dialog"
            title={t('range.custom.label')}
            className={cn(
              'inline-flex items-center gap-1 rounded-[9px] px-2.5 py-1 text-[12px] font-semibold transition-colors ring-focus',
              isCustom ? 'bg-surface-2 text-text shadow-[var(--shadow-raised)]' : 'text-text-muted hover:text-text',
            )}
          >
            <CalendarRange className="size-3.5" aria-hidden />
            {t('range.custom.short')}
          </button>
        )}
      </div>

      {customEnabled && open && (
        <div
          role="dialog"
          aria-label={t('ui.range.customDialog')}
          className="absolute right-0 top-[calc(100%+8px)] z-30 w-72 rounded-[var(--radius-md)] border border-border-2 bg-bg-elev p-4 shadow-[var(--shadow-pop)]"
        >
          <div className="space-y-3">
            <label className="block">
              <span className="mb-1.5 block text-[13px] font-medium text-text-muted">{t('ui.range.from')}</span>
              <input
                type="datetime-local"
                value={from}
                max={to || undefined}
                onChange={(e) => setFrom(e.target.value)}
                className="h-10 w-full rounded-[var(--radius-md)] border border-border-2 bg-surface-2/60 px-3 text-sm text-text outline-none transition focus:border-accent/60 focus:ring-2 focus:ring-accent/25"
              />
            </label>
            <label className="block">
              <span className="mb-1.5 block text-[13px] font-medium text-text-muted">{t('ui.range.to')}</span>
              <input
                type="datetime-local"
                value={to}
                min={from || undefined}
                onChange={(e) => setTo(e.target.value)}
                className="h-10 w-full rounded-[var(--radius-md)] border border-border-2 bg-surface-2/60 px-3 text-sm text-text outline-none transition focus:border-accent/60 focus:ring-2 focus:ring-accent/25"
              />
            </label>
            {!valid && from !== '' && to !== '' && (
              <p className="text-[12px] text-crit">{t('ui.range.invalid')}</p>
            )}
            <div className="flex justify-end gap-2 pt-0.5">
              <Button size="sm" variant="ghost" onClick={() => setOpen(false)}>
                {t('common.cancel')}
              </Button>
              <Button size="sm" variant="primary" onClick={apply} disabled={!valid}>
                {t('common.apply')}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
