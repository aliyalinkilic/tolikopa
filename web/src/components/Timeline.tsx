import { useMemo } from 'react'
import { CalendarClock } from 'lucide-react'
import { formatAxisTime, formatDayTime, formatSpan } from '@/lib/format'
import { EmptyState } from './EmptyState'
import { cn } from '@/lib/cn'
import type { SessionItem } from '@/lib/types'
import { useT } from '@/i18n'
import { chart } from '@/lib/theme'

interface TimelineProps {
  sessions: SessionItem[]
  from: string
  to: string
  groupBy?: 'hostname' | 'none'
  emptyLabel?: string
}

interface Band {
  key: string
  left: number
  width: number
  kind: string
  session: SessionItem
}

const KIND_COLOR: Record<string, string> = {
  active: chart.ok,
  idle: chart.warn,
  logged_on: chart.accent2,
}

function clampPct(n: number): number {
  return Math.max(0, Math.min(100, n))
}

export function Timeline({ sessions, from, to, groupBy = 'hostname', emptyLabel }: TimelineProps) {
  const t = useT()
  const winStart = new Date(from).getTime()
  const winEnd = new Date(to).getTime()
  const span = Math.max(1, winEnd - winStart)

  const rows = useMemo(() => {
    const map = new Map<string, { label: string; bands: Band[] }>()
    for (const s of sessions) {
      const groupKey = groupBy === 'hostname' ? s.machine_id : 'all'
      const label = groupBy === 'hostname' ? s.hostname || s.machine_id : t('ui.timeline.sessions')
      const startMs = new Date(s.started_at).getTime()
      const endMs = s.ended_at ? new Date(s.ended_at).getTime() : winEnd
      const left = clampPct(((startMs - winStart) / span) * 100)
      const right = clampPct(((endMs - winStart) / span) * 100)
      const width = Math.max(0.6, right - left)
      const band: Band = { key: `${s.machine_id}-${s.started_at}`, left, width, kind: s.kind, session: s }
      const entry = map.get(groupKey)
      if (entry) entry.bands.push(band)
      else map.set(groupKey, { label, bands: [band] })
    }
    return [...map.values()]
  }, [sessions, groupBy, winStart, winEnd, span])

  const ticks = useMemo(() => {
    const n = 6
    return Array.from({ length: n + 1 }, (_, i) => {
      const x = winStart + (span * i) / n
      return { left: (i / n) * 100, label: formatAxisTime(new Date(x), span) }
    })
  }, [winStart, span])

  if (rows.length === 0) {
    return <EmptyState icon={CalendarClock} title={emptyLabel ?? t('ui.timeline.empty')} />
  }

  return (
    <div>
      <div className="mb-3 flex items-center gap-4 text-[12px] text-text-muted">
        <span className="inline-flex items-center gap-1.5">
          <span className="h-2 w-3.5 rounded-full" style={{ backgroundColor: KIND_COLOR.active }} aria-hidden />
          {t('ui.timeline.active')}
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="h-2 w-3.5 rounded-full" style={{ backgroundColor: KIND_COLOR.logged_on }} aria-hidden />
          {t('ui.timeline.loggedOn')}
        </span>
      </div>

      <div className="space-y-2">
        {rows.map((row, ri) => (
          <div key={ri} className="flex items-center gap-3">
            <div className="w-28 shrink-0 truncate text-[12px] font-medium text-text-muted" title={row.label}>
              {row.label}
            </div>
            <div className="relative h-7 flex-1 overflow-hidden rounded-[8px] bg-bg-elev/60">
              {row.bands.map((b) => (
                <div
                  key={b.key}
                  className="absolute top-1/2 h-4 -translate-y-1/2 rounded-[5px]"
                  style={{
                    left: `${b.left}%`,
                    width: `${b.width}%`,
                    minWidth: 3,
                    backgroundColor: KIND_COLOR[b.kind] ?? chart.offline,
                    opacity: b.kind === 'active' ? 0.95 : 0.7,
                  }}
                  title={`${row.label} · ${formatDayTime(b.session.started_at)} → ${
                    b.session.ended_at ? formatDayTime(b.session.ended_at) : t('ui.timeline.ongoing')
                  } (${formatSpan(b.session.started_at, b.session.ended_at)})`}
                />
              ))}
            </div>
          </div>
        ))}
      </div>

      <div className="relative mt-2 ml-[124px] h-4">
        {ticks.map((tick, i) => (
          <span
            key={i}
            className={cn('tabular absolute -translate-x-1/2 text-[10px] text-text-dim', i === 0 && 'translate-x-0', i === ticks.length - 1 && '-translate-x-full')}
            style={{ left: `${tick.left}%` }}
          >
            {tick.label}
          </span>
        ))}
      </div>
    </div>
  )
}
