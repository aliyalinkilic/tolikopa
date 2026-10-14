import { useId } from 'react'

interface SparklineProps {
  data: number[]
  width?: number
  height?: number
  color?: string
  fill?: boolean
  strokeWidth?: number
  className?: string
}

/** Dependency-free trend sparkline (SVG). For KPI trends, not a full chart. */
export function Sparkline({
  data,
  width = 120,
  height = 34,
  color = 'var(--color-accent)',
  fill = true,
  strokeWidth = 2,
  className,
}: SparklineProps) {
  const id = useId()
  if (data.length < 2) {
    return <svg width={width} height={height} className={className} aria-hidden />
  }
  const min = Math.min(...data)
  const max = Math.max(...data)
  const span = max - min || 1
  const pad = strokeWidth
  const w = width - pad * 2
  const h = height - pad * 2
  const step = w / (data.length - 1)
  const pts = data.map((v, i) => {
    const x = pad + i * step
    const y = pad + h - ((v - min) / span) * h
    return [x, y] as const
  })
  const line = pts.map(([x, y], i) => `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`).join(' ')
  const area = `${line} L${pts[pts.length - 1][0].toFixed(1)},${(height - pad).toFixed(1)} L${pad},${(height - pad).toFixed(1)} Z`

  return (
    <svg
      width="100%"
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      className={className}
      aria-hidden
    >
      <defs>
        <linearGradient id={`spark-${id}`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.28" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      {fill && <path d={area} fill={`url(#spark-${id})`} />}
      <path
        d={line}
        fill="none"
        stroke={color}
        strokeWidth={strokeWidth}
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}
