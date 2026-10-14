import { motion } from 'motion/react'
import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { CountUp } from './CountUp'
import { Sparkline } from './Sparkline'
import { fadeUp } from '@/lib/motion'
import { statusColor } from '@/lib/labels'
import type { StatusToken } from '@/lib/labels'
import { chart, tint } from '@/lib/theme'

type KpiTone = StatusToken | 'accent'

const toneColor: Record<KpiTone, string> = {
  accent: chart.accent,
  ok: statusColor.ok,
  warn: statusColor.warn,
  crit: statusColor.crit,
  offline: statusColor.offline,
  info: statusColor.info,
}

interface KpiCardProps {
  label: string
  value: number
  icon: LucideIcon
  format?: (n: number) => string
  tone?: KpiTone
  spark?: number[]
  footer?: ReactNode
}

export function KpiCard({ label, value, icon: Icon, format, tone = 'accent', spark, footer }: KpiCardProps) {
  const color = toneColor[tone]
  return (
    <motion.div variants={fadeUp} className="card relative overflow-hidden p-5">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-[11px] font-semibold uppercase tracking-[0.14em] text-text-dim">{label}</p>
          <CountUp
            value={value}
            format={format}
            className="tabular mt-2 block text-[30px] font-bold leading-none text-text"
          />
        </div>
        <div
          className="grid size-10 shrink-0 place-items-center rounded-[12px]"
          style={{ backgroundColor: tint(color, 12), color }}
        >
          <Icon className="size-5" aria-hidden />
        </div>
      </div>
      {spark && spark.length > 1 && (
        <div className="mt-4 -mb-1">
          <Sparkline data={spark} color={color} height={30} />
        </div>
      )}
      {footer && <div className="mt-3 text-[12px] text-text-muted">{footer}</div>}
    </motion.div>
  )
}
