import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { api, setCsrfToken, setEnrollmentHandler, setUnauthorizedHandler } from '@/lib/api'
import { adoptUserPrefs, setPrefsPersister } from '@/lib/prefs'
import { queryClient } from '@/lib/queryClient'
import type { LoginResponse, LoginResult, MeResponse } from '@/lib/types'

type AuthStatus = 'loading' | 'authed' | 'anon'

interface AuthContextValue {
  user: MeResponse | null
  status: AuthStatus
  /**
   * Resolves to `null` once signed in, or to the short-lived MFA token when the
   * account has 2FA and a code is still needed (no session exists yet).
   */
  login: (username: string, password: string) => Promise<string | null>
  /** Adopt a session the server just opened (POST /api/setup/admin answers with a login body). */
  startSession: (res: LoginResponse) => Promise<void>
  /** Second step: `code` is a 6-digit TOTP or a recovery code. */
  verifyMfa: (mfaToken: string, code: string) => Promise<void>
  logout: () => Promise<void>
  /** Re-read /api/auth/me (after a profile, 2FA or policy change). */
  refresh: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

function fetchMe(): Promise<MeResponse> {
  return api.request<MeResponse>('GET', '/api/auth/me', { silent401: true })
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<MeResponse | null>(null)
  const [status, setStatus] = useState<AuthStatus>('loading')

  const adopt = useCallback((me: MeResponse) => {
    if (me.csrf) setCsrfToken(me.csrf)
    adoptUserPrefs(me.prefs)
    setUser(me)
    setStatus('authed')
  }, [])

  const goAnon = useCallback(() => {
    setCsrfToken(null)
    setUser(null)
    setStatus('anon')
    queryClient.clear()
  }, [])

  const refresh = useCallback(async () => {
    try {
      adopt(await fetchMe())
    } catch {
      // A failed refresh keeps the current view; a real session loss arrives as a
      // 401 on the next request and is handled by the global handler below.
    }
  }, [adopt])

  // Global 401 handler: any authenticated request that 401s drops us to login.
  useEffect(() => {
    setUnauthorizedHandler(() => {
      setCsrfToken(null)
      setUser(null)
      setStatus((s) => (s === 'authed' ? 'anon' : s))
    })
    // The 2FA policy was turned on mid-session: /me now carries the flag that
    // routes this account to the enrolment screen.
    setEnrollmentHandler(() => void refresh())
    return () => {
      setUnauthorizedHandler(null)
      setEnrollmentHandler(null)
    }
  }, [refresh])

  // Preference changes are mirrored to the account only while a session exists.
  useEffect(() => {
    setPrefsPersister(status === 'authed' ? (patch) => api.put<void>('/api/me/prefs', patch) : null)
    return () => setPrefsPersister(null)
  }, [status])

  // Probe the existing session on load (401 here is expected → anon, no redirect).
  useEffect(() => {
    let alive = true
    fetchMe()
      .then((me) => {
        if (alive) adopt(me)
      })
      .catch(() => {
        if (alive) setStatus('anon')
      })
    return () => {
      alive = false
    }
  }, [adopt])

  const startSession = useCallback(
    async (res: LoginResponse) => {
      setCsrfToken(res.csrf)
      // The login body is minimal; /me carries prefs, 2FA state and display name.
      let me: MeResponse = {
        username: res.username,
        role: res.role,
        mfa_enrollment_required: res.mfa_enrollment_required,
      }
      try {
        me = { ...me, ...(await fetchMe()) }
      } catch {
        // keep the minimal identity — the session itself is established
      }
      adopt(me)
    },
    [adopt],
  )

  const login = useCallback(
    async (username: string, password: string) => {
      const res = await api.post<LoginResult>('/api/auth/login', { username, password })
      if ('mfa_required' in res && res.mfa_required) return res.mfa_token
      await startSession(res as LoginResponse)
      return null
    },
    [startSession],
  )

  const verifyMfa = useCallback(
    async (mfaToken: string, code: string) => {
      const res = await api.post<LoginResponse>('/api/auth/login/mfa', { mfa_token: mfaToken, code })
      await startSession(res)
    },
    [startSession],
  )

  const logout = useCallback(async () => {
    try {
      await api.post('/api/auth/logout')
    } catch {
      // best-effort: drop the session locally regardless
    }
    goAnon()
  }, [goAnon])

  const value = useMemo(
    () => ({ user, status, login, startSession, verifyMfa, logout, refresh }),
    [user, status, login, startSession, verifyMfa, logout, refresh],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}

export function useIsAdmin(): boolean {
  const { user } = useAuth()
  return user?.role === 'admin'
}
