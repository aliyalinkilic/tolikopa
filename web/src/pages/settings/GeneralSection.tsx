import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { Building2, Palette, Save, ShieldCheck } from 'lucide-react'
import { Card, CardHeader } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Switch } from '@/components/ui/Switch'
import { LanguagePicker, Segmented, ThemePicker } from '@/components/PrefControls'
import { SkeletonText } from '@/components/Skeleton'
import { useGeneralSettings, useUpdateGeneralSettings } from '@/hooks/queries'
import { useAuth, useIsAdmin } from '@/hooks/useAuth'
import { useToast } from '@/hooks/useToast'
import { errorText } from '@/lib/errors'
import { LANGS, LANG_NAMES, useT } from '@/i18n'
import type { Lang } from '@/i18n'
import type { GeneralSettings } from '@/lib/types'

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
    </div>
  )
}

function OrganisationCard() {
  const t = useT()
  const { user } = useAuth()
  const { push } = useToast()
  const { data, isLoading, isError } = useGeneralSettings()
  const update = useUpdateGeneralSettings()
  const [draft, setDraft] = useState<GeneralSettings | null>(null)

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
