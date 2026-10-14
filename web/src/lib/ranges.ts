import { translate } from '@/i18n'
import type { Resolution } from './types'

export interface RangeOption {
  key: string
  label: string
  short: string
  ms: number
  res: Resolution
}

type PresetKey = '1h' | '6h' | '24h' | '7d' | '30d'

// `label` / `short` are getters so they always read the active language.
function preset(key: PresetKey, ms: number, res: Resolution): RangeOption {
  return {
    key,
    ms,
    res,
    get label() {
      return translate(`range.${key}.label`)
    },
    get short() {
      return translate(`range.${key}.short`)
    },
  }
}

export const RANGES: RangeOption[] = [
  preset('1h', 3600_000, 'raw'),
  preset('6h', 6 * 3600_000, 'raw'),
  preset('24h', 24 * 3600_000, '5m'),
  preset('7d', 7 * 86400_000, '5m'),
  preset('30d', 30 * 86400_000, '1h'),
]

export const DEFAULT_RANGE = '24h'

/** Sentinel key for the user-defined custom window. */
export const CUSTOM_RANGE = 'custom'

export function getRange(key: string): RangeOption {
  return RANGES.find((r) => r.key === key) ?? RANGES[2]
}

export function rangeFromTo(ms: number): { from: string; to: string } {
  const now = Date.now()
  return { from: new Date(now - ms).toISOString(), to: new Date(now).toISOString() }
}

/** Pick a sensible aggregate resolution for an arbitrary span (mirrors the presets). */
export function resolutionForSpan(ms: number): Resolution {
  if (ms <= 6 * 3600_000) return 'raw'
  if (ms <= 7 * 86400_000) return '5m'
  return '1h'
}
