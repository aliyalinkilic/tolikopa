// Pure helpers for endpoint hygiene (the Optimization page: app removal and
// startup entries).
//
// Two jobs live here:
//   1. Normalizing the admin API payload. The backend for this feature ships in
//      parallel with the panel, so the response is read defensively: missing
//      posture fields degrade to "unknown" rather than to a confident zero, and a
//      flat `catalog` array is accepted as well as one grouped by kind.
//   2. Draft/diff arithmetic — what the admin changed relative to the saved
//      policy — so the save affordance and the confirm dialog agree on the facts.
import type {
  FleetMachine,
  RemediationAttemptDTO,
  RemediationCatalogItem,
  RemediationCatalogGroups,
  RemediationDrift,
  RemediationOverview,
  RemediationPolicy,
  RemediationPosture,
  RemediationRollup,
} from './types'
import { translate } from '@/i18n'
import { catalogName, catalogNotes } from '@/i18n/catalog'
import { compareText, lowerText } from './format'
import { REMOVAL_CATEGORY_ORDER, RISK_TIER_ORDER, removalCategoryKey } from './labels'
import type { RemovalMode } from './labels'

// ── Wire shapes (tolerant) ─────────────────────────────────────────────────────

type DriftWire = Partial<RemediationDrift>
type PostureWire = Partial<Omit<RemediationPosture, 'drift'>> & { drift?: DriftWire }

/** The roll-up the server actually sends (model.RemediationFleetSummary). */
interface FleetSummaryWire {
  machines?: number
  reporting?: number
  never_reported?: number
  catalog_stale?: number
  policy_current?: number
  kill_switch_off?: number
  /** Per-switch break-down (server migration 0020). */
  apps_not_allowed?: number
  startup_not_allowed?: number
  enforce_off?: number
  switches_unknown?: number
}

export interface RemediationOverviewWire {
  generated_at?: string
  catalog_version?: string
  /** Grouped by kind, or a flat list we split ourselves. */
  catalog?: Partial<RemediationCatalogGroups> | RemediationCatalogItem[]
  items?: RemediationCatalogItem[]
  /** The server's actual shape: two top-level arrays, one per kind. */
  apps?: RemediationCatalogItem[]
  startup?: RemediationCatalogItem[]
  /** revision arrives as a number here; the ingest wire carries it as a string. */
  policy?: Partial<Omit<RemediationPolicy, 'revision'>> & { revision?: string | number }
  posture?: PostureWire
  fleet?: FleetSummaryWire
}

const EMPTY_POLICY: RemediationPolicy = {
  revision: '',
  enabled: false,
  dry_run: true,
  excluded_tags: [],
  apps: [],
  startup: [],
}

function toGroups(wire: RemediationOverviewWire): RemediationCatalogGroups {
  // Preferred: the server's two top-level arrays.
  if (wire.apps || wire.startup) {
    return { app: wire.apps ?? [], startup: wire.startup ?? [] }
  }
  const flat = Array.isArray(wire.catalog) ? wire.catalog : wire.items
  if (flat) {
    return {
      app: flat.filter((i) => i.kind === 'app'),
      startup: flat.filter((i) => i.kind === 'startup'),
    }
  }
  const grouped = (wire.catalog ?? {}) as Partial<RemediationCatalogGroups>
  return { app: grouped.app ?? [], startup: grouped.startup ?? [] }
}

/** Fills the gaps the backend may not send yet, without inventing counts. */
export function normalizeOverview(wire: RemediationOverviewWire): RemediationOverview {
  const catalog = toGroups(wire)
  const posture = wire.posture ?? {}
  const drift = posture.drift ?? {}
  const fleet = wire.fleet ?? {}
  const { revision, ...restPolicy } = wire.policy ?? {}

  return {
    generated_at: wire.generated_at ?? '',
    catalog_version: wire.catalog_version ?? drift.server_version ?? '',
    catalog,
    policy: {
      ...EMPTY_POLICY,
      ...restPolicy,
      // The admin API types revision as a number; the agent wire uses a string
      // (tag-excluded machines get a "<n>-x" suffix). The panel speaks string.
      revision: revision === undefined || revision === null ? '' : String(revision),
      excluded_tags: restPolicy.excluded_tags ?? [],
      apps: restPolicy.apps ?? [],
      startup: restPolicy.startup ?? [],
    },
    posture: {
      reporting_machines: posture.reporting_machines ?? fleet.reporting ?? 0,
      targeted_machines: posture.targeted_machines ?? fleet.machines ?? 0,
      applied_machines: posture.applied_machines ?? fleet.policy_current ?? 0,
      kill_switch_off: posture.kill_switch_off ?? fleet.kill_switch_off ?? 0,
      // Per-kind counts arrive since server migration 0020; an older server sends
      // none, and 0 then means "not measured", not "nobody is blocked".
      apps_not_allowed: posture.apps_not_allowed ?? fleet.apps_not_allowed ?? 0,
      startup_not_allowed: posture.startup_not_allowed ?? fleet.startup_not_allowed ?? 0,
      enforce_off: posture.enforce_off ?? fleet.enforce_off ?? 0,
      switches_unknown: posture.switches_unknown ?? fleet.switches_unknown ?? 0,
      // Not in the settings payload — RemovalSection probes the attempts feed for
      // an observed dry run instead.
      dry_run_observed: posture.dry_run_observed ?? false,
      last_dry_run_at: posture.last_dry_run_at ?? null,
      last_attempt_at: posture.last_attempt_at ?? null,
      drift: {
        server_version: drift.server_version ?? wire.catalog_version ?? '',
        stale_machines: drift.stale_machines ?? fleet.catalog_stale ?? 0,
        versions: drift.versions,
      },
    },
  }
}

/** Catalog names/notes in the active language (see i18n/catalog.ts). */
export function localizeOverview(overview: RemediationOverview): RemediationOverview {
  const fix = (it: RemediationCatalogItem): RemediationCatalogItem => ({
    ...it,
    display_name: catalogName(it.item_id, it.display_name),
    notes: catalogNotes(it.item_id, it.notes) || it.notes,
  })
  return { ...overview, catalog: { app: overview.catalog.app.map(fix), startup: overview.catalog.startup.map(fix) } }
}

// ── Catalog helpers ────────────────────────────────────────────────────────────

export function allCatalogItems(catalog: RemediationCatalogGroups): RemediationCatalogItem[] {
  return [...catalog.app, ...catalog.startup]
}

export function itemKey(kind: string, itemId: string): string {
  return `${kind}:${itemId}`
}

export function indexCatalog(catalog: RemediationCatalogGroups): Map<string, RemediationCatalogItem> {
  const map = new Map<string, RemediationCatalogItem>()
  for (const it of allCatalogItems(catalog)) map.set(itemKey(it.kind, it.item_id), it)
  return map
}

const categoryRank = (category: string): number => {
  const idx = (REMOVAL_CATEGORY_ORDER as readonly string[]).indexOf(removalCategoryKey(category))
  return idx === -1 ? REMOVAL_CATEGORY_ORDER.length : idx
}

const riskRank = (tier: string): number => {
  const idx = (RISK_TIER_ORDER as readonly string[]).indexOf(tier)
  return idx === -1 ? RISK_TIER_ORDER.length : idx
}

export interface CatalogGroup {
  category: string
  items: RemediationCatalogItem[]
}

/** Groups by catalog category, riskiest item first inside each group. */
export function groupByCategory(items: RemediationCatalogItem[]): CatalogGroup[] {
  const buckets = new Map<string, RemediationCatalogItem[]>()
  for (const it of items) {
    // Catalog categories are Turkish words (data, not UI text): fold with Turkish
    // casing rules regardless of the panel language.
    const key = (it.category || '').toLocaleLowerCase('tr')
    const list = buckets.get(key)
    if (list) list.push(it)
    else buckets.set(key, [it])
  }
  return [...buckets.entries()]
    .map(([category, list]) => ({
      category,
      items: [...list].sort(
        (a, b) =>
          riskRank(a.risk_tier) - riskRank(b.risk_tier) ||
          compareText(a.display_name, b.display_name),
      ),
    }))
    .sort((a, b) => categoryRank(a.category) - categoryRank(b.category) || compareText(a.category, b.category))
}

/** Free-text match over the display name, the package family and process names. */
export function matchesSearch(item: RemediationCatalogItem, query: string): boolean {
  const q = lowerText(query.trim())
  if (!q) return true
  if (lowerText(item.display_name).includes(q)) return true
  if (lowerText(item.family).includes(q)) return true
  if (lowerText(item.item_id).includes(q)) return true
  if (lowerText(item.category || '').includes(q)) return true
  return (item.process_names ?? []).some((p) => lowerText(p).includes(q))
}

// ── Draft / diff ───────────────────────────────────────────────────────────────

export interface RemovalDraft {
  enabled: boolean
  dry_run: boolean
  excluded_tags: string[]
  apps: string[]
  startup: string[]
}

export function policyToDraft(p: RemediationPolicy): RemovalDraft {
  return {
    enabled: p.enabled,
    dry_run: p.dry_run,
    excluded_tags: [...(p.excluded_tags ?? [])].sort(compareText),
    apps: [...(p.apps ?? [])].sort(),
    startup: [...(p.startup ?? [])].sort(),
  }
}

export function modeOf(draft: Pick<RemovalDraft, 'enabled' | 'dry_run'>): RemovalMode {
  if (!draft.enabled) return 'off'
  return draft.dry_run ? 'dry_run' : 'enforce'
}

export function withMode(draft: RemovalDraft, mode: RemovalMode): RemovalDraft {
  switch (mode) {
    case 'off': return { ...draft, enabled: false }
    // Leaving "off" keeps whatever the dry-run flag was; the mode is explicit.
    case 'dry_run': return { ...draft, enabled: true, dry_run: true }
    case 'enforce': return { ...draft, enabled: true, dry_run: false }
  }
}

function sameList(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false
  const sa = [...a].sort()
  const sb = [...b].sort()
  return sa.every((v, i) => v === sb[i])
}

export interface RemovalDiff {
  added: { kind: 'app' | 'startup'; item_id: string }[]
  removed: { kind: 'app' | 'startup'; item_id: string }[]
  tagsAdded: string[]
  tagsRemoved: string[]
  modeFrom: RemovalMode
  modeTo: RemovalMode
  modeChanged: boolean
  selectionChanged: boolean
  changed: boolean
}

function listDiff(base: string[], next: string[]): { added: string[]; removed: string[] } {
  const b = new Set(base)
  const n = new Set(next)
  return {
    added: next.filter((v) => !b.has(v)),
    removed: base.filter((v) => !n.has(v)),
  }
}

export function diffDraft(base: RemovalDraft, next: RemovalDraft): RemovalDiff {
  const apps = listDiff(base.apps, next.apps)
  const startup = listDiff(base.startup, next.startup)
  const tags = listDiff(base.excluded_tags, next.excluded_tags)
  const modeFrom = modeOf(base)
  const modeTo = modeOf(next)

  const added = [
    ...apps.added.map((item_id) => ({ kind: 'app' as const, item_id })),
    ...startup.added.map((item_id) => ({ kind: 'startup' as const, item_id })),
  ]
  const removed = [
    ...apps.removed.map((item_id) => ({ kind: 'app' as const, item_id })),
    ...startup.removed.map((item_id) => ({ kind: 'startup' as const, item_id })),
  ]

  const selectionChanged =
    !sameList(base.apps, next.apps) ||
    !sameList(base.startup, next.startup) ||
    !sameList(base.excluded_tags, next.excluded_tags)
  const modeChanged = modeFrom !== modeTo

  return {
    added,
    removed,
    tagsAdded: tags.added,
    tagsRemoved: tags.removed,
    modeFrom,
    modeTo,
    modeChanged,
    selectionChanged,
    changed: selectionChanged || modeChanged,
  }
}

// ── Fleet-derived blast radius ─────────────────────────────────────────────────
// NOTE: there is no GET /api/admin/machine-tags, so the tag universe below is
// derived from /api/fleet. It therefore only knows tags that are currently set on
// at least one machine; a tag typed here that no machine carries is still valid
// policy input (the server stores it verbatim), it just excludes nobody today.

// Tags are user-entered and compared case-insensitively. Turkish casing rules are
// used as the one fixed fold (not the UI language) so the same two tags match or
// differ identically for every admin.
const norm = (x: string): string => x.trim().toLocaleLowerCase('tr')

export interface TagUsage {
  tag: string
  machines: number
}

export function tagUniverse(fleet: FleetMachine[] | undefined): TagUsage[] {
  const counts = new Map<string, { tag: string; machines: number }>()
  for (const m of fleet ?? []) {
    for (const raw of m.tags ?? []) {
      const key = norm(raw)
      if (!key) continue
      const hit = counts.get(key)
      if (hit) hit.machines++
      else counts.set(key, { tag: raw.trim(), machines: 1 })
    }
  }
  return [...counts.values()].sort((a, b) => compareText(a.tag, b.tag))
}

export function isExcluded(machine: FleetMachine, excludedTags: string[]): boolean {
  if (excludedTags.length === 0) return false
  const excluded = new Set(excludedTags.map(norm))
  return (machine.tags ?? []).some((tag) => excluded.has(norm(tag)))
}

export interface BlastRadius {
  /** Windows machines known to the fleet — Linux agents have no such catalog. */
  fleet: number
  excluded: number
  targeted: number
  /** Of the targeted machines, how many are online right now. */
  targetedOnline: number
}

export function blastRadius(fleet: FleetMachine[] | undefined, excludedTags: string[]): BlastRadius {
  const windows = (fleet ?? []).filter((m) => m.kind !== 'linux')
  let excluded = 0
  let targeted = 0
  let targetedOnline = 0
  for (const m of windows) {
    if (isExcluded(m, excludedTags)) {
      excluded++
      continue
    }
    targeted++
    if (m.online) targetedOnline++
  }
  return { fleet: windows.length, excluded, targeted, targetedOnline }
}

// ── Attempts ───────────────────────────────────────────────────────────────────

/** Per-item outcome roll-up, used when the server does not send one. */
export function rollupAttempts(attempts: RemediationAttemptDTO[]): RemediationRollup[] {
  const byItem = new Map<string, { entry: RemediationRollup; machines: Set<string> }>()
  for (const a of attempts) {
    const key = itemKey(a.kind, a.item_id)
    let hit = byItem.get(key)
    if (!hit) {
      hit = {
        entry: { kind: a.kind, item_id: a.item_id, machines: 0, outcomes: {}, last_ts: a.ts },
        machines: new Set<string>(),
      }
      byItem.set(key, hit)
    }
    hit.machines.add(a.machine_id)
    hit.entry.outcomes[a.outcome] = (hit.entry.outcomes[a.outcome] ?? 0) + 1
    if (!hit.entry.last_ts || a.ts > hit.entry.last_ts) hit.entry.last_ts = a.ts
  }
  return [...byItem.values()]
    .map(({ entry, machines }) => ({ ...entry, machines: machines.size }))
    .sort((a, b) => (b.last_ts ?? '').localeCompare(a.last_ts ?? ''))
}

/** True once any agent has actually reported a dry-run action. Gates enforce mode
 *  when the server does not (yet) send `posture.dry_run_observed`. */
export function hasDryRunEvidence(attempts: RemediationAttemptDTO[] | undefined): boolean {
  return (attempts ?? []).some((a) => a.outcome === 'dry_run')
}

/** Presence line. `null`/absent count must never render as a confident 0. */
export function presenceLabel(item: RemediationCatalogItem): string {
  const count = item.present_count
  if (count === null || count === undefined) return translate('removal.presence.unknown')
  if (item.kind === 'startup') {
    // A disabled entry is still present; "active on N" must count enabled ones.
    if (typeof item.enabled_count === 'number') {
      const n = item.enabled_count
      return n === 0 ? translate('removal.presence.startupNone') : translate('removal.presence.startupSome', { count: n })
    }
    return count === 0 ? translate('removal.presence.startupNone') : translate('removal.presence.startupSome', { count })
  }
  return count === 0 ? translate('removal.presence.appNone') : translate('removal.presence.appSome', { count })
}
