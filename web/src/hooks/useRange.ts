import { useCallback, useMemo, useState } from 'react'
import {
  CUSTOM_RANGE,
  DEFAULT_RANGE,
  getRange,
  rangeFromTo,
  resolutionForSpan,
  type RangeOption,
} from '@/lib/ranges'
import { translate } from '@/i18n'

export interface CustomWindow {
  from: string
  to: string
}

/** Range selector state + a memoized {from,to} window that recomputes on change. */
export function useRange(initial: string = DEFAULT_RANGE) {
  const [key, setKey] = useState(initial)
  const [custom, setCustomState] = useState<CustomWindow | null>(null)

  const isCustom = key === CUSTOM_RANGE && custom !== null

  /** Set a custom {from,to} (RFC3339 UTC) and switch the selector to "Custom". */
  const setCustom = useCallback((c: CustomWindow) => {
    setCustomState(c)
    setKey(CUSTOM_RANGE)
  }, [])

  const win = useMemo<CustomWindow>(() => {
    if (isCustom && custom) return { from: custom.from, to: custom.to }
    // eslint-disable-next-line react-hooks/exhaustive-deps
    return rangeFromTo(getRange(key).ms)
  }, [key, custom, isCustom])

  const range = useMemo<RangeOption>(() => {
    if (isCustom && custom) {
      const ms = Math.max(0, Date.parse(custom.to) - Date.parse(custom.from))
      return {
        key: CUSTOM_RANGE,
        ms,
        res: resolutionForSpan(ms),
        get label() {
          return translate('range.custom.label')
        },
        get short() {
          return translate('range.custom.short')
        },
      }
    }
    return getRange(key)
  }, [key, custom, isCustom])

  return { key, setKey, range, win, custom, setCustom }
}
