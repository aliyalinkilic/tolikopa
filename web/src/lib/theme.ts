// JS-side colour tokens for charts, gauges and inline styles.
//
// index.css is the single source of truth: every value here is a `var(--…)`
// reference, so SVG attributes and inline styles follow the active theme with no
// re-render. Recharts wants literals for some props (it measures and interpolates),
// so chart components pass these through `useResolveColor()`, which reads the
// computed value and re-resolves whenever the theme changes.
import { useMemo } from 'react'
import { useResolvedTheme } from './prefs'

const v = (name: string): string => `var(--color-${name})`

export const chart = {
  cpu: v('metric-cpu'),
  mem: v('metric-mem'),
  netUp: v('metric-netup'),
  netDown: v('metric-netdown'),
  grid: v('chart-grid'),
  axis: v('chart-axis'),
  cursor: v('chart-cursor'),
  band: v('chart-band'),
  axisText: v('text-dim'),
  ok: v('ok'),
  warn: v('warn'),
  crit: v('crit'),
  offline: v('offline'),
  info: v('info'),
  accent: v('accent'),
  accent2: v('accent-2'),
  surface: v('surface'),
  track: v('track'),
  muted: v('cat-other'),
} as const

/** Sequential heatmap ramp, low → high. */
export const HEAT_RAMP = [v('heat-1'), v('heat-2'), v('heat-3'), v('heat-4'), v('heat-5'), v('heat-6')] as const
export const HEAT_EMPTY = v('heat-empty')

/** `color` at `percent`% opacity — the theme-safe replacement for `${hex}22`. */
export function tint(color: string, percent: number): string {
  return `color-mix(in srgb, ${color} ${percent}%, transparent)`
}

const VAR_RE = /^var\((--[\w-]+)\)$/

/**
 * Returns a resolver that turns `var(--token)` into the literal colour of the
 * theme currently on screen. Its identity changes with the theme, so anything
 * memoized on it recomputes.
 */
export function useResolveColor(): (color: string) => string {
  const theme = useResolvedTheme()
  return useMemo(() => {
    const style = getComputedStyle(document.documentElement)
    const cache = new Map<string, string>()
    return (color: string): string => {
      const m = VAR_RE.exec(color)
      if (!m) return color
      let hit = cache.get(m[1])
      if (hit === undefined) {
        hit = style.getPropertyValue(m[1]).trim() || color
        cache.set(m[1], hit)
      }
      return hit
    }
    // `theme` is the cache key: a new theme means new computed values.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [theme])
}

export interface Thresholds {
  warn: number
  crit: number
  /** true when a HIGHER value is worse (CPU, RAM, disk usage). */
  higherIsWorse?: boolean
}

export type Tone = 'ok' | 'warn' | 'crit'

/** Classify a 0..100 value against thresholds → status tone. */
export function metricTone(value: number, x: Thresholds): Tone {
  const higherIsWorse = x.higherIsWorse ?? true
  if (higherIsWorse) {
    if (value >= x.crit) return 'crit'
    if (value >= x.warn) return 'warn'
    return 'ok'
  }
  if (value <= x.crit) return 'crit'
  if (value <= x.warn) return 'warn'
  return 'ok'
}

export const toneColor: Record<Tone, string> = {
  ok: chart.ok,
  warn: chart.warn,
  crit: chart.crit,
}

// Default thresholds per metric (percent scale).
export const CPU_THRESHOLDS: Thresholds = { warn: 75, crit: 90, higherIsWorse: true }
export const RAM_THRESHOLDS: Thresholds = { warn: 80, crit: 92, higherIsWorse: true }
export const DISK_USED_THRESHOLDS: Thresholds = { warn: 80, crit: 92, higherIsWorse: true }
export const DISK_FREE_THRESHOLDS: Thresholds = { warn: 20, crit: 8, higherIsWorse: false }
// Battery charge (lower is worse): crit <20, warn <40, ok otherwise.
export const BATTERY_CHARGE_THRESHOLDS: Thresholds = { warn: 40, crit: 20, higherIsWorse: false }
// Battery health / wear (lower is worse): crit <60, warn <80, ok ≥80 — informs replacement.
export const BATTERY_HEALTH_THRESHOLDS: Thresholds = { warn: 80, crit: 60, higherIsWorse: false }
