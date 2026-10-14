import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { adoptServerDefaultLang } from '@/lib/prefs'
import type { PublicSettings } from '@/lib/types'

/**
 * Org name, server default language and build version. Needs no session, so the
 * login screen can brand itself; a server that predates the endpoint simply
 * leaves this undefined and every caller falls back to the generic text.
 */
export function usePublicSettings() {
  return useQuery({
    queryKey: ['public-settings'],
    queryFn: () => api.request<PublicSettings>('GET', '/api/settings/public', { silent401: true }),
    staleTime: 5 * 60_000,
    retry: false,
  })
}

/** Mount once near the root: feeds the server default language into the prefs chain. */
export function usePublicSettingsSync(): void {
  const { data } = usePublicSettings()
  const lang = data?.default_lang
  useEffect(() => {
    adoptServerDefaultLang(lang)
  }, [lang])
}
