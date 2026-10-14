import { useEffect, useState } from 'react'
import {
  Archive, Lock, PackageX, Pencil, Plus, RefreshCw, Save, Settings2, SlidersHorizontal, Tags, Trash2, UserPlus,
} from 'lucide-react'
import { PageHeader } from '@/components/AppShell'
import { Tabs } from '@/components/ui/Tabs'
import { Card, CardHeader } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { Switch } from '@/components/ui/Switch'
import { Modal } from '@/components/ui/Modal'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { Badge } from '@/components/ui/Badge'
import { DataTable } from '@/components/DataTable'
import type { Column } from '@/components/DataTable'
import { EmptyState } from '@/components/EmptyState'
import { SkeletonText } from '@/components/Skeleton'
import { MachinesSection } from '@/pages/settings/MachinesSection'
import { RemovalSection } from '@/pages/settings/RemovalSection'
import {
  useAlertRules, useCategories, useCreateCategory, useDeleteCategory,
  useReclassify, useRemediationOverview, useUnclassified, useUpdateAlertRules,
  useUpdateCategory,
} from '@/hooks/queries'
import { useRange } from '@/hooks/useRange'
import { useIsAdmin } from '@/hooks/useAuth'
import { useToast } from '@/hooks/useToast'
import { useUrlState } from '@/hooks/useUrlState'
import { CATEGORY_ORDER, catMeta, removalModeMeta, ruleHelp, ruleLabel } from '@/lib/labels'
import { modeOf } from '@/lib/remediation'
import { formatNumber } from '@/lib/format'
import type { AlertRuleDTO, CategoryRule } from '@/lib/types'
import { translate, useT } from '@/i18n'
import { Trans } from '@/i18n/rich'
import type { TKey } from '@/i18n'
import { GeneralSection } from '@/pages/settings/GeneralSection'
import { PanelUsersSection } from '@/pages/settings/PanelUsersSection'
import { errorText } from '@/lib/errors'

// "general" is open to every signed-in user (theme + language); the rest are admin-only.
const TAB_KEYS = ['general', 'categories', 'removal', 'machines', 'rules', 'users'] as const
type TabKey = (typeof TAB_KEYS)[number]

export function Settings() {
  const t = useT()
  const isAdmin = useIsAdmin()
  // Linkable: /settings?tab=removal (old ?sekme=… links are redirected in App.tsx)
  const [tabParam, setTab] = useUrlState('tab', 'general')
  const tab: TabKey = (TAB_KEYS as readonly string[]).includes(tabParam) ? (tabParam as TabKey) : 'general'

  const general = { key: 'general', label: t('settings.tab.general'), icon: <Settings2 className="size-4" aria-hidden /> }
  const tabs = isAdmin
    ? [
        general,
        { key: 'categories', label: t('settings.tab.categories'), icon: <Tags className="size-4" aria-hidden /> },
        {
          key: 'removal',
          label: t('settings.tab.removal'),
          icon: <PackageX className="size-4" aria-hidden />,
          badge: <RemovalTabBadge />,
        },
        { key: 'machines', label: t('settings.tab.machines'), icon: <Archive className="size-4" aria-hidden /> },
        { key: 'rules', label: t('settings.tab.rules'), icon: <SlidersHorizontal className="size-4" aria-hidden /> },
        { key: 'users', label: t('settings.tab.users'), icon: <UserPlus className="size-4" aria-hidden /> },
      ]
    : [general]
  // A viewer following a link to an admin tab gets a clear notice, not a blank page.
  const locked = !isAdmin && tab !== 'general'

  return (
    <div>
      <PageHeader title={t('nav.settings')} description={isAdmin ? t('settings.subtitle') : t('settings.viewerSubtitle')} />
      <div className="mb-5">
        <Tabs tabs={tabs} value={locked ? '' : tab} onChange={setTab} ariaLabel={t('settings.tabsLabel')} className="w-full sm:w-auto" />
      </div>
      {locked ? (
        <EmptyState icon={Lock} title={t('shell.adminRequiredTitle')} description={t('settings.adminRequiredBody')} />
      ) : (
        <>
          {tab === 'general' && <GeneralSection />}
          {tab === 'categories' && <CategoriesSection />}
          {tab === 'removal' && <RemovalSection />}
          {tab === 'machines' && <MachinesSection />}
          {tab === 'rules' && <AlertRulesSection />}
          {tab === 'users' && <PanelUsersSection />}
        </>
      )}
    </div>
  )
}

/** Shows the live removal mode on the tab itself — an admin should never have to
 *  open the tab to find out that the fleet is currently enforcing. */
function RemovalTabBadge() {
  const { data } = useRemediationOverview()
  if (!data) return null
  const mode = modeOf(data.policy)
  if (mode === 'off') {
    const count = data.policy.apps.length + data.policy.startup.length
    return count > 0 ? <Badge tone="neutral">{count}</Badge> : null
  }
  const meta = removalModeMeta(mode)
  return (
    <Badge tone={meta.token} dot pulse={mode === 'enforce'}>
      {meta.label}
    </Badge>
  )
}

// ── Categories ──────────────────────────────────────────────────────────────────
const MATCH_TYPE_KEYS: Record<string, TKey> = {
  exact: 'settings.cat.matchExact',
  prefix: 'settings.cat.matchPrefix',
  glob: 'settings.cat.matchGlob',
}

// Built per render so the labels follow the active language.
const matchTypes = () => Object.entries(MATCH_TYPE_KEYS).map(([value, key]) => ({ value, label: translate(key) }))
const categoryOptions = () => CATEGORY_ORDER.map((c) => ({ value: c, label: catMeta(c).label }))

type DraftRule = Omit<CategoryRule, 'id'> & { id?: number }

function emptyDraft(): DraftRule {
  return { category: 'other', match_type: 'exact', match_value: '', priority: 100, enabled: true }
}

function CategoriesSection() {
  const t = useT()
  const { data: categories, isLoading } = useCategories()
  const { win } = useRange('7d')
  const { data: unclassified } = useUnclassified(win)
  const createCat = useCreateCategory()
  const updateCat = useUpdateCategory()
  const deleteCat = useDeleteCategory()
  const reclassify = useReclassify()
  const { push } = useToast()

  const [draft, setDraft] = useState<DraftRule | null>(null)
  // Deleting used to fire straight from the icon button, with no confirmation.
  const [pendingDelete, setPendingDelete] = useState<CategoryRule | null>(null)

  const save = () => {
    if (!draft) return
    const done = () => {
      setDraft(null)
      push({ title: t('settings.cat.saved'), tone: 'ok' })
    }
    // e.g. category_rule_conflict — say why instead of failing silently.
    const failed = (err: unknown) =>
      push({ title: t('settings.cat.saveFailed'), description: errorText(err), tone: 'crit' })
    if (draft.id != null) updateCat.mutate({ ...(draft as CategoryRule) }, { onSuccess: done, onError: failed })
    else createCat.mutate(draft, { onSuccess: done, onError: failed })
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
    { key: 'match_type', header: t('settings.cat.match'), render: (r) => <span className="text-text-muted">{MATCH_TYPE_KEYS[r.match_type] ? t(MATCH_TYPE_KEYS[r.match_type]) : r.match_type}</span> },
    { key: 'match_value', header: t('settings.cat.value'), render: (r) => <span className="font-mono text-[13px] text-text">{r.match_value}</span> },
    { key: 'priority', header: t('settings.cat.priority'), align: 'right', sortable: true, sortValue: (r) => r.priority, render: (r) => <span className="tabular text-text-muted">{r.priority}</span> },
    {
      key: 'enabled',
      header: t('common.enabled'),
      align: 'center',
      render: (r) => (
        <div className="flex justify-center">
          <Switch checked={r.enabled} onChange={(v) => updateCat.mutate({ ...r, enabled: v })} label={t('settings.cat.toggleRule', { category: catMeta(r.category).label })} />
        </div>
      ),
    },
    {
      key: 'actions',
      header: '',
      align: 'right',
      render: (r) => (
        <div className="flex justify-end gap-1">
          <button type="button" onClick={() => setDraft(r)} aria-label={t('common.edit')} className="grid size-8 place-items-center rounded-[10px] text-text-muted hover:bg-wash-2 hover:text-text ring-focus">
            <Pencil className="size-4" aria-hidden />
          </button>
          <button type="button" onClick={() => setPendingDelete(r)} aria-label={t('settings.cat.deleteRule', { value: r.match_value })} className="grid size-8 place-items-center rounded-[10px] text-text-muted hover:bg-crit/15 hover:text-crit ring-focus">
            <Trash2 className="size-4" aria-hidden />
          </button>
        </div>
      ),
    },
  ]

  return (
    <div className="grid gap-4 lg:grid-cols-[1.6fr_1fr]">
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
        <div className="px-5 pb-5">
          {isLoading ? (
            <SkeletonText lines={6} />
          ) : (
            <DataTable
              columns={columns}
              rows={categories ?? []}
              getRowKey={(r) => String(r.id)}
              initialSort={{ key: 'priority', dir: 'asc' }}
              emptyLabel={t('settings.cat.empty')}
              className="border-0"
            />
          )}
        </div>
      </Card>

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
              onClick={() => reclassify.mutate('7d', { onSuccess: () => push({ title: t('settings.cat.reclassifyStarted'), tone: 'info' }) })}
            >
              {t('settings.cat.reclassify')}
            </Button>
          }
        />
        <div className="mt-4">
          {!unclassified || unclassified.length === 0 ? (
            <EmptyState icon={Tags} title={t('settings.cat.allClassified')} />
          ) : (
            <ul className="divide-y divide-border">
              {unclassified.slice(0, 12).map((u) => (
                <li key={u.name} className="flex items-center justify-between gap-2 py-2">
                  <span className="truncate font-mono text-[13px] text-text">{u.name}</span>
                  <div className="flex items-center gap-2">
                    <span className="tabular text-[12px] text-text-dim">{formatNumber(u.count)}</span>
                    <button
                      type="button"
                      onClick={() => setDraft({ ...emptyDraft(), match_value: u.name })}
                      className="rounded-[8px] px-2 py-1 text-[12px] font-medium text-accent hover:bg-accent/10 ring-focus"
                    >
                      {t('settings.cat.addRule')}
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      </Card>

      <Modal
        open={draft !== null}
        onClose={() => setDraft(null)}
        title={draft?.id != null ? t('settings.cat.editTitle') : t('settings.cat.newTitle')}
        footer={
          <>
            <Button variant="ghost" onClick={() => setDraft(null)}>{t('common.cancel')}</Button>
            <Button variant="primary" leftIcon={<Save className="size-4" aria-hidden />} loading={createCat.isPending || updateCat.isPending} onClick={save} disabled={!draft?.match_value.trim()}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        {draft && (
          <div className="space-y-4 pb-2">
            <Select label={t('common.category')} options={categoryOptions()} value={draft.category} onChange={(e) => setDraft({ ...draft, category: e.target.value })} />
            <div className="grid grid-cols-2 gap-3">
              <Select label={t('settings.cat.matchType')} options={matchTypes()} value={draft.match_type} onChange={(e) => setDraft({ ...draft, match_type: e.target.value })} />
              <Input label={t('settings.cat.priority')} type="number" value={draft.priority} onChange={(e) => setDraft({ ...draft, priority: Number(e.target.value) || 0 })} />
            </div>
            <Input label={t('settings.cat.matchValue')} placeholder={t('settings.cat.matchValuePlaceholder')} value={draft.match_value} onChange={(e) => setDraft({ ...draft, match_value: e.target.value })} hint={t('settings.cat.matchValueHint')} />
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

// ── Alert rules ─────────────────────────────────────────────────────────────────
function AlertRulesSection() {
  const t = useT()
  const { data, isLoading } = useAlertRules()
  const update = useUpdateAlertRules()
  const { push } = useToast()
  const [rules, setRules] = useState<AlertRuleDTO[]>([])
  // Seed from the server only while nothing is being edited: a background refetch
  // used to overwrite thresholds the admin had typed but not saved yet.
  const [dirty, setDirty] = useState(false)

  useEffect(() => {
    if (data && !dirty) setRules(data)
  }, [data, dirty])

  const patch = (rule: string, changes: Partial<AlertRuleDTO>) => {
    setDirty(true)
    setRules((list) => list.map((r) => (r.rule === rule ? { ...r, ...changes } : r)))
  }

  const num = (v: string): number | null => (v.trim() === '' ? null : Number(v))

  return (
    <Card>
      <CardHeader
        title={t('settings.rules.title')}
        description={t('settings.rules.body')}
        action={
          <Button
            variant="primary"
            size="sm"
            leftIcon={<Save className="size-4" aria-hidden />}
            loading={update.isPending}
            onClick={() =>
              update.mutate(rules, {
                onSuccess: () => {
                  setDirty(false)
                  push({ title: t('settings.rules.saved'), tone: 'ok' })
                },
                onError: (err) => push({ title: t('settings.rules.saveFailed'), description: errorText(err), tone: 'crit' }),
              })
            }
          >
            {t('common.save')}
          </Button>
        }
      />
      <div className="mt-4 overflow-x-auto">
        {isLoading ? (
          <SkeletonText lines={6} />
        ) : rules.length === 0 ? (
          <EmptyState icon={SlidersHorizontal} title={t('settings.rules.empty')} />
        ) : (
          <table className="w-full text-left text-[13px]">
            <thead>
              <tr className="border-b border-border text-[11px] font-semibold uppercase tracking-[0.08em] text-text-dim">
                <th className="py-2.5 pr-3">{t('settings.rules.rule')}</th>
                <th className="px-3 text-center">{t('common.enabled')}</th>
                <th className="px-3 text-right">{t('severity.warn')}</th>
                <th className="px-3 text-right">{t('severity.crit')}</th>
                <th className="px-3 text-right">{t('settings.rules.duration')}</th>
              </tr>
            </thead>
            <tbody>
              {rules.map((r) => (
                <tr key={r.rule} className="border-b border-border/70 last:border-0">
                  <td className="py-3 pr-3">
                    <span className="block font-medium text-text">{ruleLabel(r.rule)}</span>
                    {ruleHelp(r.rule) && <span className="mt-0.5 block text-[12px] text-text-dim">{ruleHelp(r.rule)}</span>}
                  </td>
                  <td className="px-3 text-center">
                    <div className="flex justify-center">
                      <Switch checked={r.enabled} onChange={(v) => patch(r.rule, { enabled: v })} label={t('settings.rules.enabledFor', { rule: ruleLabel(r.rule) })} />
                    </div>
                  </td>
                  <td className="px-3">
                    <input
                      type="number"
                      value={r.warn_threshold ?? ''}
                      onChange={(e) => patch(r.rule, { warn_threshold: num(e.target.value) })}
                      className="tabular h-9 w-24 rounded-[10px] border border-border-2 bg-surface-2/60 px-2 text-right text-text outline-none focus:border-accent/60 focus:ring-2 focus:ring-accent/25"
                      aria-label={t('settings.rules.warnFor', { rule: ruleLabel(r.rule) })}
                    />
                  </td>
                  <td className="px-3">
                    <input
                      type="number"
                      value={r.crit_threshold ?? ''}
                      onChange={(e) => patch(r.rule, { crit_threshold: num(e.target.value) })}
                      className="tabular h-9 w-24 rounded-[10px] border border-border-2 bg-surface-2/60 px-2 text-right text-text outline-none focus:border-accent/60 focus:ring-2 focus:ring-accent/25"
                      aria-label={t('settings.rules.critFor', { rule: ruleLabel(r.rule) })}
                    />
                  </td>
                  <td className="px-3">
                    <input
                      type="number"
                      value={r.for_seconds}
                      onChange={(e) => patch(r.rule, { for_seconds: Number(e.target.value) || 0 })}
                      className="tabular h-9 w-24 rounded-[10px] border border-border-2 bg-surface-2/60 px-2 text-right text-text outline-none focus:border-accent/60 focus:ring-2 focus:ring-accent/25"
                      aria-label={t('settings.rules.durationFor', { rule: ruleLabel(r.rule) })}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </Card>
  )
}

