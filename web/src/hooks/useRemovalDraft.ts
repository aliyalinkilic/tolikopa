import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { diffDraft, itemKey, modeOf, policyToDraft, withMode } from '@/lib/remediation'
import type { RemovalDiff, RemovalDraft } from '@/lib/remediation'
import type { RemovalMode } from '@/lib/labels'
import { compareText } from '@/lib/format'
import type { RemediationPolicy } from '@/lib/types'

export interface RemovalDraftApi {
  /** null until the first server policy has arrived. */
  draft: RemovalDraft | null
  /** The saved policy as a draft — the baseline every diff is taken against. */
  base: RemovalDraft | null
  mode: RemovalMode
  diff: RemovalDiff | null
  dirty: boolean
  /** Another admin saved a new revision while this draft was open. */
  stale: boolean
  isSelected: (kind: 'app' | 'startup', itemId: string) => boolean
  toggle: (kind: 'app' | 'startup', itemId: string, next?: boolean) => void
  /** Bulk set for a list of ids of one kind (used by "safe defaults"). */
  setMany: (kind: 'app' | 'startup', itemIds: string[], selected: boolean) => void
  setMode: (mode: RemovalMode) => void
  setExcludedTags: (tags: string[]) => void
  /** Throw the local edits away and take the server's copy. */
  reset: () => void
  /** Adopt a just-saved policy as the new baseline without a refetch round-trip. */
  adopt: (policy: RemediationPolicy) => void
}

/**
 * Seed-once editing state for the removal policy.
 *
 * The important property: the draft is seeded from the FIRST policy the query
 * returns and never again. `useRemediationOverview` polls, and re-seeding on every
 * refetch (the shape `AlertRulesSection` uses — `useEffect(() => setRules(data))`)
 * silently throws away whatever the admin had ticked mid-edit. Instead a concurrent
 * change surfaces as `stale`, and the admin chooses.
 */
export function useRemovalDraft(policy: RemediationPolicy | undefined): RemovalDraftApi {
  const [draft, setDraft] = useState<RemovalDraft | null>(null)
  const seededRevision = useRef<string | null>(null)
  const seeded = useRef(false)

  useEffect(() => {
    if (!policy || seeded.current) return
    seeded.current = true
    seededRevision.current = policy.revision
    setDraft(policyToDraft(policy))
  }, [policy])

  const base = useMemo(() => (policy ? policyToDraft(policy) : null), [policy])

  const selected = useMemo(() => {
    const set = new Set<string>()
    if (!draft) return set
    for (const id of draft.apps) set.add(itemKey('app', id))
    for (const id of draft.startup) set.add(itemKey('startup', id))
    return set
  }, [draft])

  const isSelected = useCallback(
    (kind: 'app' | 'startup', itemId: string) => selected.has(itemKey(kind, itemId)),
    [selected],
  )

  const toggle = useCallback((kind: 'app' | 'startup', itemId: string, next?: boolean) => {
    setDraft((prev) => {
      if (!prev) return prev
      const list = kind === 'app' ? prev.apps : prev.startup
      const has = list.includes(itemId)
      const want = next ?? !has
      if (want === has) return prev
      const updated = want ? [...list, itemId].sort() : list.filter((v) => v !== itemId)
      return kind === 'app' ? { ...prev, apps: updated } : { ...prev, startup: updated }
    })
  }, [])

  const setMany = useCallback((kind: 'app' | 'startup', itemIds: string[], select: boolean) => {
    setDraft((prev) => {
      if (!prev) return prev
      const list = kind === 'app' ? prev.apps : prev.startup
      const set = new Set(list)
      for (const id of itemIds) {
        if (select) set.add(id)
        else set.delete(id)
      }
      const updated = [...set].sort()
      return kind === 'app' ? { ...prev, apps: updated } : { ...prev, startup: updated }
    })
  }, [])

  const setMode = useCallback((mode: RemovalMode) => {
    setDraft((prev) => (prev ? withMode(prev, mode) : prev))
  }, [])

  const setExcludedTags = useCallback((tags: string[]) => {
    setDraft((prev) =>
      prev ? { ...prev, excluded_tags: [...tags].sort(compareText) } : prev,
    )
  }, [])

  const reset = useCallback(() => {
    if (!policy) return
    seededRevision.current = policy.revision
    setDraft(policyToDraft(policy))
  }, [policy])

  const adopt = useCallback((next: RemediationPolicy) => {
    seeded.current = true
    seededRevision.current = next.revision
    setDraft(policyToDraft(next))
  }, [])

  const diff = useMemo(() => (base && draft ? diffDraft(base, draft) : null), [base, draft])

  return {
    draft,
    base,
    mode: draft ? modeOf(draft) : 'off',
    diff,
    dirty: diff?.changed ?? false,
    stale: !!policy && seededRevision.current !== null && policy.revision !== seededRevision.current,
    isSelected,
    toggle,
    setMany,
    setMode,
    setExcludedTags,
    reset,
    adopt,
  }
}
