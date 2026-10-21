import { useEffect, useState } from 'react'
import {
  Archive, Lock, Save, Settings2, SlidersHorizontal, Tags, UserPlus,
} from 'lucide-react'
import { PageHeader } from '@/components/AppShell'
import { Tabs } from '@/components/ui/Tabs'
import { Card, CardHeader } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { Switch } from '@/components/ui/Switch'
import { EmptyState } from '@/components/EmptyState'
import { SkeletonText } from '@/components/Skeleton'
import { MachinesSection } from '@/pages/settings/MachinesSection'
import { CategoriesSection } from '@/pages/settings/CategoriesSection'
import { CrashIgnoreEditor } from '@/pages/settings/CrashIgnoreEditor'
import { useAlertRules, useUpdateAlertRules } from '@/hooks/queries'
import { useIsAdmin } from '@/hooks/useAuth'
import { useToast } from '@/hooks/useToast'
import { useUrlState } from '@/hooks/useUrlState'
import { ruleHelp, ruleLabel } from '@/lib/labels'
import type { AlertRuleDTO } from '@/lib/types'
import { useT } from '@/i18n'
import { GeneralSection } from '@/pages/settings/GeneralSection'
import { PanelUsersSection } from '@/pages/settings/PanelUsersSection'
import { errorText } from '@/lib/errors'

// "general" is open to every signed-in user (theme + language); the rest are admin-only.
const TAB_KEYS = ['general', 'categories', 'machines', 'rules', 'users'] as const
type TabKey = (typeof TAB_KEYS)[number]

export function Settings() {
  const t = useT()
  const isAdmin = useIsAdmin()
  // Linkable: /settings?tab=machines (old ?sekme=… links are redirected in App.tsx;
  // ?tab=removal now goes to /optimization, see SettingsRoute there)
  const [tabParam, setTab] = useUrlState('tab', 'general')
  const tab: TabKey = (TAB_KEYS as readonly string[]).includes(tabParam) ? (tabParam as TabKey) : 'general'

  const general = { key: 'general', label: t('settings.tab.general'), icon: <Settings2 className="size-4" aria-hidden /> }
  const tabs = isAdmin
    ? [
        general,
        { key: 'categories', label: t('settings.tab.categories'), icon: <Tags className="size-4" aria-hidden /> },
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
          {tab === 'machines' && <MachinesSection />}
          {tab === 'rules' && <AlertRulesSection />}
          {tab === 'users' && <PanelUsersSection />}
        </>
      )}
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
  // Only a server that returns ignore_apps for crash_storm can store it.
  const crashStorm = rules.find((r) => r.rule === 'crash_storm' && Array.isArray(r.ignore_apps))

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
      {crashStorm && (
        <div className="mt-6 border-t border-border pt-5">
          <CrashIgnoreEditor
            value={crashStorm.ignore_apps ?? []}
            onChange={(next) => patch('crash_storm', { ignore_apps: next })}
          />
        </div>
      )}
    </Card>
  )
}

