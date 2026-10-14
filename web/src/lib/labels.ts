// Labels + visual meta for enumerated wire values. Text comes from the i18n
// dictionaries (resolved at call time, so it follows the active language); colours
// are theme tokens. Colour is never the only signal — every mapping pairs a colour
// with a label and (where shown) an icon.
import type { LucideIcon } from 'lucide-react'
import {
  Globe, FileText, Code2, Clapperboard, MessagesSquare, Cog, Boxes,
  MemoryStick, HardDrive, Cpu, HeartPulse, Bug, RotateCcw,
  Gamepad2, PackageOpen,
} from 'lucide-react'
import { getLocale, translate, translateDynamic } from '@/i18n'
import { formatFixed, formatNumber, formatPercent } from './format'

export type StatusToken = 'ok' | 'warn' | 'crit' | 'offline' | 'info'

/** Every tone a Badge can take (mirrors BadgeTone in components/ui/Badge.tsx). */
export type ToneToken = StatusToken | 'neutral' | 'accent'

export const statusColor: Record<StatusToken, string> = {
  ok: 'var(--color-ok)',
  warn: 'var(--color-warn)',
  crit: 'var(--color-crit)',
  offline: 'var(--color-offline)',
  info: 'var(--color-info)',
}

const DASH = '—'

/** "crash_storm" → "Crash Storm": a readable last resort for an id the dictionary
 *  does not know yet, so a new server-side enum value never shows up raw. */
function humanize(id: string): string {
  const locale = getLocale()
  return id
    .split(/[_\-\s]+/)
    .filter(Boolean)
    .map((w) => w.charAt(0).toLocaleUpperCase(locale) + w.slice(1))
    .join(' ')
}

function label(key: string, fallbackId: string): string {
  return translateDynamic(key) ?? (fallbackId ? humanize(fallbackId) : DASH)
}

// ── Program categories ─────────────────────────────────────────────────────────
export interface CategoryMeta {
  key: string
  label: string
  color: string
  icon: LucideIcon
}

export const CATEGORY_ORDER = ['browser', 'office', 'dev', 'media', 'comms', 'system', 'other'] as const

const CATEGORY_VISUAL: Record<string, { color: string; icon: LucideIcon }> = {
  browser: { color: 'var(--color-cat-browser)', icon: Globe },
  office: { color: 'var(--color-cat-office)', icon: FileText },
  dev: { color: 'var(--color-cat-dev)', icon: Code2 },
  media: { color: 'var(--color-cat-media)', icon: Clapperboard },
  comms: { color: 'var(--color-cat-comms)', icon: MessagesSquare },
  system: { color: 'var(--color-cat-system)', icon: Cog },
  other: { color: 'var(--color-cat-other)', icon: Boxes },
}

export function catMeta(category: string): CategoryMeta {
  const key = category || 'other'
  const visual = CATEGORY_VISUAL[key] ?? CATEGORY_VISUAL.other
  return { key, label: label(`category.${key}`, key), ...visual }
}

// ── Session state ──────────────────────────────────────────────────────────────
export interface SessionMeta {
  label: string
  token: StatusToken
}

const SESSION_TOKEN: Record<string, StatusToken> = {
  active: 'ok',
  // Logged on but no input for a while — amber-ish, between active (ok) and locked.
  idle: 'warn',
  locked: 'warn',
  disconnected: 'offline',
  none: 'offline',
}

export function sessionMeta(state: string): SessionMeta {
  return { label: label(`session.${state}`, state), token: SESSION_TOKEN[state] ?? 'offline' }
}

// ── Severity ───────────────────────────────────────────────────────────────────
export function severityToken(severity: string): StatusToken {
  const s = (severity || '').toLowerCase()
  if (s.startsWith('crit')) return 'crit'
  if (s.startsWith('warn')) return 'warn'
  if (s.startsWith('info')) return 'info'
  return 'warn'
}

export function severityLabel(severity: string): string {
  const x = severityToken(severity)
  return translate(x === 'crit' ? 'severity.crit' : x === 'warn' ? 'severity.warn' : 'severity.info')
}

// Order used for severity summaries / sorting (worst first).
export const SEVERITY_ORDER: StatusToken[] = ['crit', 'warn', 'info']

// ── Alert status ───────────────────────────────────────────────────────────────
export function alertStatusLabel(status: string): string {
  // The API has used both spellings for an acknowledged alert.
  return label(`alertStatus.${status === 'acked' ? 'ack' : status}`, status)
}

// ── Event kinds ────────────────────────────────────────────────────────────────
export function eventKindLabel(kind: string): string {
  return label(`eventKind.${kind}`, kind)
}

// ── Alert rules ────────────────────────────────────────────────────────────────
export function ruleLabel(rule: string): string {
  return label(`rule.${rule}`, rule)
}

/** One-line explanation of what a rule's thresholds measure, when we know it. */
export function ruleHelp(rule: string): string | undefined {
  return translateDynamic(`ruleHelp.${rule}`)
}

// ── Slowness reasons ───────────────────────────────────────────────────────────
export interface SlowReasonMeta {
  label: string
  icon: LucideIcon
  /** Renders the measured value as a phrase; `value` units differ per key. */
  describe: (value: number) => string
}

const SLOW_REASON: Record<string, { icon: LucideIcon; value: (v: number) => string }> = {
  mem_pressure: { icon: MemoryStick, value: (v) => formatPercent(Math.round(v * 100)) },
  low_ram: { icon: MemoryStick, value: (v) => formatFixed(v, v < 10 ? 1 : 0) },
  disk_full: { icon: HardDrive, value: (v) => formatPercent(Math.round(v)) },
  weak_cpu: { icon: Cpu, value: (v) => formatNumber(Math.round(v)) },
  smart: { icon: HeartPulse, value: () => '' },
  crashes: { icon: Bug, value: (v) => formatNumber(Math.round(v)) },
  long_uptime: { icon: RotateCcw, value: (v) => formatNumber(Math.round(v)) },
}

export function slowReasonMeta(key: string): SlowReasonMeta {
  const known = SLOW_REASON[key]
  if (!known) return { label: key ? humanize(key) : DASH, icon: Boxes, describe: () => '' }
  return {
    label: label(`slowness.reason.${key}.label`, key),
    icon: known.icon,
    describe: (v) =>
      translateDynamic(`slowness.reason.${key}.describe`, { value: known.value(v), count: Math.round(v) }) ?? '',
  }
}

/** Slowness score → status token. Mirrors the server thresholds in slow.go. */
export function slowSeverityToken(severity: string): StatusToken {
  switch (severity) {
    case 'crit': return 'crit'
    case 'warn': return 'warn'
    default: return 'ok'
  }
}

export function slowSeverityLabel(severity: string): string {
  switch (severity) {
    case 'crit': return translate('slowness.severity.crit')
    case 'warn': return translate('slowness.severity.warn')
    default: return translate('slowness.severity.ok')
  }
}

// ── Endpoint hygiene (app removal) ─────────────────────────────────────────────
// The wire values come from the agent's compiled catalog; every one of them gets
// a label here so no component ever inlines a switch.

/** Removal policy modes as the panel presents them. */
export type RemovalMode = 'off' | 'dry_run' | 'enforce'

export interface RemovalModeMeta {
  label: string
  token: ToneToken
  /** One-line explanation of what the mode actually does on the endpoints. */
  description: string
}

const REMOVAL_MODE_TOKEN: Record<RemovalMode, ToneToken> = { off: 'offline', dry_run: 'info', enforce: 'crit' }

export function removalModeMeta(mode: RemovalMode): RemovalModeMeta {
  return {
    label: translate(`removal.mode.${mode}.label`),
    token: REMOVAL_MODE_TOKEN[mode],
    description: translate(`removal.mode.${mode}.description`),
  }
}

export const REMOVAL_MODE_ORDER: RemovalMode[] = ['off', 'dry_run', 'enforce']

export interface RiskTierMeta {
  label: string
  token: ToneToken
  description: string
}

const RISK_TIER_TOKEN: Record<string, ToneToken> = { safe: 'ok', review: 'warn', advanced: 'crit' }

export function riskTierMeta(tier: string): RiskTierMeta {
  return {
    label: label(`removal.risk.${tier}.label`, tier),
    token: RISK_TIER_TOKEN[tier] ?? 'neutral',
    description: translateDynamic(`removal.risk.${tier}.description`) ?? '',
  }
}

/** Worst risk first — used to sort catalog rows within a category. */
export const RISK_TIER_ORDER = ['advanced', 'review', 'safe'] as const

export function removalKindLabel(kind: string): string {
  return label(`removal.kind.${kind}`, kind)
}

/** Plural form used for segment headers / counts. */
export function removalKindPlural(kind: string): string {
  return label(`removal.kindPlural.${kind}`, kind)
}

export interface RemovalCategoryMeta {
  key: string
  label: string
  icon: LucideIcon
  color: string
}

/**
 * The catalog compiled into the agent labels its categories with Turkish words
 * (productivity, communication, …, with non-ASCII letters). They are data, not UI text, so they are folded to an
 * ASCII key here — Turkish casing rules on purpose, whatever the panel language —
 * and the visible label comes from the dictionary.
 */
export function removalCategoryKey(category: string): string {
  return (category || '')
    .toLocaleLowerCase('tr')
    .normalize('NFD')
    .replace(/\p{M}/gu, '') // combining marks left over from NFD
    .replace(/\u0131/g, 'i')
    .trim()
}

const REMOVAL_CATEGORY_VISUAL: Record<string, { icon: LucideIcon; color: string }> = {
  oyun: { icon: Gamepad2, color: 'var(--color-cat-media)' },
  medya: { icon: Clapperboard, color: 'var(--color-cat-dev)' },
  uretkenlik: { icon: FileText, color: 'var(--color-cat-office)' },
  iletisim: { icon: MessagesSquare, color: 'var(--color-cat-comms)' },
  sistem: { icon: Cog, color: 'var(--color-cat-system)' },
  oem: { icon: PackageOpen, color: 'var(--color-cat-browser)' },
}

export const REMOVAL_CATEGORY_ORDER = ['oem', 'oyun', 'medya', 'iletisim', 'uretkenlik', 'sistem'] as const

export function removalCategoryMeta(category: string): RemovalCategoryMeta {
  const key = removalCategoryKey(category)
  const visual = REMOVAL_CATEGORY_VISUAL[key] ?? { icon: Boxes, color: 'var(--color-cat-other)' }
  return {
    key: key || 'other',
    label: translateDynamic(`removal.category.${key || 'other'}`) ?? (category || translate('removal.category.other')),
    ...visual,
  }
}

/** Where an app package lives / gets removed from. */
export function removalScopeLabel(scope?: string): string {
  return scope ? (translateDynamic(`removal.scope.${scope}`) ?? '') : ''
}

export function removalActionLabel(action: string): string {
  return label(`removal.action.${action}`, action)
}

export interface RemovalOutcomeMeta {
  label: string
  token: ToneToken
  /** Why this outcome happened, in one sentence. */
  description: string
}

const REMOVAL_OUTCOME_TOKEN: Record<string, ToneToken> = {
  ok: 'ok',
  dry_run: 'info',
  not_present: 'neutral',
  not_allowed: 'warn',
  blocked: 'warn',
  failed: 'crit',
  timeout: 'crit',
  unknown_item: 'crit',
}

export function removalOutcomeMeta(outcome: string): RemovalOutcomeMeta {
  return {
    label: label(`removal.outcome.${outcome}.label`, outcome),
    token: REMOVAL_OUTCOME_TOKEN[outcome] ?? 'neutral',
    description: translateDynamic(`removal.outcome.${outcome}.description`) ?? '',
  }
}

export interface RemovalErrorMeta {
  label: string
  /** What the admin should do about it, in one or two sentences. */
  hint: string
  /** True when a later hygiene pass usually clears it without intervention. */
  transient: boolean
}

// HRESULTs the Appx deployment API and the registry path actually return, plus
// Tolikopa's own 0xE000000x codes (agent/internal/actuator/actuator.go). Codes only —
// the agent never sends an OS message, so the dictionaries are where the meaning
// lives; this table only records whether a code tends to clear by itself.
const REMOVAL_ERROR_TRANSIENT: Record<string, boolean> = {
  '0X80073D02': true,
  '0X80073D19': true,
  // ERROR_REMOVE_FAILED — generic. "Protected system app" is only one cause.
  '0X80073CFA': false,
  '0X80070005': true,
  '0X80073CF1': true,
  '0X80073CF6': false,
  '0X80073CF9': false,
  '0X80073CFE': false,
  '0X800704C7': true,
  '0X80004002': false,
  '0X80040154': false,
  '0X80070002': true,
  '0XE0000001': false,
  '0XE0000002': false,
  '0XE0000003': false,
  '0XE0000004': true,
  '0XE0000005': true,
}

/** Meaning of a remediation error code, or null for an unknown/empty one. */
export function removalErrorMeta(code?: string | null): RemovalErrorMeta | null {
  if (!code) return null
  const key = code.trim().toUpperCase()
  const text = translateDynamic(`removal.error.${key}.label`)
  if (text === undefined || !(key in REMOVAL_ERROR_TRANSIENT)) return null
  return {
    label: text,
    hint: translateDynamic(`removal.error.${key}.hint`) ?? '',
    transient: REMOVAL_ERROR_TRANSIENT[key],
  }
}

/** Column/summary order for outcome roll-ups (best news first, failures last). */
export const REMOVAL_OUTCOME_ORDER = [
  'ok', 'dry_run', 'not_present', 'not_allowed', 'blocked', 'failed', 'timeout', 'unknown_item',
] as const

// ── Machine kind ───────────────────────────────────────────────────────────────
// Product names — the same in every language.
export function kindLabel(kind: string): string {
  switch (kind) {
    case 'windows': return 'Windows'
    case 'linux': return 'Linux'
    default: return kind || DASH
  }
}
