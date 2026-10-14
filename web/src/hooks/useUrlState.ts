import { useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'

/** Two-way bind a single query-string param to state (shareable filter URLs). */
export function useUrlState(key: string, defaultValue = ''): [string, (value: string) => void] {
  const [params, setParams] = useSearchParams()
  const value = params.get(key) ?? defaultValue
  const setValue = useCallback(
    (next: string) => {
      setParams(
        (prev) => {
          const sp = new URLSearchParams(prev)
          if (!next) sp.delete(key)
          else sp.set(key, next)
          return sp
        },
        { replace: true },
      )
    },
    [key, setParams],
  )
  return [value, setValue]
}
