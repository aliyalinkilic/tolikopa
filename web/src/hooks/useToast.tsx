import { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import type { StatusToken } from '@/lib/labels'

export interface ToastItem {
  id: number
  title: string
  description?: string
  tone: StatusToken
  href?: string
}

interface ToastContextValue {
  toasts: ToastItem[]
  push: (toast: Omit<ToastItem, 'id'>) => number
  dismiss: (id: number) => void
}

const ToastContext = createContext<ToastContextValue | null>(null)
const AUTO_DISMISS_MS = 6500

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastItem[]>([])
  const nextId = useRef(1)

  const dismiss = useCallback((id: number) => {
    setToasts((list) => list.filter((x) => x.id !== id))
  }, [])

  const push = useCallback(
    (toast: Omit<ToastItem, 'id'>) => {
      const id = nextId.current++
      setToasts((list) => [...list.slice(-4), { ...toast, id }])
      window.setTimeout(() => dismiss(id), AUTO_DISMISS_MS)
      return id
    },
    [dismiss],
  )

  const value = useMemo(() => ({ toasts, push, dismiss }), [toasts, push, dismiss])
  return <ToastContext.Provider value={value}>{children}</ToastContext.Provider>
}

export function useToast(): ToastContextValue {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast must be used within ToastProvider')
  return ctx
}
