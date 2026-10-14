// Appearance + language preferences.
//
// Resolution order (highest wins), for both theme and language:
//   1. the signed-in user's stored pref (GET /api/auth/me → prefs)
//   2. localStorage
//   3. language only: the server default (GET /api/settings/public → default_lang)
//   4. language only: the browser language
//   5. built-in default (system / tr)
//
// A change made in the UI applies instantly, is written to localStorage and — when
// a session exists — sent to PUT /api/me/prefs. index.html carries a tiny inline
// script that applies (2) before first paint; this module must stay in agreement
// with it (same storage keys, same attribute names).
import { useSyncExternalStore } from 'react'
import { DEFAULT_LANG, getLang, isLang, setLang } from '@/i18n'
import type { Lang } from '@/i18n'

export type ThemePref = 'dark' | 'light' | 'system'
export type ResolvedTheme = 'dark' | 'light'
export const THEME_PREFS: readonly ThemePref[] = ['dark', 'light', 'system']

const THEME_KEY = 'tolikopa-theme'
const LANG_KEY = 'tolikopa-lang'
// "system" is what the server reports for an account that never chose a theme, so
// it is the default here too — the login screen and the signed-in app then agree.
const DEFAULT_THEME: ThemePref = 'system'

/** Browser chrome colour per theme — mirrors --color-bg in index.css. */
const THEME_COLOR: Record<ResolvedTheme, string> = { dark: '#0a0e1a', light: '#f3f5fa' }

export function isThemePref(v: unknown): v is ThemePref {
  return v === 'dark' || v === 'light' || v === 'system'
}

function readStorage(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null // storage blocked (private mode / policy) — prefs just do not persist
  }
}

function writeStorage(key: string, value: string): void {
  try {
    localStorage.setItem(key, value)
  } catch {
    // see readStorage
  }
}

// ── Theme ──────────────────────────────────────────────────────────────────────
const media = window.matchMedia('(prefers-color-scheme: light)')

function resolve(pref: ThemePref): ResolvedTheme {
  if (pref === 'system') return media.matches ? 'light' : 'dark'
  return pref
}

const storedTheme = readStorage(THEME_KEY)
let themePref: ThemePref = isThemePref(storedTheme) ? storedTheme : DEFAULT_THEME
let resolvedTheme: ResolvedTheme = resolve(themePref)
const themeListeners = new Set<() => void>()

function applyTheme(): void {
  const next = resolve(themePref)
  const root = document.documentElement
  root.dataset.theme = next
  root.style.colorScheme = next
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', THEME_COLOR[next])
  document.querySelector('meta[name="color-scheme"]')?.setAttribute('content', next)
  resolvedTheme = next
  themeListeners.forEach((l) => l())
}

// "System" follows the OS live.
media.addEventListener('change', () => {
  if (themePref === 'system') applyTheme()
})

function subscribeTheme(listener: () => void): () => void {
  themeListeners.add(listener)
  return () => {
    themeListeners.delete(listener)
  }
}

export function getThemePref(): ThemePref {
  return themePref
}

export function useThemePref(): ThemePref {
  return useSyncExternalStore(subscribeTheme, getThemePref, getThemePref)
}

/** The theme actually on screen ('system' already resolved). */
export function useResolvedTheme(): ResolvedTheme {
  return useSyncExternalStore(subscribeTheme, () => resolvedTheme, () => resolvedTheme)
}

// ── Language ───────────────────────────────────────────────────────────────────
type LangSource = 'user' | 'local' | 'server' | 'browser' | 'default'

function browserLang(): Lang | null {
  for (const tag of navigator.languages ?? [navigator.language]) {
    const base = (tag || '').slice(0, 2).toLowerCase()
    if (isLang(base)) return base
  }
  return null
}

let langSource: LangSource = 'default'

function initLang(): void {
  const stored = readStorage(LANG_KEY)
  if (isLang(stored)) {
    langSource = 'local'
    setLang(stored)
    return
  }
  const fromBrowser = browserLang()
  if (fromBrowser) {
    langSource = 'browser'
    setLang(fromBrowser)
    return
  }
  setLang(DEFAULT_LANG)
}

// ── Server sync ────────────────────────────────────────────────────────────────
export interface PrefsPatch {
  theme?: ThemePref
  lang?: Lang
}

type Persist = (patch: PrefsPatch) => Promise<unknown>
let persist: Persist | null = null

/** Registered by AuthProvider while a session exists; null when signed out. */
export function setPrefsPersister(fn: Persist | null): void {
  persist = fn
  // Choices made before sign-in that the account does not have yet (see
  // adoptUserPrefs) are saved to it now that there is a session to save them to.
  if (fn && pendingForAccount) {
    const patch = pendingForAccount
    pendingForAccount = null
    push(patch)
  }
}

let pendingForAccount: PrefsPatch | null = null

function push(patch: PrefsPatch): void {
  // Best-effort: the choice is already applied and in localStorage; a failed PUT
  // only means another browser will not pick it up.
  // Only the changed key is sent: the server leaves an omitted key untouched.
  void persist?.(patch).catch(() => undefined)
}

/** User picked a theme in the UI. */
export function setThemePref(next: ThemePref): void {
  themePref = next
  writeStorage(THEME_KEY, next)
  applyTheme()
  push({ theme: next })
}

/** User picked a language in the UI. */
export function setLangPref(next: Lang): void {
  langSource = 'local'
  writeStorage(LANG_KEY, next)
  setLang(next)
  push({ lang: next })
}

/** Prefs stored on the account (from /api/auth/me). null fields = never chosen. */
export function adoptUserPrefs(prefs: { theme?: string | null; lang?: string | null } | null | undefined): void {
  if (!prefs) return
  const toAccount: PrefsPatch = {}

  if (isThemePref(prefs.theme)) {
    if (prefs.theme !== themePref) {
      themePref = prefs.theme
      writeStorage(THEME_KEY, themePref)
      applyTheme()
    }
  } else if (isThemePref(readStorage(THEME_KEY))) {
    // null = the account never chose. A theme picked in this browser before the
    // first sign-in is kept, and becomes the account's preference.
    toAccount.theme = themePref
  }

  if (isLang(prefs.lang)) {
    langSource = 'user'
    writeStorage(LANG_KEY, prefs.lang)
    setLang(prefs.lang)
  } else if (langSource === 'local') {
    toAccount.lang = getLang()
  }

  if (toAccount.theme || toAccount.lang) {
    if (persist) push(toAccount)
    else pendingForAccount = { ...pendingForAccount, ...toAccount }
  }
}

/** Server-wide default (from /api/settings/public). Only outranks the browser guess. */
export function adoptServerDefaultLang(lang: string | null | undefined): void {
  if (!isLang(lang)) return
  if (langSource === 'browser' || langSource === 'default') {
    langSource = 'server'
    setLang(lang)
  }
}

export { getLang }

// Apply once at module load (main.tsx imports this before rendering).
initLang()
applyTheme()
