import { useEffect, useMemo, useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { Building2, Clock, Palette, Plus, Save, ShieldCheck, X } from 'lucide-react'
import { Card, CardHeader } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Switch } from '@/components/ui/Switch'
import { LanguagePicker, Segmented, ThemePicker } from '@/components/PrefControls'
import { SkeletonText } from '@/components/Skeleton'
import { useFleet, useGeneralSettings, useUpdateGeneralSettings } from '@/hooks/queries'
import { useAuth, useIsAdmin } from '@/hooks/useAuth'
import { useToast } from '@/hooks/useToast'
import { errorText } from '@/lib/errors'
import { LANGS, LANG_NAMES, useT } from '@/i18n'
import type { Lang } from '@/i18n'
import type { GeneralSettings, WorkHours } from '@/lib/types'
import { FilterChip } from '@/components/FilterChip'
import { weekdayNames } from '@/lib/format'

/** Settings → General. Everyone: theme + language. Admins: server-wide settings. */
export function GeneralSection() {
  const t = useT()
  const isAdmin = useIsAdmin()
  return (
    <div className="grid items-start gap-4 lg:grid-cols-2">
      <Card>
        <div className="flex items-start gap-3">
          <span className="grid size-9 shrink-0 place-items-center rounded-[12px] bg-glass text-accent" aria-hidden>
            <Palette className="size-[18px]" />
          </span>
          <CardHeader
            title={t('settings.general.appearanceTitle')}
            description={t('settings.general.appearanceBody')}
            className="flex-1"
          />
        </div>
        <div className="mt-5 space-y-5">
          <ThemePicker hint />
          <LanguagePicker hint />
        </div>
      </Card>

      {isAdmin && <OrganisationCard />}
      {isAdmin && <WorkHoursCard />}
    </div>
  )
}

function OrganisationCard() {
  const t = useT()
  const { user } = useAuth()
  const { push } = useToast()
  const { data, isLoading, isError } = useGeneralSettings()
  const update = useUpdateGeneralSettings()
  const [draft, setDraft] = useState<Omit<GeneralSettings, 'work_hours'> | null>(null)

  // Seed once per server copy; a refetch after save re-seeds with what was stored.
  useEffect(() => {
    if (data) setDraft({ org_name: data.org_name ?? '', default_lang: data.default_lang, require_2fa: !!data.require_2fa })
  }, [data])

  // The server refuses to turn the policy on for an admin without 2FA
  // (409 enable_own_2fa_first) — so do not offer a switch that cannot work.
  const ownTwoFactor = user?.totp_enabled === true
  const canRequire = ownTwoFactor || draft?.require_2fa === true

  const dirty =
    !!draft &&
    !!data &&
    (draft.org_name.trim() !== (data.org_name ?? '') ||
      draft.default_lang !== data.default_lang ||
      draft.require_2fa !== !!data.require_2fa)

  const save = (e: FormEvent) => {
    e.preventDefault()
    if (!draft) return
    update.mutate(
      { ...draft, org_name: draft.org_name.trim() },
      {
        onSuccess: () => push({ title: t('settings.general.saved'), tone: 'ok' }),
        onError: (err) => push({ title: t('settings.general.saveFailed'), description: errorText(err), tone: 'crit' }),
      },
    )
  }

  return (
    <Card>
      <div className="flex items-start gap-3">
        <span className="grid size-9 shrink-0 place-items-center rounded-[12px] bg-glass text-accent" aria-hidden>
          <Building2 className="size-[18px]" />
        </span>
        <CardHeader title={t('settings.general.orgTitle')} description={t('settings.general.orgBody')} className="flex-1" />
      </div>

      {isLoading || (!draft && !isError) ? (
        <SkeletonText lines={5} className="mt-5" />
      ) : isError || !draft ? (
        <p role="alert" className="mt-5 text-[13px] text-crit">
          {t('settings.general.loadFailed')}
        </p>
      ) : (
        <form onSubmit={save} className="mt-5 space-y-5">
          <Input
            label={t('settings.general.orgName')}
            hint={t('settings.general.orgNameHint')}
            value={draft.org_name}
            maxLength={120}
            onChange={(e) => setDraft({ ...draft, org_name: e.target.value })}
            autoComplete="organization"
          />

          <Segmented<Lang>
            label={t('settings.general.defaultLang')}
            hint={t('settings.general.defaultLangHint')}
            value={draft.default_lang}
            onChange={(lang) => setDraft({ ...draft, default_lang: lang })}
            options={LANGS.map((l) => ({ value: l, label: LANG_NAMES[l], lang: l }))}
          />

          <div className="rounded-[var(--radius-md)] border border-border bg-surface-2/40 p-4">
            <div className="flex items-start justify-between gap-4">
              <div className="min-w-0">
                <p id="require-2fa-label" className="flex items-center gap-2 text-[14px] font-medium text-text">
                  <ShieldCheck className="size-4 shrink-0 text-accent" aria-hidden />
                  {t('settings.general.require2fa')}
                </p>
                <p id="require-2fa-desc" className="mt-1 text-[13px] text-text-muted">
                  {t('settings.general.require2faBody')}
                </p>
              </div>
              <Switch
                checked={draft.require_2fa}
                disabled={!canRequire}
                onChange={(v) => setDraft({ ...draft, require_2fa: v })}
                label={t('settings.general.require2fa')}
              />
            </div>
            {!ownTwoFactor && (
              <p className="mt-3 flex flex-wrap items-center gap-x-2 gap-y-1 border-t border-border pt-3 text-[12px] text-warn">
                <span>{t('settings.general.require2faBlocked')}</span>
                <Link to="/profile#two-factor" className="font-semibold text-accent hover:underline ring-focus">
                  {t('settings.general.require2faLink')}
                </Link>
              </p>
            )}
          </div>

          <div className="flex justify-end">
            <Button
              type="submit"
              variant="primary"
              disabled={!dirty}
              loading={update.isPending}
              leftIcon={<Save className="size-4" aria-hidden />}
            >
              {t('common.save')}
            </Button>
          </div>
        </form>
      )}
    </Card>
  )
}

type WorkHoursDraft = Omit<WorkHours, 'timezone'>

function sameDraft(a: WorkHoursDraft, b: WorkHoursDraft): boolean {
  return (
    a.start === b.start &&
    a.end === b.end &&
    a.grace_minutes === b.grace_minutes &&
    a.offline_after_minutes === b.offline_after_minutes &&
    a.stale_after_days === b.stale_after_days &&
    a.days.join() === b.days.join() &&
    a.tags.join('\u0000') === b.tags.join('\u0000')
  )
}

const clampInt = (v: string, lo: number, hi: number) => Math.min(hi, Math.max(lo, Math.round(Number(v) || 0)))

/** Settings → General → Work Hours (admin). Saved on its own: PUT {work_hours}. */
function WorkHoursCard() {
  const t = useT()
  const { push } = useToast()
  const { data, isLoading, isError } = useGeneralSettings()
  const update = useUpdateGeneralSettings()
  const { data: fleet } = useFleet()
  const [draft, setDraft] = useState<WorkHoursDraft | null>(null)
  const [newTag, setNewTag] = useState('')
  const weekdays = weekdayNames() // Monday first

  const stored = data?.work_hours
  useEffect(() => {
    if (stored) {
      setDraft({
        days: [...stored.days],
        start: stored.start,
        end: stored.end,
        tags: [...stored.tags],
        grace_minutes: stored.grace_minutes,
        offline_after_minutes: stored.offline_after_minutes,
        stale_after_days: stored.stale_after_days ?? 1,
      })
    }
  }, [stored])

  // Tags in use on machines, offered as one-click suggestions.
  const knownTags = useMemo(() => {
    const seen = new Map<string, string>()
    for (const m of fleet ?? []) {
      for (const tag of m.tags ?? []) if (!seen.has(tag.toLowerCase())) seen.set(tag.toLowerCase(), tag)
    }
    return [...seen.values()].sort((a, b) => a.localeCompare(b))
  }, [fleet])

  if (isLoading || (!draft && !isError)) {
    return (
      <Card className="lg:col-span-2">
        <SkeletonText lines={6} />
      </Card>
    )
  }
  if (isError || !draft || !stored) {
    return (
      <Card className="lg:col-span-2">
        <p role="alert" className="text-[13px] text-crit">
          {t('settings.general.loadFailed')}
        </p>
      </Card>
    )
  }

  const has = (tag: string) => draft.tags.some((x) => x.toLowerCase() === tag.trim().toLowerCase())
  const addTag = (tag: string) => {
    const v = tag.trim()
    if (!v || has(v)) return
    setDraft({ ...draft, tags: [...draft.tags, v] })
    setNewTag('')
  }
  const toggleDay = (d: number) =>
    setDraft({
      ...draft,
      days: draft.days.includes(d) ? draft.days.filter((x) => x !== d) : [...draft.days, d].sort((a, b) => a - b),
    })
  const suggestions = knownTags.filter((x) => !has(x))

  // "HH:MM" strings compare correctly as text.
  const rangeOk = draft.start !== '' && draft.end !== '' && draft.end > draft.start
  const dirty = !sameDraft(draft, stored)

  const save = (e: FormEvent) => {
    e.preventDefault()
    if (!rangeOk) return
    update.mutate(
      { work_hours: draft },
      {
        onSuccess: () => push({ title: t('settings.general.workHours.saved'), tone: 'ok' }),
        onError: (err) => push({ title: t('settings.general.workHours.saveFailed'), description: errorText(err), tone: 'crit' }),
      },
    )
  }

  return (
    <Card className="lg:col-span-2">
      <div className="flex items-start gap-3">
        <span className="grid size-9 shrink-0 place-items-center rounded-[12px] bg-glass text-accent" aria-hidden>
          <Clock className="size-[18px]" />
        </span>
        <CardHeader title={t('settings.general.workHours.title')} description={t('settings.general.workHours.body')} className="flex-1" />
      </div>

      <form onSubmit={save} className="mt-5 grid gap-5 lg:grid-cols-2">
        <div className="space-y-5">
          <fieldset>
            <legend className="mb-1.5 block text-[13px] font-medium text-text-muted">{t('settings.general.workHours.days')}</legend>
            <div className="flex flex-wrap gap-2">
              {weekdays.map((name, i) => (
                <FilterChip key={i} active={draft.days.includes(i + 1)} onClick={() => toggleDay(i + 1)}>
                  {name}
                </FilterChip>
              ))}
            </div>
            {draft.days.length === 0 && <p className="mt-2 text-[12px] text-warn">{t('settings.general.workHours.noDays')}</p>}
          </fieldset>

          <div>
            <div className="grid grid-cols-2 gap-3">
              <Input
                type="time"
                label={t('settings.general.workHours.start')}
                value={draft.start}
                onChange={(e) => setDraft({ ...draft, start: e.target.value })}
                required
              />
              <Input
                type="time"
                label={t('settings.general.workHours.end')}
                value={draft.end}
                onChange={(e) => setDraft({ ...draft, end: e.target.value })}
                error={rangeOk ? undefined : t('settings.general.workHours.invalidRange')}
                required
              />
            </div>
            <p className="mt-2 text-[12px] text-text-dim">
              {t('settings.general.workHours.timezone', { tz: stored.timezone ?? 'Europe/Istanbul' })}
            </p>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <Input
              type="number"
              min={0}
              max={240}
              label={t('settings.general.workHours.grace')}
              hint={t('settings.general.workHours.graceHint')}
              value={draft.grace_minutes}
              onChange={(e) => setDraft({ ...draft, grace_minutes: clampInt(e.target.value, 0, 240) })}
            />
            <Input
              type="number"
              min={5}
              max={480}
              label={t('settings.general.workHours.offlineAfter')}
              hint={t('settings.general.workHours.offlineAfterHint')}
              value={draft.offline_after_minutes}
              onChange={(e) => setDraft({ ...draft, offline_after_minutes: clampInt(e.target.value, 5, 480) })}
            />
            <Input
              type="number"
              min={1}
              max={30}
              label={t('settings.general.workHours.staleAfter')}
              hint={t('settings.general.workHours.staleAfterHint')}
              value={draft.stale_after_days}
              onChange={(e) => setDraft({ ...draft, stale_after_days: clampInt(e.target.value, 1, 30) })}
            />
          </div>
        </div>

        <div className="space-y-4">
          <div>
            <p className="mb-1.5 block text-[13px] font-medium text-text-muted">{t('settings.general.workHours.tags')}</p>
            <p className="mb-3 text-[12px] text-text-dim">{t('settings.general.workHours.tagsHint')}</p>
            {draft.tags.length === 0 ? (
              <p className="text-[12px] text-warn">{t('settings.general.workHours.noTags')}</p>
            ) : (
              <ul className="flex flex-wrap gap-2">
                {draft.tags.map((tag) => (
                  <li key={tag}>
                    <span className="inline-flex items-center gap-1 rounded-full border border-accent/40 bg-accent/10 py-1 pl-3 pr-1 text-[13px] font-medium text-text">
                      {tag}
                      <button
                        type="button"
                        className="grid size-5 place-items-center rounded-full text-text-dim hover:text-crit ring-focus"
                        onClick={() => setDraft({ ...draft, tags: draft.tags.filter((x) => x !== tag) })}
                        aria-label={t('settings.general.workHours.removeTag', { tag })}
                      >
                        <X className="size-3.5" aria-hidden />
                      </button>
                    </span>
                  </li>
                ))}
              </ul>
            )}
            <div className="mt-3 flex items-end gap-2">
              <Input
                aria-label={t('settings.general.workHours.tagPlaceholder')}
                placeholder={t('settings.general.workHours.tagPlaceholder')}
                value={newTag}
                maxLength={64}
                onChange={(e) => setNewTag(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    addTag(newTag)
                  }
                }}
              />
              <Button
                type="button"
                variant="secondary"
                leftIcon={<Plus className="size-4" aria-hidden />}
                onClick={() => addTag(newTag)}
                disabled={!newTag.trim()}
              >
                {t('settings.general.workHours.addTag')}
              </Button>
            </div>
            {suggestions.length > 0 && (
              <div className="mt-3 flex flex-wrap items-center gap-2 text-[12px] text-text-dim">
                <span>{t('settings.general.workHours.suggestions')}</span>
                {suggestions.map((tag) => (
                  <FilterChip key={tag} active={false} onClick={() => addTag(tag)}>
                    {tag}
                  </FilterChip>
                ))}
              </div>
            )}
          </div>
          <p className="rounded-[var(--radius-md)] border border-border bg-surface-2/40 p-3 text-[12px] text-text-muted">
            {t('settings.general.workHours.behaviour')}
          </p>
        </div>

        <div className="flex justify-end lg:col-span-2">
          <Button
            type="submit"
            variant="primary"
            disabled={!dirty || !rangeOk}
            loading={update.isPending}
            leftIcon={<Save className="size-4" aria-hidden />}
          >
            {t('common.save')}
          </Button>
        </div>
      </form>
    </Card>
  )
}
