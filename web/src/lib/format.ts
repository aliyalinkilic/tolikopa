// Locale-aware formatting. Wall-clock times are always rendered in Europe/Istanbul
// (the fleet's timezone — permanent UTC+3); the active UI language only picks the
// conventions: tr-TR or en-GB (day-first dates, 24-hour clock, decimal comma/point).
import { getLang, getLocale, translate } from '@/i18n'
import type { Lang } from '@/i18n'

const TZ = 'Europe/Istanbul'
const DASH = '—'

interface Formatters {
  full: Intl.DateTimeFormat
  date: Intl.DateTimeFormat
  time: Intl.DateTimeFormat
  short: Intl.DateTimeFormat
  dayTime: Intl.DateTimeFormat
  weekday: Intl.DateTimeFormat
  number: Intl.NumberFormat
  collator: Intl.Collator
}

const cache: Partial<Record<Lang, Formatters>> = {}
const fixedCache = new Map<string, Intl.NumberFormat>()

function fmt(): Formatters {
  const lang = getLang()
  const hit = cache[lang]
  if (hit) return hit
  const locale = getLocale()
  const clock = { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' } as const
  const made: Formatters = {
    full: new Intl.DateTimeFormat(locale, { timeZone: TZ, day: '2-digit', month: '2-digit', year: 'numeric', ...clock }),
    date: new Intl.DateTimeFormat(locale, { timeZone: TZ, day: '2-digit', month: '2-digit', year: 'numeric' }),
    time: new Intl.DateTimeFormat(locale, { timeZone: TZ, ...clock }),
    short: new Intl.DateTimeFormat(locale, { timeZone: TZ, day: '2-digit', month: 'short' }),
    dayTime: new Intl.DateTimeFormat(locale, { timeZone: TZ, day: '2-digit', month: '2-digit', ...clock }),
    weekday: new Intl.DateTimeFormat(locale, { timeZone: 'UTC', weekday: 'short' }),
    number: new Intl.NumberFormat(locale),
    collator: new Intl.Collator(locale, { sensitivity: 'base', numeric: true }),
  }
  cache[lang] = made
  return made
}

/** Fixed-fraction number in the active locale ("52,0" / "52.0"). */
function fixed(n: number, digits: number): string {
  const key = `${getLang()}:${digits}`
  let f = fixedCache.get(key)
  if (!f) {
    f = new Intl.NumberFormat(getLocale(), { minimumFractionDigits: digits, maximumFractionDigits: digits })
    fixedCache.set(key, f)
  }
  return f.format(n)
}

function toDate(v: string | number | Date): Date {
  return v instanceof Date ? v : new Date(v)
}

export function formatDateTime(v: string | number | Date): string {
  const d = toDate(v)
  return isNaN(d.getTime()) ? DASH : fmt().full.format(d)
}

export function formatDate(v: string | number | Date): string {
  const d = toDate(v)
  return isNaN(d.getTime()) ? DASH : fmt().date.format(d)
}

export function formatTime(v: string | number | Date): string {
  const d = toDate(v)
  return isNaN(d.getTime()) ? DASH : fmt().time.format(d)
}

export function formatShortDate(v: string | number | Date): string {
  const d = toDate(v)
  return isNaN(d.getTime()) ? DASH : fmt().short.format(d)
}

export function formatDayTime(v: string | number | Date): string {
  const d = toDate(v)
  return isNaN(d.getTime()) ? DASH : fmt().dayTime.format(d)
}

/** Axis-friendly label that adapts to the span of a time series. */
export function formatAxisTime(v: string | number | Date, spanMs: number): string {
  const d = toDate(v)
  if (isNaN(d.getTime())) return ''
  if (spanMs <= 6 * 3600_000) return fmt().time.format(d)
  if (spanMs <= 3 * 86400_000) return fmt().dayTime.format(d)
  return fmt().short.format(d)
}

export function relativeTime(v: string | number | Date): string {
  const d = toDate(v)
  if (isNaN(d.getTime())) return DASH
  const diff = Date.now() - d.getTime()
  const s = Math.round(diff / 1000)
  if (s < 0) return translate('format.soon')
  if (s < 45) return translate('format.justNow')
  const m = Math.round(s / 60)
  if (m < 60) return translate('format.minutesAgo', { count: m })
  const h = Math.round(m / 60)
  if (h < 24) return translate('format.hoursAgo', { count: h })
  const days = Math.round(h / 24)
  if (days < 7) return translate('format.daysAgo', { count: days })
  return formatDate(d)
}

/** Short weekday names, Monday first (index 0 = Monday, matching the API). */
export function weekdayNames(): string[] {
  // 2024-01-01 was a Monday.
  return Array.from({ length: 7 }, (_, i) => fmt().weekday.format(new Date(Date.UTC(2024, 0, 1 + i))))
}

/** "09:00" — an hour-of-day label. */
export function formatHour(hour: number): string {
  return `${String(hour).padStart(2, '0')}:00`
}

/** Bytes with binary steps but familiar KB/MB/GB labels. */
export function formatBytes(bytes: number, digits = 1): string {
  if (!isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const val = bytes / Math.pow(1024, i)
  const d = i === 0 ? 0 : val >= 100 ? 0 : digits
  return `${fixed(val, d)} ${units[i]}`
}

export function formatBitsPerSec(bytesPerSec: number): string {
  return translate('format.perSecond', { value: formatBytes(bytesPerSec) })
}

/** milliwatt-hours → "52,0 Wh" / "52.0 Wh". */
export function formatWh(mwh: number): string {
  if (!isFinite(mwh) || mwh <= 0) return DASH
  return `${fixed(mwh / 1000, 1)} Wh`
}

/** value already on a 0..100 scale → "%42" (tr) / "42%" (en). */
export function formatPercent(value: number, digits = 0): string {
  if (!isFinite(value)) return DASH
  return translate('format.percent', { value: fixed(value, digits) })
}

/** ratio on a 0..1 scale → percent. */
export function formatRatioPercent(ratio: number, digits = 0): string {
  return formatPercent(ratio * 100, digits)
}

export function formatNumber(n: number): string {
  if (!isFinite(n)) return DASH
  return fmt().number.format(n)
}

/** Number with a fixed fraction length, in the active locale. */
export function formatFixed(n: number, digits: number): string {
  if (!isFinite(n)) return DASH
  return fixed(n, digits)
}

function unit(n: number, key: 'day' | 'hour' | 'minute' | 'second'): string {
  return translate(`format.unit.${key}`, { n })
}

export function formatDurationSec(seconds: number): string {
  if (!isFinite(seconds) || seconds < 0) return DASH
  const s = Math.floor(seconds)
  const days = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  // A zero second unit is dropped: "2 d", not "2 d 0 h".
  if (days > 0) return h > 0 ? `${unit(days, 'day')} ${unit(h, 'hour')}` : unit(days, 'day')
  if (h > 0) return m > 0 ? `${unit(h, 'hour')} ${unit(m, 'minute')}` : unit(h, 'hour')
  if (m > 0) return unit(m, 'minute')
  return unit(s, 'second')
}

/** Compact duration from seconds → "1 sa 30 dk" / "1 h 30 min"; zero → "0 dk".
 *  Used for per-app active-use totals. */
export function formatDuration(seconds: number): string {
  if (!isFinite(seconds) || seconds <= 0) return unit(0, 'minute')
  const s = Math.floor(seconds)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (h > 0) return m > 0 ? `${unit(h, 'hour')} ${unit(m, 'minute')}` : unit(h, 'hour')
  if (m > 0) return unit(m, 'minute')
  return unit(s, 'second')
}

/** Whole minutes → "2 sa 5 dk" / "2 h 5 min" (used for active-hours totals). */
export function formatMinutes(minutes: number): string {
  if (!isFinite(minutes) || minutes <= 0) return unit(0, 'minute')
  const total = Math.round(minutes)
  const h = Math.floor(total / 60)
  const m = total % 60
  if (h > 0 && m > 0) return `${unit(h, 'hour')} ${unit(m, 'minute')}`
  if (h > 0) return unit(h, 'hour')
  return unit(m, 'minute')
}

/** Span between two ISO instants, e.g. session length. `null` end = still open. */
export function formatSpan(startIso: string, endIso: string | null): string {
  const start = new Date(startIso).getTime()
  const end = endIso ? new Date(endIso).getTime() : Date.now()
  if (isNaN(start) || isNaN(end)) return DASH
  return formatDurationSec((end - start) / 1000)
}

// ── Text in the active locale ──────────────────────────────────────────────────
// Casing and collation are language-specific (Turkish dotted/dotless i): always go
// through these instead of hard-coding a locale at the call site.

export function compareText(a: string, b: string): number {
  return fmt().collator.compare(a, b)
}

export function lowerText(s: string): string {
  return s.toLocaleLowerCase(getLocale())
}

/** First letter, upper-cased for the active language — avatar initials. */
export function initialOf(s: string | null | undefined): string {
  const first = Array.from(s ?? '')[0]
  return first ? first.toLocaleUpperCase(getLocale()) : '?'
}

/** Case-insensitive equality for user-entered tags. Folds with BOTH casing rules so
 *  the same two tags match whichever language the panel is in (dotted/dotless i). */
export function sameText(a: string, b: string): boolean {
  return a.toLocaleLowerCase('tr') === b.toLocaleLowerCase('tr') || a.toLowerCase() === b.toLowerCase()
}

/** Split "CORP\ayse" into { domain, name }. */
export function splitLogonUser(logon: string): { domain: string | null; name: string } {
  if (!logon) return { domain: null, name: DASH }
  const idx = logon.indexOf('\\')
  if (idx === -1) return { domain: null, name: logon }
  return { domain: logon.slice(0, idx), name: logon.slice(idx + 1) }
}

export function displayUser(logon: string): string {
  return splitLogonUser(logon).name || logon || DASH
}
