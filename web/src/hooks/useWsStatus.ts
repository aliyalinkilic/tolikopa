import { useEffect, useState } from 'react'
import { wsClient } from '@/lib/ws'

/** Reflects the shared socket's connection state (for the TopBar live indicator). */
export function useWsStatus(): boolean {
  const [connected, setConnected] = useState(false)
  useEffect(() => wsClient.onStatus(setConnected), [])
  return connected
}
