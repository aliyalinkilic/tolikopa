import { motion, useReducedMotion } from 'motion/react'
import { metricTone, toneColor } from '@/lib/theme'
import type { Thresholds } from '@/lib/theme'
import { cn } from '@/lib/cn'
import { formatPercent } from '@/lib/format'
import { chart, tint } from '@/lib/theme'

interface RingGaugeProps {
  /** 0..100 */
  value: number
  label: string
  thresholds?: Thresholds
  size?: number
  stroke?: number
  displayValue?: string
  className?: string
}

/**
 * Circular gauge — track + animated progress arc. Arc color follows the metric's
 * threshold tone (ok/warn/crit); the metric label underneath keeps color from being
 * the only signal. strokeDashoffset transitions over --dur-slow.
 */
export function RingGauge({
  value,
  label,
  thresholds,
  size = 96,
  stroke = 8,
  displayValue,
  className,
}: RingGaugeProps) {
  const reduce = useReducedMotion()
  const clamped = Math.max(0, Math.min(100, isFinite(value) ? value : 0))
  const r = (size - stroke) / 2
  const c = 2 * Math.PI * r
  const offset = c * (1 - clamped / 100)
  const color = thresholds ? toneColor[metricTone(clamped, thresholds)] : chart.accent
  const center = size / 2

  return (
    <div className={cn('relative inline-grid place-items-center', className)} style={{ width: size, height: size }}>
      <svg width={size} height={size} className="-rotate-90" aria-hidden>
        <circle cx={center} cy={center} r={r} fill="none" stroke={chart.track} strokeWidth={stroke} />
        <motion.circle
          cx={center}
          cy={center}
          r={r}
          fill="none"
          stroke={color}
          strokeWidth={stroke}
          strokeLinecap="round"
          strokeDasharray={c}
          initial={{ strokeDashoffset: reduce ? offset : c }}
          animate={{ strokeDashoffset: offset }}
          transition={reduce ? { duration: 0 } : { duration: 0.38, ease: [0.22, 1, 0.36, 1] }}
          style={{ filter: `drop-shadow(0 0 6px ${tint(color, 27)})` }}
        />
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">
        <span className="tabular text-[15px] font-semibold leading-none text-text" style={{ color }}>
          {displayValue ?? formatPercent(clamped)}
        </span>
        <span className="mt-1 text-[10px] font-medium uppercase tracking-[0.12em] text-text-dim">{label}</span>
      </div>
    </div>
  )
}
