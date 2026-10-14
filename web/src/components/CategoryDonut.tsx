import { useReducedMotion } from 'motion/react'
import { Cell, Pie, PieChart, ResponsiveContainer, Tooltip } from 'recharts'
import { PieChart as PieIcon } from 'lucide-react'
import { catMeta } from '@/lib/labels'
import { formatRatioPercent } from '@/lib/format'
import { EmptyState } from './EmptyState'
import type { CategoryShare } from '@/lib/types'
import { useT } from '@/i18n'
import { chart, useResolveColor } from '@/lib/theme'

interface CategoryDonutProps {
  data: CategoryShare[]
  height?: number
}

interface Slice {
  key: string
  label: string
  color: string
  share: number
  samples: number
}

interface DonutTooltipItem {
  payload?: Slice
}

function DonutTooltip({ active, payload }: { active?: boolean; payload?: DonutTooltipItem[] }) {
  const t = useT()
  if (!active || !payload || payload.length === 0) return null
  const slice = payload[0].payload
  if (!slice) return null
  return (
    <div className="glass rounded-[var(--radius-md)] border border-border-2 px-3 py-2 shadow-[var(--shadow-pop)]">
      <div className="flex items-center gap-2 text-[12px]">
        <span className="size-2 rounded-full" style={{ backgroundColor: slice.color }} aria-hidden />
        <span className="text-text">{slice.label}</span>
        <span className="tabular ml-3 font-semibold text-text">{formatRatioPercent(slice.share, 1)}</span>
      </div>
      <div className="mt-0.5 text-[11px] text-text-dim">{t('ui.chart.samples', { count: slice.samples, })}</div>
    </div>
  )
}

export function CategoryDonut({ data, height = 176 }: CategoryDonutProps) {
  const t = useT()
  const reduce = useReducedMotion()
  const resolve = useResolveColor()
  const slices: Slice[] = data
    .filter((d) => d.share > 0)
    .map((d) => {
      const meta = catMeta(d.category)
      return { key: meta.key, label: meta.label, color: resolve(meta.color), share: d.share, samples: d.samples }
    })
    .sort((a, b) => b.share - a.share)

  if (slices.length === 0) {
    return <EmptyState icon={PieIcon} title={t('ui.chart.noCategory')} />
  }

  const top = slices[0]

  return (
    <div className="flex flex-col items-center gap-5 sm:flex-row">
      <div className="relative shrink-0" style={{ width: height, height }}>
        <ResponsiveContainer width="100%" height="100%">
          <PieChart>
            <Pie
              data={slices}
              dataKey="share"
              nameKey="label"
              cx="50%"
              cy="50%"
              innerRadius="64%"
              outerRadius="98%"
              paddingAngle={2}
              stroke={resolve(chart.surface)}
              strokeWidth={2}
              isAnimationActive={!reduce}
              animationDuration={420}
            >
              {slices.map((s) => (
                <Cell key={s.key} fill={s.color} />
              ))}
            </Pie>
            <Tooltip content={<DonutTooltip />} />
          </PieChart>
        </ResponsiveContainer>
        <div className="pointer-events-none absolute inset-0 grid place-items-center">
          <div className="text-center">
            <div className="tabular text-[22px] font-bold leading-none text-text">
              {formatRatioPercent(top.share)}
            </div>
            <div className="mt-1 text-[11px] text-text-dim">{top.label}</div>
          </div>
        </div>
      </div>

      <ul className="w-full flex-1 space-y-1.5">
        {slices.map((s) => {
          const Icon = catMeta(s.key).icon
          return (
            <li key={s.key} className="flex items-center gap-2 text-[13px]">
              <span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: s.color }} aria-hidden />
              <Icon className="size-3.5 shrink-0 text-text-dim" aria-hidden />
              <span className="truncate text-text-muted">{s.label}</span>
              <span className="tabular ml-auto font-semibold text-text">{formatRatioPercent(s.share)}</span>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
