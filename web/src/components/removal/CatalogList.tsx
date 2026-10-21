import { useMemo, useState } from 'react'
import { motion, useReducedMotion } from 'motion/react'
import { ListChecks, PackageSearch, Search, ShieldCheck, XCircle } from 'lucide-react'
import { Button } from '@/components/ui/Button'
import { Card, CardHeader } from '@/components/ui/Card'
import { Checkbox } from '@/components/ui/Checkbox'
import { Input } from '@/components/ui/Input'
import { EmptyState } from '@/components/EmptyState'
import { FilterChip } from '@/components/FilterChip'
import { CatalogItemRow } from './CatalogItemRow'
import { groupByCategory, matchesSearch } from '@/lib/remediation'
import { removalCategoryMeta, removalKindPlural, riskTierMeta } from '@/lib/labels'
import { fadeUp, staggerContainer } from '@/lib/motion'
import type { RemediationCatalogGroups, RemediationCatalogItem, RemediationKind } from '@/lib/types'
import { useT } from '@/i18n'

interface CatalogListProps {
  catalog: RemediationCatalogGroups
  isSelected: (kind: RemediationKind, itemId: string) => boolean
  onToggle: (kind: RemediationKind, itemId: string, next?: boolean) => void
  onSetMany: (kind: RemediationKind, itemIds: string[], selected: boolean) => void
  disabled?: boolean
  /** Lock the list to one kind (the Optimization page gives apps and startup
   *  entries their own tabs). Omitted = the kind switcher is shown. */
  kind?: RemediationKind
  title?: string
  description?: string
}

type RiskFilter = 'all' | 'safe' | 'review' | 'advanced'

const RISK_FILTERS: RiskFilter[] = ['all', 'safe', 'review', 'advanced']

export function CatalogList({
  catalog, isSelected, onToggle, onSetMany, disabled, kind: lockedKind, title, description,
}: CatalogListProps) {
  const t = useT()
  const [chosenKind, setKind] = useState<RemediationKind>('app')
  const kind = lockedKind ?? chosenKind
  const [query, setQuery] = useState('')
  const [risk, setRisk] = useState<RiskFilter>('all')
  const [onlySelected, setOnlySelected] = useState(false)
  const [onlyPresent, setOnlyPresent] = useState(false)
  const reduce = useReducedMotion()

  const items = kind === 'app' ? catalog.app : catalog.startup

  const selectedCount = (list: RemediationCatalogItem[], k: RemediationKind) =>
    list.reduce((n, it) => (isSelected(k, it.item_id) ? n + 1 : n), 0)

  const appSelected = selectedCount(catalog.app, 'app')
  const startupSelected = selectedCount(catalog.startup, 'startup')

  const visible = useMemo(
    () =>
      items.filter((it) => {
        if (!matchesSearch(it, query)) return false
        if (risk !== 'all' && it.risk_tier !== risk) return false
        if (onlySelected && !isSelected(kind, it.item_id)) return false
        if (onlyPresent && !(typeof it.present_count === 'number' && it.present_count > 0)) return false
        return true
      }),
    [items, query, risk, onlySelected, onlyPresent, isSelected, kind],
  )

  const groups = useMemo(() => groupByCategory(visible), [visible])

  const safeDefaults = useMemo(
    () => ({
      app: catalog.app.filter((i) => i.safe_default && !i.deprecated).map((i) => i.item_id),
      startup: catalog.startup.filter((i) => i.safe_default && !i.deprecated).map((i) => i.item_id),
    }),
    [catalog],
  )
  // A locked list only ever touches its own kind.
  const kinds: RemediationKind[] = lockedKind ? [lockedKind] : ['app', 'startup']
  const safeDefaultTotal = kinds.reduce((n, k) => n + safeDefaults[k].length, 0)
  const selectedTotal = kinds.reduce((n, k) => n + (k === 'app' ? appSelected : startupSelected), 0)

  const applySafeDefaults = () => {
    for (const k of kinds) onSetMany(k, safeDefaults[k], true)
  }

  const clearAll = () => {
    for (const k of kinds) onSetMany(k, catalog[k].map((i) => i.item_id), false)
  }

  return (
    <Card padded={false}>
      <CardHeader
        title={title ?? t('removal.catalog.title')}
        description={description ?? t('removal.catalog.body')}
        action={
          <div className="flex flex-wrap items-center justify-end gap-2">
            <Button
              size="sm"
              variant="secondary"
              disabled={disabled || safeDefaultTotal === 0}
              leftIcon={<ShieldCheck className="size-4" aria-hidden />}
              onClick={applySafeDefaults}
            >
              {t('removal.catalog.selectSafe', { count: safeDefaultTotal })}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={disabled || selectedTotal === 0}
              leftIcon={<XCircle className="size-4" aria-hidden />}
              onClick={clearAll}
            >
              {t('removal.catalog.clear')}
            </Button>
          </div>
        }
        className="p-5 pb-4"
      />

      <div className="space-y-3 border-b border-border px-5 pb-4">
        <div
          className="flex flex-wrap items-center gap-1.5"
          role="group"
          aria-label={t('removal.catalog.kindGroup')}
        >
          {!lockedKind && (['app', 'startup'] as RemediationKind[]).map((k) => (
            <FilterChip
              key={k}
              active={kind === k}
              onClick={() => setKind(k)}
              count={k === 'app' ? appSelected : startupSelected}
              title={t('removal.catalog.kindChipTitle', { kind: removalKindPlural(k) })}
            >
              {removalKindPlural(k)}
            </FilterChip>
          ))}
          <span className={lockedKind ? 'text-[12px] text-text-dim' : 'ml-1 text-[12px] text-text-dim'}>
            {kind === 'app'
              ? t('removal.catalog.appSummary', { total: catalog.app.length, selected: appSelected })
              : t('removal.catalog.startupSummary', { total: catalog.startup.length, selected: startupSelected })}
          </span>
        </div>

        <div className="flex flex-col gap-2.5 sm:flex-row sm:items-center">
          <div className="sm:max-w-xs">
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t('removal.catalog.searchPlaceholder')}
              aria-label={t('removal.catalog.searchLabel')}
              leftIcon={<Search className="size-4" aria-hidden />}
            />
          </div>
          <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label={t('removal.catalog.riskGroup')}>
            {RISK_FILTERS.map((r) => (
              <FilterChip key={r} active={risk === r} onClick={() => setRisk(r)}>
                {r === 'all' ? t('removal.catalog.allRisks') : riskTierMeta(r).label}
              </FilterChip>
            ))}
            <FilterChip active={onlySelected} onClick={() => setOnlySelected((v) => !v)}>
              {t('removal.catalog.onlySelected')}
            </FilterChip>
            <FilterChip
              active={onlyPresent}
              onClick={() => setOnlyPresent((v) => !v)}
              title={t('removal.catalog.onlyPresentTitle')}
            >
              {t('removal.catalog.onlyPresent')}
            </FilterChip>
          </div>
        </div>
      </div>

      <div className="p-5 pt-4">
        {groups.length === 0 ? (
          <EmptyState
            icon={PackageSearch}
            title={t('removal.catalog.noMatchTitle')}
            description={t('removal.catalog.noMatchBody')}
          />
        ) : (
          <motion.div
            variants={staggerContainer()}
            initial={reduce ? false : 'hidden'}
            animate="show"
            className="space-y-6"
          >
            {groups.map((group) => {
              const meta = removalCategoryMeta(group.category)
              const Icon = meta.icon
              const ids = group.items.map((i) => i.item_id)
              const chosen = ids.filter((id) => isSelected(kind, id)).length
              const all = chosen === ids.length && ids.length > 0

              return (
                // role=group: these checkboxes belong together under one name.
                <motion.section key={group.category} variants={fadeUp} role="group" aria-label={meta.label}>
                  <div className="mb-2 flex items-center justify-between gap-3 border-b border-border pb-2">
                    <div className="flex items-center gap-2">
                      <span className="size-2 rounded-full" style={{ backgroundColor: meta.color }} aria-hidden />
                      <Icon className="size-4 text-text-dim" aria-hidden />
                      <h4 className="text-[14px] font-semibold text-text">{meta.label}</h4>
                      <span className="tabular text-[12px] text-text-dim">
                        {chosen}/{ids.length}
                      </span>
                    </div>
                    <Checkbox
                      size="sm"
                      checked={all}
                      indeterminate={chosen > 0 && !all}
                      disabled={disabled}
                      onChange={(next) => onSetMany(kind, ids, next)}
                      ariaLabel={t('removal.catalog.selectAllIn', { category: meta.label })}
                      label={<span className="text-[12px] text-text-muted">{t('common.all')}</span>}
                    />
                  </div>

                  <ul className="space-y-1">
                    {group.items.map((item) => (
                      <CatalogItemRow
                        key={item.item_id}
                        item={item}
                        selected={isSelected(kind, item.item_id)}
                        disabled={disabled}
                        onToggle={(next) => onToggle(kind, item.item_id, next)}
                      />
                    ))}
                  </ul>
                </motion.section>
              )
            })}
          </motion.div>
        )}

        <p className="mt-5 flex items-start gap-2 text-[12px] text-text-dim">
          <ListChecks className="mt-px size-3.5 shrink-0" aria-hidden />
          <span>
            {t('removal.catalog.footnote')}
          </span>
        </p>
      </div>
    </Card>
  )
}
