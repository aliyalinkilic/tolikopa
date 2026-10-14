// In-house i18n: two typed dictionaries, a tiny external store for the active
// language, and `t(key, params?)` with `{name}` interpolation + Intl.PluralRules.
//
// `translate` is callable from anywhere (lib/format.ts, lib/labels.ts, hooks that
// push toasts) and always reads the active language at call time. Components use
// `useT()` so they re-render when the language changes.
import { useSyncExternalStore } from 'react'
import { tr } from './tr'
import { en } from './en'
import { isPlural } from './plural'
import type { Leaf, Paths } from './plural'

export type Lang = 'tr' | 'en'
export const LANGS: readonly Lang[] = ['tr', 'en']
export const DEFAULT_LANG: Lang = 'tr'

/** Every valid translation key, e.g. `'nav.devices'`. */
export type TKey = Paths<typeof tr>
export type TParams = Record<string, string | number | null | undefined>
export type TFn = (key: TKey, params?: TParams) => string

const DICTS: Record<Lang, unknown> = { tr, en }

/** BCP 47 tags used for Intl. English uses en-GB: day-first dates, 24-hour clock. */
const LOCALES: Record<Lang, string> = { tr: 'tr-TR', en: 'en-GB' }

export function isLang(v: unknown): v is Lang {
  return v === 'tr' || v === 'en'
}

// ── Store ──────────────────────────────────────────────────────────────────────
let current: Lang = isLang(document.documentElement.lang) ? document.documentElement.lang : DEFAULT_LANG
const listeners = new Set<() => void>()

export function getLang(): Lang {
  return current
}

export function getLocale(): string {
  return LOCALES[current]
}

export function setLang(lang: Lang): void {
  document.documentElement.lang = lang
  if (lang === current) return
  current = lang
  listeners.forEach((l) => l())
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

// ── Lookup ─────────────────────────────────────────────────────────────────────
function lookup(lang: Lang, key: string): Leaf | undefined {
  let node: unknown = DICTS[lang]
  for (const part of key.split('.')) {
    if (typeof node !== 'object' || node === null || isPlural(node)) return undefined
    node = (node as Record<string, unknown>)[part]
  }
  return typeof node === 'string' || isPlural(node) ? node : undefined
}

const pluralRules: Partial<Record<Lang, Intl.PluralRules>> = {}

const numberFormats: Partial<Record<Lang, Intl.NumberFormat>> = {}

/** `{name}` → value. Numbers are grouped for the language ("1.234" / "1,234"). */
function interpolate(lang: Lang, template: string, params?: TParams): string {
  if (!params) return template
  return template.replace(/\{(\w+)\}/g, (_whole, name: string) => {
    const v = params[name]
    if (v === undefined || v === null) return ''
    if (typeof v === 'number') return (numberFormats[lang] ??= new Intl.NumberFormat(LOCALES[lang])).format(v)
    return String(v)
  })
}

function render(lang: Lang, key: string, params?: TParams): string | undefined {
  const leaf = lookup(lang, key) ?? (lang === DEFAULT_LANG ? undefined : lookup(DEFAULT_LANG, key))
  if (leaf === undefined) return undefined
  if (typeof leaf === 'string') return interpolate(lang, leaf, params)
  const count = Number(params?.count ?? 0)
  const rules = (pluralRules[lang] ??= new Intl.PluralRules(LOCALES[lang]))
  return interpolate(lang, rules.select(count) === 'one' ? leaf.one : leaf.other, params)
}

/** Raw template for a key in the active language (used by the rich renderer). */
export function template(key: TKey, count?: number): string {
  const leaf = lookup(current, key) ?? lookup(DEFAULT_LANG, key)
  if (leaf === undefined) return key
  if (typeof leaf === 'string') return leaf
  const rules = (pluralRules[current] ??= new Intl.PluralRules(LOCALES[current]))
  return rules.select(count ?? 0) === 'one' ? leaf.one : leaf.other
}

/** Translate in the active language. Safe outside React. */
export function translate(key: TKey, params?: TParams): string {
  return render(current, key, params) ?? key
}

/**
 * Translate a key built from a wire value (`rule.${id}`, `errors.${code}`).
 * Returns `fallback` — or undefined — when the dictionary has no such key, so an
 * unknown enum value degrades to the caller's choice instead of printing a key.
 */
export function translateDynamic(key: string, params?: TParams): string | undefined
export function translateDynamic(key: string, params: TParams | undefined, fallback: string): string
export function translateDynamic(key: string, params?: TParams, fallback?: string): string | undefined {
  return render(current, key, params) ?? fallback
}

// One bound function per language: its identity changes exactly when the language
// does, which makes `t` a correct dependency for useMemo/useCallback.
const BOUND: Record<Lang, TFn> = {
  tr: (key, params) => render('tr', key, params) ?? key,
  en: (key, params) => render('en', key, params) ?? key,
}

export function useLang(): Lang {
  return useSyncExternalStore(subscribe, getLang, getLang)
}

/** `const t = useT()` — subscribes the component to language changes. */
export function useT(): TFn {
  return BOUND[useLang()]
}

/** Native names, shown untranslated in the language picker. */
export const LANG_NAMES: Record<Lang, string> = { tr: 'Türkçe', en: 'English' }
