import { useMemo, useRef, useState } from 'react'
import type { HeatmapCell } from '@/lib/types'
import { formatHour, formatMinutes, weekdayNames } from '@/lib/format'
import { HEAT_EMPTY, HEAT_RAMP, useResolveColor } from '@/lib/theme'
import { useT } from '@/i18n'

const HOURS = Array.from({ length: 24 }, (_, h) => h)

function hexToRgb(hex: string): [number, number, number] {
  const h = hex.replace('#', '')
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)]
}

function mix(a: string, b: string, ratio: number): string {
  const [ar, ag, ab] = hexToRgb(a)
  const [br, bg, bb] = hexToRgb(b)
  const r = Math.round(ar + (br - ar) * ratio)
  const g = Math.round(ag + (bg - ag) * ratio)
  const bl = Math.round(ab + (bb - ab) * ratio)
  return `rgb(${r}, ${g}, ${bl})`
}

/** x in [0,1] → a step along the sequential ramp. */
function heatColor(stops: string[], x: number): string {
  const pos = Math.max(0, Math.min(1, x)) * (stops.length - 1)
  const i = Math.floor(pos)
  if (i >= stops.length - 1) return stops[stops.length - 1]
  return mix(stops[i], stops[i + 1], pos - i)
}

interface Tip {
  x: number
  y: number
  label: string
}

export function ActivityHeatmap({ data }: { data: HeatmapCell[] }) {
  const t = useT()
  const wrapRef = useRef<HTMLDivElement>(null)
  const [tip, setTip] = useState<Tip | null>(null)
  // Sequential single-hue ramp, magnitude only — never used for identity. The ramp
  // is a theme token (dark: recedes into the surface; light: pale → deep), resolved
  // to literals here because the steps between stops are interpolated in JS.
  const resolve = useResolveColor()
  const stops = useMemo(() => HEAT_RAMP.map(resolve), [resolve])
  const weekdays = weekdayNames()

  const { grid, max } = useMemo(() => {
    const g = Array.from({ length: 7 }, () => new Array<number>(24).fill(0))
    for (const c of data) {
      if (c.weekday >= 0 && c.weekday < 7 && c.hour >= 0 && c.hour < 24) {
        g[c.weekday][c.hour] += c.active_minutes
      }
    }
    let m = 0
    for (const row of g) for (const v of row) if (v > m) m = v
    return { grid: g, max: Math.max(m, 1) }
  }, [data])

  const cellFill = (v: number): string => (v <= 0 ? HEAT_EMPTY : heatColor(stops, 0.16 + 0.84 * (v / max)))
  const cellLabel = (r: number, h: number, v: number) => `${weekdays[r]} ${formatHour(h)} · ${formatMinutes(v)}`

  const showTip = (el: HTMLElement, r: number, h: number, v: number) => {
    const wrap = wrapRef.current
    if (!wrap) return
    const cell = el.getBoundingClientRect()
    const box = wrap.getBoundingClientRect()
    setTip({
      x: cell.left - box.left + cell.width / 2,
      y: cell.top - box.top,
      label: cellLabel(r, h, v),
    })
  }

  return (
    <div>
      <div ref={wrapRef} className="relative">
        <div className="overflow-x-auto pb-1">
        <table
          className="w-full border-separate min-w-[600px]"
          style={{ borderSpacing: '3px' }}
          onMouseLeave={() => setTip(null)}
        >
          <caption className="sr-only">{t('ui.heatmap.caption')}</caption>
          <colgroup>
            <col style={{ width: 34 }} />
            {HOURS.map((h) => (
              <col key={h} />
            ))}
          </colgroup>
          <thead>
            <tr>
              <th />
              {HOURS.map((h) => (
                <th
                  key={h}
                  scope="col"
                  aria-label={t('ui.heatmap.hour', { hour: h })}
                  className="tabular pb-1 text-[10px] font-medium text-text-dim"
                >
                  {h % 3 === 0 ? h : ''}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {weekdays.map((wd, r) => (
              <tr key={r}>
                <th
                  scope="row"
                  className="pr-1.5 text-right align-middle text-[11px] font-medium text-text-muted"
                >
                  {wd}
                </th>
                {HOURS.map((h) => {
                  const v = grid[r][h]
                  return (
                    <td
                      key={h}
                      title={cellLabel(r, h, v)}
                      onMouseEnter={(e) => showTip(e.currentTarget, r, h, v)}
                      onFocus={(e) => showTip(e.currentTarget, r, h, v)}
                      onBlur={() => setTip(null)}
                      tabIndex={-1}
                      className="h-[22px] rounded-[3px] ring-1 ring-inset ring-hairline transition-[filter] hover:brightness-110"
                      style={{ backgroundColor: cellFill(v) }}
                    />
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
        </div>

        {tip && (
          <div
            className="glass pointer-events-none absolute z-20 -translate-x-1/2 -translate-y-full rounded-[var(--radius-md)] border border-border-2 px-2.5 py-1.5 shadow-[var(--shadow-pop)]"
            style={{ left: Math.max(64, Math.min(tip.x, (wrapRef.current?.clientWidth ?? 0) - 64)), top: tip.y - 6 }}
            role="status"
          >
            <span className="tabular whitespace-nowrap text-[12px] font-medium text-text">{tip.label}</span>
          </div>
        )}
      </div>

      {/* Legend — magnitude scale, paired with the tooltip's exact minutes. */}
      <div className="mt-3 flex items-center gap-2 text-[11px] text-text-dim">
        <span className="inline-flex items-center gap-1.5">
          <span
            className="size-3 rounded-[3px] ring-1 ring-inset ring-hairline"
            style={{ backgroundColor: HEAT_EMPTY }}
            aria-hidden
          />
          {t('ui.heatmap.none')}
        </span>
        <span className="ml-auto">{t('ui.heatmap.low')}</span>
        <span className="flex items-center gap-[3px]" aria-hidden>
          {[0.16, 0.36, 0.56, 0.76, 1].map((step) => (
            <span key={step} className="size-3 rounded-[3px]" style={{ backgroundColor: heatColor(stops, step) }} />
          ))}
        </span>
        <span>{t('ui.heatmap.high')}</span>
      </div>
    </div>
  )
}
