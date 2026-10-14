// Single shared WebSocket to /ws (same-origin, cookie auth). Channel subscriptions
// are ref-counted; the socket reconnects with backoff and re-subscribes on open.
import { api, ApiError } from './api'
import type { WSEnvelope } from './types'

// The server closes with 1008 (policy violation) when the session behind the
// socket was revoked, expired or fell into forced 2FA enrolment. A refused
// upgrade is not visible to scripts at all (the socket just closes before
// opening), so both cases are treated the same way.
const CLOSE_POLICY = 1008

type MessageListener = (msg: WSEnvelope) => void
type StatusListener = (connected: boolean) => void

function wsUrl(): string {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${location.host}/ws`
}

class WSClient {
  private ws: WebSocket | null = null
  private listeners = new Set<MessageListener>()
  private statusListeners = new Set<StatusListener>()
  private channels = new Map<string, number>()
  private reconnectTimer: number | null = null
  private backoff = 1000
  private connected = false
  private probing = false

  private ensureOpen(): void {
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) return
    this.open()
  }

  private open(): void {
    try {
      this.ws = new WebSocket(wsUrl())
    } catch {
      this.scheduleReconnect()
      return
    }
    let opened = false
    this.ws.onopen = () => {
      opened = true
      this.backoff = 1000
      this.setConnected(true)
      this.sendSubscribe()
    }
    this.ws.onmessage = (ev) => {
      let msg: WSEnvelope
      try {
        msg = JSON.parse(ev.data as string) as WSEnvelope
      } catch {
        return
      }
      this.listeners.forEach((l) => l(msg))
    }
    this.ws.onclose = (ev) => {
      this.setConnected(false)
      this.ws = null
      if (ev.code === CLOSE_POLICY || !opened) this.probeSession()
      else this.scheduleReconnect()
    }
    this.ws.onerror = () => {
      this.ws?.close()
    }
  }

  /**
   * Asks the REST API whether the session is still good. A 401/403 there runs the
   * app's sign-out / enrolment handlers (see lib/api.ts), which unmount everything
   * that holds a subscription; only a session that checks out reconnects.
   */
  private probeSession(): void {
    if (this.probing) return
    this.probing = true
    api
      .get('/api/auth/me')
      .then(() => this.scheduleReconnect())
      .catch((err: unknown) => {
        // Network trouble or a 5xx says nothing about the session: keep trying.
        const lost =
          err instanceof ApiError &&
          (err.status === 401 || (err.status === 403 && err.code === 'mfa_enrollment_required'))
        if (!lost) this.scheduleReconnect()
      })
      .finally(() => {
        this.probing = false
      })
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimer !== null) return
    if (this.listeners.size === 0 && this.channels.size === 0) return
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null
      this.backoff = Math.min(Math.round(this.backoff * 1.7), 15000)
      this.open()
    }, this.backoff)
  }

  private sendSubscribe(): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return
    const subscribe = [...this.channels.keys()]
    this.ws.send(JSON.stringify({ subscribe }))
  }

  private setConnected(v: boolean): void {
    if (this.connected === v) return
    this.connected = v
    this.statusListeners.forEach((l) => l(v))
  }

  private maybeClose(): void {
    if (this.listeners.size > 0 || this.channels.size > 0) return
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    this.ws?.close()
    this.ws = null
  }

  /** Subscribe to server channels ("fleet", "alerts", "machine:<id>"). Returns unsubscribe. */
  subscribe(channels: string[]): () => void {
    for (const c of channels) this.channels.set(c, (this.channels.get(c) ?? 0) + 1)
    this.ensureOpen()
    this.sendSubscribe()
    return () => {
      for (const c of channels) {
        const n = (this.channels.get(c) ?? 1) - 1
        if (n <= 0) this.channels.delete(c)
        else this.channels.set(c, n)
      }
      this.sendSubscribe()
      this.maybeClose()
    }
  }

  /** Listen to all inbound messages. Returns an unsubscribe function. */
  on(listener: MessageListener): () => void {
    this.listeners.add(listener)
    this.ensureOpen()
    return () => {
      this.listeners.delete(listener)
      this.maybeClose()
    }
  }

  /** Observe connection status; the listener is invoked immediately with the current value. */
  onStatus(listener: StatusListener): () => void {
    this.statusListeners.add(listener)
    listener(this.connected)
    return () => {
      this.statusListeners.delete(listener)
    }
  }
}

export const wsClient = new WSClient()
