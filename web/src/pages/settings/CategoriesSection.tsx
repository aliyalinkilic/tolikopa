import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, FlaskConical, Pencil, Plus, RefreshCw, Save, Search, SlidersHorizontal, Tags, Trash2 } from 'lucide-react'
import { Card, CardHeader } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { Switch } from '@/components/ui/Switch'
import { Checkbox } from '@/components/ui/Checkbox'
import { Modal } from '@/components/ui/Modal'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { Badge } from '@/components/ui/Badge'
import { DataTable } from '@/components/DataTable'
import type { Column } from '@/components/DataTable'
import { EmptyState } from '@/components/EmptyState'
import { FilterChip } from '@/components/FilterChip'
import { SkeletonText } from '@/components/Skeleton'
import {
  useCategories, useCategoryTest, useCreateCategory, useDeleteCategory, useReclassify, useUnclassified,
  useUpdateCategory,
} from '@/hooks/queries'
import { useRange } from '@/hooks/useRange'
import { useToast } from '@/hooks/useToast'
import { CATEGORY_ORDER, catMeta } from '@/lib/labels'
import { formatRatioPercent, relativeTime } from '@/lib/format'
import type { CategoryRule, UnclassifiedProcess, UnclassifiedSort } from '@/lib/types'
import { translate, useT } from '@/i18n'
import { Trans } from '@/i18n/rich'
import type { TKey } from '@/i18n'
import { errorText } from '@/lib/errors'
import { cn } from '@/lib/cn'

// ── Rule matching (mirrors server/internal/ingest/categories.go) ─────────────────
// Used only for previews and hints in the panel; the server stays authoritative
// (the test box asks it directly).

/** Lower-case, strip ".exe" — what the stored process names look like. */
function normName(s: string): string {
  const v = s.trim().toLowerCase()
  return v.endsWith('.exe') ? v.slice(0, -4) : v
}

/** Go path.Match subset: `*`, `?`, `[...]` (with `^` or `!` negation). */
function globRegex(pattern: string): RegExp | null {
  let re = '^'
  for (let i = 0; i < pattern.length; i++) {
    const ch = pattern[i]
    if (ch === '*') re += '[^/]*'
    else if (ch === '?') re += '[^/]'
    else if (ch === '[') {
      const end = pattern.indexOf(']', i + 1)
      if (end < 0) return null
      let cls = pattern.slice(i + 1, end)
      if (cls.startsWith('^') || cls.startsWith('!')) cls = '^' + cls.slice(1)
      re += '[' + cls.replace(/\\/g, '\\\\') + ']'
      i = end
    } else re += ch.replace(/[.+^${}()|\\]/g, '\\$&')
  }
  try {
    return new RegExp(re + '$')
  } catch {
    return null
  }
}

function ruleMatches(type: string, value: string, name: string): boolean {
  const v = value.trim().toLowerCase()
  if (!v) return false
  if (type === 'exact') return name === v
  if (type === 'prefix') return name.startsWith(v)
  if (type === 'glob') return globRegex(v)?.test(name) ?? false
  return false
}

interface Overlap {
  other: CategoryRule
  /** The rule that wins for names both match. */
  winner: CategoryRule
}

/** Prefix rules of different categories where one prefix extends the other: a
 *  name matching both lands in whichever has the lower priority (then the longer
 *  value) — easy to get wrong by hand. Exact rules are deliberate exceptions and
 *  globs are not compared. */
function prefixOverlaps(rule: Pick<CategoryRule, 'id' | 'category' | 'match_type' | 'match_value' | 'priority'>, rules: CategoryRule[]): Overlap[] {
  if (rule.match_type !== 'prefix') return []
  const v = rule.match_value.trim().toLowerCase()
  if (!v) return []
  const out: Overlap[] = []
  for (const o of rules) {
    if (o.id === rule.id || !o.enabled || o.match_type !== 'prefix' || o.category === rule.category) continue
    if (!(v.startsWith(o.match_value) || o.match_value.startsWith(v))) continue
    const self = rule as CategoryRule
    const winner =
      o.priority !== rule.priority
        ? o.priority < rule.priority ? o : self
        : o.match_value.length > v.length ? o : self
    out.push({ other: o, winner })
  }
  return out
}

// ── Section ──────────────────────────────────────────────────────────────────────
const MATCH_TYPE_KEYS: Record<string, TKey> = {
  exact: 'settings.cat.matchExact',
  prefix: 'settings.cat.matchPrefix',
  glob: 'settings.cat.matchGlob',
}

// Built per render so the labels follow the active language.
const matchTypes = () => Object.entries(MATCH_TYPE_KEYS).map(([value, key]) => ({ value, label: translate(key) }))

type DraftRule = Omit<CategoryRule, 'id'> & { id?: number }
type RuleFilter = 'all' | 'unused' | 'custom' | 'disabled'

const RECLASSIFY_WINDOW = '7d'
const UNCLASSIFIED_PREVIEW = 12

function emptyDraft(): DraftRule {
  return { category: 'other', match_type: 'exact', match_value: '', priority: 100, enabled: true }
}

/** Only the writable fields — what create/update store. */
function writable(r: DraftRule): Omit<CategoryRule, 'id'> {
  return { category: r.category, match_type: r.match_type, match_value: r.match_value, priority: r.priority, enabled: r.enabled }
}

export function CategoriesSection() {
  const t = useT()
  const { data: rules, isLoading } = useCategories()
  const { win } = useRange(RECLASSIFY_WINDOW)
  const [unSort, setUnSort] = useState<UnclassifiedSort>('machines')
  const { data: unclassified } = useUnclassified(win, unSort)
  const createCat = useCreateCategory()
  const updateCat = useUpdateCategory()
  const deleteCat = useDeleteCategory()
  const reclassify = useReclassify()
  const { push } = useToast()

  const [draft, setDraft] = useState<DraftRule | null>(null)
  const [applyNow, setApplyNow] = useState(true)
  // Deleting used to fire straight from the icon button, with no confirmation.
  const [pendingDelete, setPendingDelete] = useState<CategoryRule | null>(null)
  const [query, setQuery] = useState('')
  const [catFilter, setCatFilter] = useState('')
  const [filter, setFilter] = useState<RuleFilter>('all')
  const [showAllUnclassified, setShowAllUnclassified] = useState(false)

  const allRules = useMemo(() => rules ?? [], [rules])
  const unNames = useMemo(() => (unclassified ?? []).map((u) => u.name), [unclassified])

  // Category options: the known set, plus any custom category an admin typed in
  // through the API so editing such a rule does not silently re-file it.
  const categoryOptions = useMemo(() => {
    const keys: string[] = [...CATEGORY_ORDER]
    for (const r of allRules) if (!keys.includes(r.category)) keys.push(r.category)
    return keys.map((c) => ({ value: c, label: catMeta(c).label }))
  }, [allRules])

  const counts = useMemo(
    () => ({
      all: allRules.length,
      unused: allRules.filter((r) => r.enabled && !r.samples).length,
      custom: allRules.filter((r) => !r.builtin).length,
      disabled: allRules.filter((r) => !r.enabled).length,
    }),
    [allRules],
  )

  const visibleRules = useMemo(() => {
    const q = query.trim().toLowerCase()
    return allRules.filter((r) => {
      if (catFilter && r.category !== catFilter) return false
      if (filter === 'unused' && (!r.enabled || (r.samples ?? 0) > 0)) return false
      if (filter === 'custom' && r.builtin) return false
      if (filter === 'disabled' && r.enabled) return false
      if (!q) return true
      // A search term that is a process name also finds the rules matching it.
      return r.match_value.includes(q) || catMeta(r.category).label.toLowerCase().includes(q) || ruleMatches(r.match_type, r.match_value, normName(q))
    })
  }, [allRules, query, catFilter, filter])

  const overlapsById = useMemo(() => {
    const m = new Map<number, Overlap[]>()
    for (const r of allRules) {
      if (!r.enabled) continue
      const o = prefixOverlaps(r, allRules)
      if (o.length) m.set(r.id, o)
    }
    return m
  }, [allRules])

  /** Apply rules to already-stored samples of these names (no-op when empty). */
  const applyTo = (names: string[], after?: (updated: number) => void) => {
    if (names.length === 0) return after?.(0)
    reclassify.mutate(
      { window: RECLASSIFY_WINDOW, names: names.slice(0, 200) },
      {
        onSuccess: (res) => after?.(res?.updated_rows ?? 0),
        onError: (err) => push({ title: t('settings.cat.reclassifyFailed'), description: errorText(err), tone: 'crit' }),
      },
    )
  }

  const draftMatches = useMemo(
    () => (draft ? unNames.filter((n) => ruleMatches(draft.match_type, draft.match_value, n)) : []),
    [draft, unNames],
  )

  const save = () => {
    if (!draft) return
    const names = applyNow ? draftMatches : []
    const done = () => {
      setDraft(null)
      applyTo(names, (updated) =>
        push({
          title: t('settings.cat.saved'),
          description: names.length ? t('settings.cat.reclassifyDone', { count: updated }) : undefined,
          tone: 'ok',
        }),
      )
    }
    // e.g. category_rule_conflict — say why instead of failing silently.
    const failed = (err: unknown) => push({ title: t('settings.cat.saveFailed'), description: errorText(err), tone: 'crit' })
    if (draft.id != null) updateCat.mutate({ ...writable(draft), id: draft.id }, { onSuccess: done, onError: failed })
    else createCat.mutate(writable(draft), { onSuccess: done, onError: failed })
  }

  const quickAssign = (u: UnclassifiedProcess, category: string) => {
    createCat.mutate(
      { category, match_type: 'exact', match_value: u.name, priority: 100, enabled: true },
      {
        onSuccess: () =>
          applyTo([u.name], (updated) =>
            push({
              title: t('settings.cat.assigned', { name: u.name, category: catMeta(category).label }),
              description: t('settings.cat.reclassifyDone', { count: updated }),
              tone: 'ok',
            }),
          ),
        onError: (err) => push({ title: t('settings.cat.saveFailed'), description: errorText(err), tone: 'crit' }),
      },
    )
  }

  const columns: Column<CategoryRule>[] = [
    {
      key: 'category',
      header: t('common.category'),
      sortable: true,
      sortValue: (r) => catMeta(r.category).label,
      render: (r) => {
        const meta = catMeta(r.category)
        const Icon = meta.icon
        return (
          <span className="inline-flex items-center gap-1.5 font-medium text-text">
            <span className="size-2 rounded-full" style={{ backgroundColor: meta.color }} aria-hidden />
            <Icon className="size-3.5 text-text-dim" aria-hidden />
            {meta.label}
          </span>
        )
      },
    },
    {
      key: 'match_type',
      header: t('settings.cat.match'),
      sortable: true,
      sortValue: (r) => r.match_type,
      render: (r) => <span className="text-text-muted">{MATCH_TYPE_KEYS[r.match_type] ? t(MATCH_TYPE_KEYS[r.match_type]) : r.match_type}</span>,
    },
    {
      key: 'match_value',
      header: t('settings.cat.value'),
      sortable: true,
      sortValue: (r) => r.match_value,
      render: (r) => {
        const overlaps = overlapsById.get(r.id)
        return (
          <span className="inline-flex flex-wrap items-center gap-1.5">
            <span className="font-mono text-[13px] text-text">{r.match_value}</span>
            {r.builtin && (
              <Badge tone="neutral" title={t('settings.cat.builtinHint')} className="px-1.5 py-0 text-[11px] leading-4">
                {t('settings.cat.builtin')}
              </Badge>
            )}
            {overlaps && (
              <span
                className="inline-flex items-center gap-1 text-[12px] text-warn"
                title={overlaps
                  .map((o) =>
                    t('settings.cat.overlapHint', {
                      other: o.other.match_value,
                      category: catMeta(o.other.category).label,
                      winner: o.winner.match_value,
                    }),
                  )
                  .join('\n')}
              >
                <AlertTriangle className="size-3.5" aria-hidden />
                {t('settings.cat.overlap')}
              </span>
            )}
          </span>
        )
      },
    },
    {
      key: 'priority',
      header: t('settings.cat.priority'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.priority,
      render: (r) => <span className="tabular text-text-muted">{r.priority}</span>,
    },
    {
      key: 'hits',
      header: t('settings.cat.hits'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.samples ?? 0,
      render: (r) =>
        r.samples ? (
          <span className="block leading-tight" title={r.last_seen ? t('settings.cat.lastSeen', { time: relativeTime(r.last_seen) }) : undefined}>
            <span className="tabular block text-text">{t('settings.cat.machinesCount', { count: r.machines ?? 0 })}</span>
            <span className="tabular block text-[11px] text-text-dim">{t('settings.cat.samplesCount', { count: r.samples })}</span>
          </span>
        ) : (
          <span className="text-[12px] text-text-dim" title={r.enabled ? t('settings.cat.neverMatchedHint') : undefined}>
            {r.enabled ? t('settings.cat.neverMatched') : '—'}
          </span>
        ),
    },
    {
      key: 'enabled',
      header: t('common.enabled'),
      align: 'center',
      render: (r) => (
        <div className="flex justify-center">
          <Switch
            checked={r.enabled}
            onChange={(v) => updateCat.mutate({ ...writable(r), id: r.id, enabled: v })}
            label={t('settings.cat.toggleRule', { category: catMeta(r.category).label })}
          />
        </div>
      ),
    },
    {
      key: 'actions',
      header: '',
      align: 'right',
      render: (r) => (
        <div className="flex justify-end gap-1">
          <button
            type="button"
            onClick={() => setDraft(r)}
            aria-label={t('common.edit')}
            className="grid size-8 place-items-center rounded-[10px] text-text-muted hover:bg-wash-2 hover:text-text ring-focus"
          >
            <Pencil className="size-4" aria-hidden />
          </button>
          <button
            type="button"
            onClick={() => setPendingDelete(r)}
            aria-label={t('settings.cat.deleteRule', { value: r.match_value })}
            className="grid size-8 place-items-center rounded-[10px] text-text-muted hover:bg-crit/15 hover:text-crit ring-focus"
          >
            <Trash2 className="size-4" aria-hidden />
          </button>
        </div>
      ),
    },
  ]

  const unList = unclassified ?? []
  const unShown = showAllUnclassified ? unList : unList.slice(0, UNCLASSIFIED_PREVIEW)
  const draftOverlaps = draft ? prefixOverlaps({ id: draft.id ?? -1, ...draft }, allRules) : []

  return (
    <div className="grid gap-4 xl:grid-cols-[1.6fr_1fr]">
      <Card padded={false}>
        <CardHeader
          title={t('settings.cat.title')}
          description={t('settings.cat.body')}
          action={
            <Button size="sm" variant="primary" leftIcon={<Plus className="size-4" aria-hidden />} onClick={() => setDraft(emptyDraft())}>
              {t('settings.cat.newRule')}
            </Button>
          }
          className="p-5"
        />
        <div className="space-y-3 px-5 pb-3">
          <div className="grid gap-3 sm:grid-cols-[1fr_14rem]">
            <Input
              aria-label={t('settings.cat.searchLabel')}
              placeholder={t('settings.cat.searchPlaceholder')}
              leftIcon={<Search className="size-4" aria-hidden />}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <Select
              aria-label={t('settings.cat.categoryFilter')}
              placeholder={t('settings.cat.allCategories')}
              options={categoryOptions}
              value={catFilter}
              onChange={(e) => setCatFilter(e.target.value)}
            />
          </div>
          <div className="flex flex-wrap gap-2" role="group" aria-label={t('settings.cat.filterLabel')}>
            {(['all', 'unused', 'custom', 'disabled'] as const).map((f) => (
              <FilterChip key={f} active={filter === f} onClick={() => setFilter(f)} count={counts[f]}>
                {t(`settings.cat.filter.${f}`)}
              </FilterChip>
            ))}
          </div>
        </div>
        <div className="px-5 pb-5">
          {isLoading ? (
            <SkeletonText lines={6} />
          ) : (
            <DataTable
              columns={columns}
              rows={visibleRules}
              getRowKey={(r) => String(r.id)}
              initialSort={{ key: 'hits', dir: 'desc' }}
              emptyLabel={allRules.length ? t('settings.cat.noMatchFilter') : t('settings.cat.empty')}
              className="border-0"
              pageSize={20}
            />
          )}
        </div>
      </Card>

      <div className="space-y-4">
        <RuleTestCard />

        <Card>
          <CardHeader
            title={t('settings.cat.unclassifiedTitle')}
            description={t('settings.cat.unclassifiedBody')}
            action={
              <Button
                size="sm"
                variant="secondary"
                leftIcon={<RefreshCw className="size-4" aria-hidden />}
                loading={reclassify.isPending}
                onClick={() =>
                  reclassify.mutate(RECLASSIFY_WINDOW, {
                    onSuccess: (res) => push({ title: t('settings.cat.reclassifyDone', { count: res?.updated_rows ?? 0 }), tone: 'ok' }),
                    onError: (err) => push({ title: t('settings.cat.reclassifyFailed'), description: errorText(err), tone: 'crit' }),
                  })
                }
              >
                {t('settings.cat.reclassify')}
              </Button>
            }
          />
          <div className="mt-3 flex flex-wrap items-center gap-2" role="group" aria-label={t('settings.cat.sortLabel')}>
            <SlidersHorizontal className="size-3.5 text-text-dim" aria-hidden />
            {(['machines', 'cpu', 'count'] as const).map((s) => (
              <FilterChip key={s} active={unSort === s} onClick={() => setUnSort(s)}>
                {t(`settings.cat.sort.${s}`)}
              </FilterChip>
            ))}
          </div>
          <div className="mt-3">
            {unList.length === 0 ? (
              <EmptyState icon={Tags} title={t('settings.cat.allClassified')} />
            ) : (
              <>
                <ul className="divide-y divide-border">
                  {unShown.map((u) => (
                    <li key={u.name} className="flex items-center justify-between gap-3 py-2">
                      <div className="min-w-0">
                        <span className="block truncate font-mono text-[13px] text-text" title={u.name}>
                          {u.name}
                        </span>
                        <span className="tabular block text-[11px] text-text-dim" title={t('settings.cat.lastSeen', { time: relativeTime(u.last_seen) })}>
                          {t('settings.cat.machinesCount', { count: u.machines })} · {t('settings.cat.cpuShare', { value: formatRatioPercent(u.cpu_share, u.cpu_share < 0.01 ? 2 : 1) })} ·{' '}
                          {t('settings.cat.samplesCount', { count: u.count })}
                        </span>
                      </div>
                      <div className="flex shrink-0 items-center gap-1">
                        <select
                          value=""
                          disabled={createCat.isPending}
                          onChange={(e) => e.target.value && quickAssign(u, e.target.value)}
                          aria-label={t('settings.cat.quickAssignFor', { name: u.name })}
                          className="h-8 max-w-[9.5rem] rounded-[8px] border border-border-2 bg-surface-2/60 px-2 text-[12px] text-text outline-none focus:border-accent/60 focus:ring-2 focus:ring-accent/25"
                        >
                          <option value="" className="bg-surface text-text">
                            {t('settings.cat.quickAssign')}
                          </option>
                          {categoryOptions.map((o) => (
                            <option key={o.value} value={o.value} className="bg-surface text-text">
                              {o.label}
                            </option>
                          ))}
                        </select>
                        <button
                          type="button"
                          onClick={() => {
                            setApplyNow(true)
                            setDraft({ ...emptyDraft(), match_value: u.name })
                          }}
                          aria-label={t('settings.cat.moreOptionsFor', { name: u.name })}
                          title={t('settings.cat.moreOptions')}
                          className="grid size-8 place-items-center rounded-[8px] text-accent hover:bg-accent/10 ring-focus"
                        >
                          <Plus className="size-4" aria-hidden />
                        </button>
                      </div>
                    </li>
                  ))}
                </ul>
                {unList.length > UNCLASSIFIED_PREVIEW && (
                  <button
                    type="button"
                    onClick={() => setShowAllUnclassified((v) => !v)}
                    className="mt-2 rounded-[8px] px-2 py-1 text-[12px] font-medium text-accent hover:bg-accent/10 ring-focus"
                  >
                    {showAllUnclassified ? t('settings.cat.showLess') : t('settings.cat.showAll', { count: unList.length })}
                  </button>
                )}
              </>
            )}
          </div>
        </Card>
      </div>

      <Modal
        open={draft !== null}
        onClose={() => setDraft(null)}
        title={draft?.id != null ? t('settings.cat.editTitle') : t('settings.cat.newTitle')}
        footer={
          <>
            <Button variant="ghost" onClick={() => setDraft(null)}>
              {t('common.cancel')}
            </Button>
            <Button
              variant="primary"
              leftIcon={<Save className="size-4" aria-hidden />}
              loading={createCat.isPending || updateCat.isPending}
              onClick={save}
              disabled={!draft?.match_value.trim()}
            >
              {t('common.save')}
            </Button>
          </>
        }
      >
        {draft && (
          <div className="space-y-4 pb-2">
            <Select
              label={t('common.category')}
              options={categoryOptions}
              value={draft.category}
              onChange={(e) => setDraft({ ...draft, category: e.target.value })}
            />
            <p className="-mt-2 text-[12px] text-text-dim">{t('settings.cat.overheadHint')}</p>
            <div className="grid grid-cols-2 gap-3">
              <Select
                label={t('settings.cat.matchType')}
                options={matchTypes()}
                value={draft.match_type}
                onChange={(e) => setDraft({ ...draft, match_type: e.target.value })}
              />
              <Input
                label={t('settings.cat.priority')}
                type="number"
                value={draft.priority}
                onChange={(e) => setDraft({ ...draft, priority: Number(e.target.value) || 0 })}
              />
            </div>
            <Input
              label={t('settings.cat.matchValue')}
              placeholder={t('settings.cat.matchValuePlaceholder')}
              value={draft.match_value}
              onChange={(e) => setDraft({ ...draft, match_value: e.target.value })}
              hint={t(draft.match_type === 'glob' ? 'settings.cat.matchValueHintGlob' : 'settings.cat.matchValueHint')}
            />
            <p className="text-[12px] text-text-dim">{t('settings.cat.priorityHint')}</p>

            {draft.match_value.trim() !== '' && (
              <div className="space-y-2 rounded-[var(--radius-md)] border border-border bg-wash/40 p-3 text-[12px]">
                {draft.match_type === 'exact' && <CurrentResolution name={draft.match_value} excludeId={draft.id} />}
                <p className="text-text-muted">
                  {draftMatches.length
                    ? t('settings.cat.previewMatches', { count: draftMatches.length })
                    : t('settings.cat.previewNone')}
                </p>
                {draftMatches.length > 0 && (
                  <p className="font-mono text-[12px] text-text">
                    {draftMatches.slice(0, 8).join(', ')}
                    {draftMatches.length > 8 ? ' …' : ''}
                  </p>
                )}
                {draftOverlaps.map((o) => (
                  <p key={o.other.id} className="flex items-start gap-1.5 text-warn">
                    <AlertTriangle className="mt-0.5 size-3.5 shrink-0" aria-hidden />
                    {t('settings.cat.overlapHint', {
                      other: o.other.match_value,
                      category: catMeta(o.other.category).label,
                      winner: o.winner.id === o.other.id ? o.other.match_value : draft.match_value.trim().toLowerCase(),
                    })}
                  </p>
                ))}
                {draftMatches.length > 0 && (
                  <Checkbox checked={applyNow} onChange={setApplyNow} label={t('settings.cat.applyNow')} size="sm" />
                )}
              </div>
            )}

            <label className="flex items-center gap-2 text-[13px] text-text-muted">
              <Switch checked={draft.enabled} onChange={(v) => setDraft({ ...draft, enabled: v })} label={t('common.enabled')} />
              {t('common.enabled')}
            </label>
          </div>
        )}
      </Modal>

      <ConfirmDialog
        open={pendingDelete !== null}
        onClose={() => setPendingDelete(null)}
        onConfirm={() => {
          if (!pendingDelete) return
          deleteCat.mutate(pendingDelete.id, {
            onSuccess: () => {
              setPendingDelete(null)
              push({ title: t('settings.cat.deleted'), tone: 'ok' })
            },
            onError: (err) => push({ title: t('settings.cat.deleteFailed'), description: errorText(err), tone: 'crit' }),
          })
        }}
        title={t('settings.cat.deleteTitle')}
        confirmLabel={t('common.delete')}
        loading={deleteCat.isPending}
        description={
          pendingDelete ? (
            <Trans
              k="settings.cat.deleteBody"
              values={{ value: pendingDelete.match_value, category: catMeta(pendingDelete.category).label }}
              tags={{ m: (c) => <span className="font-mono text-text">{c}</span> }}
            />
          ) : null
        }
      />
    </div>
  )
}

/** Debounced copy of a value, so typing does not fire a request per key. */
function useDebounced<T>(value: T, ms = 250): T {
  const [v, setV] = useState(value)
  useEffect(() => {
    const id = setTimeout(() => setV(value), ms)
    return () => clearTimeout(id)
  }, [value, ms])
  return v
}

/** In the rule modal: where an exact name resolves today (server answer). */
function CurrentResolution({ name, excludeId }: { name: string; excludeId?: number }) {
  const t = useT()
  const { data } = useCategoryTest(useDebounced(name))
  if (!data) return null
  const current = data.matches.find((m) => m.id !== excludeId)
  return (
    <p className="text-text-muted">
      {current
        ? t('settings.cat.currentlyIn', { category: catMeta(current.category).label, value: current.match_value })
        : t('settings.cat.currentlyNone')}
    </p>
  )
}

/** "Which category would chrome.exe get?" — asks the server's live cache. */
function RuleTestCard() {
  const t = useT()
  const [name, setName] = useState('')
  const debounced = useDebounced(name)
  const { data, isFetching } = useCategoryTest(debounced)
  const winner = data?.matches[0]
  const shadowed = data?.matches.slice(1) ?? []
  const meta = winner ? catMeta(winner.category) : null
  const Icon = meta?.icon

  return (
    <Card>
      <CardHeader title={t('settings.cat.testTitle')} description={t('settings.cat.testBody')} />
      <div className="mt-3 space-y-3">
        <Input
          aria-label={t('settings.cat.testLabel')}
          placeholder={t('settings.cat.testPlaceholder')}
          leftIcon={<FlaskConical className="size-4" aria-hidden />}
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        {debounced.trim() !== '' && data && (
          <div className={cn('space-y-2 text-[13px]', isFetching && 'opacity-70')} aria-live="polite">
            <p className="text-[12px] text-text-dim">
              <Trans k="settings.cat.testNormalized" values={{ name: data.name }} tags={{ m: (c) => <span className="font-mono text-text">{c}</span> }} />
            </p>
            {winner && meta && Icon ? (
              <div className="flex flex-wrap items-center gap-2">
                <span className="inline-flex items-center gap-1.5 font-medium text-text">
                  <span className="size-2 rounded-full" style={{ backgroundColor: meta.color }} aria-hidden />
                  <Icon className="size-3.5 text-text-dim" aria-hidden />
                  {meta.label}
                </span>
                <span className="text-text-dim">
                  {t('settings.cat.testVia', {
                    type: MATCH_TYPE_KEYS[winner.match_type] ? t(MATCH_TYPE_KEYS[winner.match_type]) : winner.match_type,
                    value: winner.match_value,
                  })}
                </span>
              </div>
            ) : (
              <p className="text-text-muted">{t('settings.cat.testNoMatch')}</p>
            )}
            {shadowed.length > 0 && (
              <div>
                <p className="text-[12px] font-medium text-text-muted">{t('settings.cat.testShadowed')}</p>
                <ul className="mt-1 space-y-0.5 text-[12px] text-text-dim">
                  {shadowed.map((m) => (
                    <li key={m.id}>
                      <span className="font-mono text-text-muted">{m.match_value}</span> → {catMeta(m.category).label}
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        )}
      </div>
    </Card>
  )
}
