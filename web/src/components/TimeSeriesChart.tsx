import { useId, useMemo } from 'react'
import { useReducedMotion } from 'motion/react'
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { chart, useResolveColor } from '@/lib/theme'
import { formatAxisTime, formatBytes, formatDateTime, formatNumber, formatPercent } from '@/lib/format'
import { EmptyState } from './EmptyState'
import { LineChart as LineChartIcon } from 'lucide-react'
import { useT } from '@/i18n'

export interface SeriesDef {
  key: string
  name: string
  color: string
}

type Unit = 'percent' | 'bytes' | 'number'

export interface ChartRow {
  t: string
  [key: string]: number | string | null | undefined
}

interface TimeSeriesChartProps {
  data: ChartRow[]
  series: SeriesDef[]
  spanMs: number
  height?: number
  unit?: Unit
  yDomain?: [number, number]
  emptyLabel?: string
  /** Hide the built-in legend when the caller renders its own (e.g. stat tiles). */
  hideLegend?: boolean
}

function formatValue(v: number, unit: Unit): string {
  if (unit === 'percent') return formatPercent(v, 0)
  if (unit === 'bytes') return formatBytes(v)
  return formatNumber(v)
}

interface TooltipPayloadItem {
  color?: string
  name?: string
  value?: number
  dataKey?: string | number
}

function GlassTooltip({
  active,
  label,
  payload,
  unit,
}: {
  active?: boolean
  label?: string | number
  payload?: TooltipPayloadItem[]
  unit: Unit
}) {
  if (!active || !payload || payload.length === 0) return null
  return (
    <div className="glass rounded-[var(--radius-md)] border border-border-2 px-3 py-2 shadow-[var(--shadow-pop)]">
      <div className="mb-1 text-[11px] font-medium text-text-dim">{formatDateTime(String(label))}</div>
      <div className="space-y-1">
        {payload.map((p, i) => (
          <div key={i} className="flex items-center gap-2 text-[12px]">
            <span className="size-2 rounded-full" style={{ backgroundColor: p.color }} aria-hidden />
            <span className="text-text-muted">{p.name}</span>
            <span className="tabular ml-auto font-semibold text-text">
              {formatValue(typeof p.value === 'number' ? p.value : 0, unit)}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}

export function TimeSeriesChart({
  data,
  series,
  spanMs,
  height = 260,
  unit = 'percent',
  yDomain,
  emptyLabel,
  hideLegend = false,
}: TimeSeriesChartProps) {
  const t = useT()
  const id = useId()
  const reduce = useReducedMotion()
  // Recharts gets literals; `resolve` changes identity with the theme, so the
  // chart repaints when the user switches.
  const resolve = useResolveColor()
  // Keyed on content, not identity: callers pass a fresh `series` array each render,
  // and a new array here would make Recharts restart its draw-in animation.
  const seriesKey = series.map((s) => `${s.key}|${s.name}|${s.color}`).join(';')
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const drawn = useMemo(() => series.map((s) => ({ ...s, color: resolve(s.color) })), [seriesKey, resolve])
  const grid = resolve(chart.grid)
  const axisText = resolve(chart.axisText)
  const domain = yDomain ?? (unit === 'percent' ? ([0, 100] as [number, number]) : undefined)

  if (!data || data.length === 0) {
    return (
      <div style={{ height }} className="grid place-items-center">
        <EmptyState icon={LineChartIcon} title={emptyLabel ?? t('ui.chart.empty')} />
      </div>
    )
  }

  return (
    <div>
      {series.length >= 2 && !hideLegend && (
        <div className="mb-3 flex flex-wrap items-center gap-x-4 gap-y-1.5">
          {drawn.map((s) => (
            <span key={s.key} className="inline-flex items-center gap-1.5 text-[12px] text-text-muted">
              <span className="h-2 w-3.5 rounded-full" style={{ backgroundColor: s.color }} aria-hidden />
              {s.name}
            </span>
          ))}
        </div>
      )}
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={data} margin={{ top: 6, right: 8, bottom: 0, left: 0 }}>
          <defs>
            {drawn.map((s) => (
              <linearGradient key={s.key} id={`area-${id}-${s.key}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={s.color} stopOpacity={0.32} />
                <stop offset="92%" stopColor={s.color} stopOpacity={0} />
              </linearGradient>
            ))}
          </defs>
          <CartesianGrid stroke={grid} vertical={false} />
          <XAxis
            dataKey="t"
            tickFormatter={(v: string) => formatAxisTime(v, spanMs)}
            tick={{ fill: axisText, fontSize: 11 }}
            tickLine={false}
            axisLine={{ stroke: grid }}
            minTickGap={44}
            interval="preserveStartEnd"
          />
          <YAxis
            domain={domain}
            width={48}
            allowDecimals={unit !== 'number'}
            tick={{ fill: axisText, fontSize: 11 }}
            tickFormatter={(v: number) => formatValue(v, unit)}
            tickLine={false}
            axisLine={false}
          />
          <Tooltip
            cursor={{ stroke: resolve(chart.cursor), strokeWidth: 1 }}
            content={<GlassTooltip unit={unit} />}
          />
          {drawn.map((s) => (
            <Area
              key={s.key}
              type="monotone"
              dataKey={s.key}
              name={s.name}
              stroke={s.color}
              strokeWidth={2}
              fill={`url(#area-${id}-${s.key})`}
              fillOpacity={1}
              dot={false}
              activeDot={{ r: 3.5, strokeWidth: 0 }}
              connectNulls
              isAnimationActive={!reduce}
              animationDuration={380}
            />
          ))}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}
