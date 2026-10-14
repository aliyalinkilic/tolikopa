import { useEffect, useId, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { Save } from 'lucide-react'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Checkbox } from '@/components/ui/Checkbox'
import { SETUP_TAGS } from './parts'
import { useSaveSetupConfig } from '@/hooks/queries'
import { useToast } from '@/hooks/useToast'
import { ApiError } from '@/lib/api'
import { errorText } from '@/lib/errors'
import { cn } from '@/lib/cn'
import { useT } from '@/i18n'
import { Trans } from '@/i18n/rich'
import type { SetupConfig } from '@/lib/types'

type Field = keyof SetupConfig
type FieldErrors = Partial<Record<Field, string>>

const URL_RE = /^https:\/\/[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:\d{1,5})?$/
const FQDN_RE = /^(?=.{3,253}$)[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+$/

export function isServerUrl(v: string): boolean {
  return URL_RE.test(v.trim())
}

/** corp.example.com → DC=corp,DC=example,DC=com */
export function dcSuffix(fqdn: string): string {
  return fqdn
    .trim()
    .split('.')
    .filter(Boolean)
    .map((part) => `DC=${part}`)
    .join(',')
}

/** Which field a 400 `invalid_request` is about, if the server said. */
function serverField(err: unknown): Field | null {
  if (err instanceof ApiError && err.code === 'invalid_request' && typeof err.params?.field === 'string') {
    return err.params.field as Field
  }
  return null
}

function RadioCard({
  name,
  checked,
  onChange,
  title,
  description,
  children,
}: {
  name: string
  checked: boolean
  onChange: () => void
  title: ReactNode
  description?: ReactNode
  children?: ReactNode
}) {
  return (
    // The extra content sits outside the <label>: it holds its own labelled input.
    <div
      className={cn(
        'rounded-[var(--radius-md)] border transition-colors',
        checked ? 'border-accent/40 bg-accent/[0.07]' : 'border-border bg-surface-2/30 hover:bg-surface-2/60',
      )}
    >
      <label className="ring-focus-within flex cursor-pointer items-start gap-3 rounded-[inherit] p-3.5">
        <input type="radio" name={name} checked={checked} onChange={onChange} className="sr-only" />
        <span
          className={cn('mt-0.5 grid size-4 shrink-0 place-items-center rounded-full border', checked ? 'border-accent' : 'border-border-2')}
          aria-hidden
        >
          {checked && <span className="size-2 rounded-full bg-accent" />}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block break-words text-[13px] font-medium text-text">{title}</span>
          {description && <span className="mt-0.5 block text-[12px] text-text-muted">{description}</span>}
        </span>
      </label>
      {children && <div className="px-3.5 pb-3.5 pl-[42px]">{children}</div>}
    </div>
  )
}

interface FormProps {
  config: SetupConfig
  /** Called with the server's copy after a successful save. */
  onSaved?: (config: SetupConfig) => void
  submitLabel?: string
  /** Extra buttons rendered left of the submit button (wizard "Back"). */
  secondary?: ReactNode
  /** Submit even when nothing changed (the wizard advances on submit). */
  allowUnchanged?: boolean
  /** Lets a parent know whether there are unsaved edits. */
  onDirtyChange?: (dirty: boolean) => void
}

// ── Server address ─────────────────────────────────────────────────────────────
export function ServerUrlForm({
  config,
  candidates,
  onSaved,
  submitLabel,
  secondary,
  allowUnchanged,
}: FormProps & { candidates: string[] }) {
  const t = useT()
  const name = useId()
  const { push } = useToast()
  const save = useSaveSetupConfig()
  const saved = config.server_url ?? ''
  const options = [...new Set(candidates.filter(Boolean))]
  const initialCustom = saved !== '' && !options.includes(saved)
  const [choice, setChoice] = useState<string>(initialCustom ? 'custom' : saved || options[0] || 'custom')
  const [custom, setCustom] = useState(initialCustom ? saved : '')
  const [error, setError] = useState<string | null>(null)

  const value = (choice === 'custom' ? custom : choice).trim()
  const valid = isServerUrl(value)
  const changed = value !== saved

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!valid) {
      setError(t('setup.server.invalid'))
      return
    }
    if (!changed) {
      if (allowUnchanged) onSaved?.(config)
      return
    }
    setError(null)
    save.mutate(
      { server_url: value },
      {
        onSuccess: (next) => {
          push({ title: t('setup.domain.saved'), tone: 'ok' })
          onSaved?.(next ?? { ...config, server_url: value })
        },
        onError: (err) => {
          if (serverField(err) === 'server_url') setError(t('setup.server.invalid'))
          else push({ title: t('setup.domain.saveFailed'), description: errorText(err), tone: 'crit' })
        },
      },
    )
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <fieldset className="space-y-2">
        {options.length > 0 && (
          <legend className="mb-2 text-[13px] font-medium text-text-muted">{t('setup.server.candidates')}</legend>
        )}
        {options.map((url) => (
          <RadioCard
            key={url}
            name={name}
            checked={choice === url}
            onChange={() => {
              setChoice(url)
              setError(null)
            }}
            title={<span className="font-mono">{url}</span>}
          />
        ))}
        <RadioCard
          name={name}
          checked={choice === 'custom'}
          onChange={() => setChoice('custom')}
          title={t('setup.server.custom')}
        >
          {choice === 'custom' && (
            <div>
              <Input
                aria-label={t('setup.server.customLabel')}
                value={custom}
                onChange={(e) => {
                  setCustom(e.target.value)
                  setError(null)
                }}
                placeholder="https://tolikopa.corp.example.com"
                hint={t('setup.server.hint')}
                error={error ?? (custom.trim() !== '' && !isServerUrl(custom) ? t('setup.server.invalid') : undefined)}
                inputMode="url"
                autoCapitalize="none"
                spellCheck={false}
                className="font-mono"
              />
            </div>
          )}
        </RadioCard>
      </fieldset>
      {error && choice !== 'custom' && (
        <p role="alert" className="text-[12px] text-crit">
          {error}
        </p>
      )}
      <div className="flex flex-wrap justify-end gap-2">
        {secondary}
        <Button
          type="submit"
          variant="primary"
          loading={save.isPending}
          disabled={!valid || (!changed && !allowUnchanged)}
          leftIcon={<Save className="size-4" aria-hidden />}
        >
          {submitLabel ?? t('common.save')}
        </Button>
      </div>
    </form>
  )
}

// ── Domain ─────────────────────────────────────────────────────────────────────
interface DomainDraft {
  domain_fqdn: string
  netbios: string
  ou: boolean
  target_dn: string
  gpo_name: string
  defender_exclusion: boolean
}

const DEFAULT_GPO = 'Tolikopa Agent'

function toDraft(c: SetupConfig): DomainDraft {
  return {
    domain_fqdn: c.domain_fqdn ?? '',
    netbios: c.netbios ?? '',
    ou: (c.target_dn ?? '') !== '',
    target_dn: c.target_dn ?? '',
    gpo_name: c.gpo_name || DEFAULT_GPO,
    defender_exclusion: !!c.defender_exclusion,
  }
}

function toPatch(d: DomainDraft): Omit<SetupConfig, 'server_url'> {
  return {
    domain_fqdn: d.domain_fqdn.trim().toLowerCase(),
    netbios: d.netbios.trim().toUpperCase(),
    target_dn: d.ou ? d.target_dn.trim() : '',
    gpo_name: d.gpo_name.trim(),
    defender_exclusion: d.defender_exclusion,
  }
}

/**
 * Domain FQDN, NetBIOS, link target (whole domain or one OU), GPO name and the
 * Defender exclusion. Shared by wizard step 4 and the Setup page. Server-side
 * validation errors (400 invalid_request + params.field) land on their field.
 */
export function DomainConfigForm({ config, onSaved, submitLabel, secondary, allowUnchanged, onDirtyChange }: FormProps) {
  const t = useT()
  const name = useId()
  const { push } = useToast()
  const save = useSaveSetupConfig()
  const [draft, setDraft] = useState<DomainDraft>(() => toDraft(config))
  const [serverErrors, setServerErrors] = useState<FieldErrors>({})
  const [touched, setTouched] = useState(false)

  const base = toPatch(toDraft(config))
  const patch = toPatch(draft)
  const changed = (Object.keys(patch) as (keyof typeof patch)[]).some((k) => patch[k] !== base[k])
  // An untouched default GPO name still has to be written once.
  const unsaved = changed || (config.gpo_name ?? '') !== patch.gpo_name

  useEffect(() => {
    onDirtyChange?.(changed)
  }, [changed, onDirtyChange])

  const set = (next: Partial<DomainDraft>, field?: Field) => {
    setDraft((d) => ({ ...d, ...next }))
    if (field) setServerErrors((e) => ({ ...e, [field]: undefined }))
  }

  const suffix = dcSuffix(draft.domain_fqdn)
  const fqdnOk = FQDN_RE.test(draft.domain_fqdn.trim())
  const netbiosOk = draft.netbios.trim() === '' || /^[^\s\\/:*?"<>|.]{1,15}$/.test(draft.netbios.trim())
  const dn = draft.target_dn.trim()
  const dnError = !draft.ou
    ? null
    : dn === ''
      ? t('setup.domain.targetDnRequired')
      : fqdnOk && !dn.toLowerCase().replace(/\s*,\s*/g, ',').endsWith(suffix.toLowerCase())
        ? t('setup.domain.suffixMismatch', { suffix })
        : null
  const gpoOk = draft.gpo_name.trim() !== ''
  const valid = fqdnOk && netbiosOk && !dnError && gpoOk

  const show = (cond: boolean, message: string): string | undefined => (touched && cond ? message : undefined)

  const submit = (e: FormEvent) => {
    e.preventDefault()
    setTouched(true)
    if (!valid) return
    if (!unsaved) {
      if (allowUnchanged) onSaved?.(config)
      return
    }
    setServerErrors({})
    save.mutate(patch, {
      onSuccess: (next) => {
        push({ title: t('setup.domain.saved'), tone: 'ok' })
        if (next) setDraft(toDraft(next)) // show what the server stored (it normalises)
        setTouched(false)
        onSaved?.(next ?? { ...config, ...patch })
      },
      onError: (err) => {
        const field = serverField(err)
        if (field) setServerErrors({ [field]: t('setup.domain.fieldInvalid') })
        else push({ title: t('setup.domain.saveFailed'), description: errorText(err), tone: 'crit' })
      },
    })
  }

  return (
    <form onSubmit={submit} className="space-y-5" noValidate>
      <div className="grid gap-4 sm:grid-cols-[1.6fr_1fr]">
        <Input
          label={t('setup.domain.fqdn')}
          hint={t('setup.domain.fqdnHint')}
          error={serverErrors.domain_fqdn ?? show(!fqdnOk, t('setup.domain.fqdnInvalid'))}
          value={draft.domain_fqdn}
          onChange={(e) => set({ domain_fqdn: e.target.value }, 'domain_fqdn')}
          placeholder="corp.example.com"
          autoCapitalize="none"
          spellCheck={false}
          className="font-mono"
        />
        <Input
          label={t('setup.domain.netbios')}
          hint={t('setup.domain.netbiosHint')}
          error={serverErrors.netbios ?? (!netbiosOk ? t('setup.domain.netbiosInvalid') : undefined)}
          value={draft.netbios}
          maxLength={15}
          onChange={(e) => set({ netbios: e.target.value }, 'netbios')}
          placeholder="CORP"
          autoCapitalize="characters"
          spellCheck={false}
          className="font-mono uppercase"
        />
      </div>

      <fieldset className="space-y-2">
        <legend className="mb-2 text-[13px] font-medium text-text-muted">{t('setup.domain.target')}</legend>
        <RadioCard
          name={name}
          checked={!draft.ou}
          onChange={() => set({ ou: false }, 'target_dn')}
          title={t('setup.domain.targetAll')}
          description={t('setup.domain.targetAllBody')}
        />
        <RadioCard
          name={name}
          checked={draft.ou}
          onChange={() => set({ ou: true })}
          title={t('setup.domain.targetOu')}
          description={t('setup.domain.targetOuBody')}
        >
          {draft.ou && (
            <div className="space-y-1.5">
              <Input
                label={t('setup.domain.targetDn')}
                error={serverErrors.target_dn ?? (touched || dn !== '' ? (dnError ?? undefined) : undefined)}
                value={draft.target_dn}
                onChange={(e) => set({ target_dn: e.target.value }, 'target_dn')}
                placeholder={`OU=Workstations,${suffix || 'DC=corp,DC=example,DC=com'}`}
                autoCapitalize="none"
                spellCheck={false}
                className="font-mono"
              />
              {suffix && (
                <span className="block text-[12px] text-text-muted">
                  <Trans k="setup.domain.expectedSuffix" values={{ suffix }} tags={SETUP_TAGS} />
                </span>
              )}
              <span className="block text-[12px] text-text-dim">{t('setup.domain.targetDnHint')}</span>
            </div>
          )}
        </RadioCard>
      </fieldset>

      <Input
        label={t('setup.domain.gpoName')}
        error={serverErrors.gpo_name ?? show(!gpoOk, t('setup.domain.gpoNameRequired'))}
        value={draft.gpo_name}
        maxLength={120}
        onChange={(e) => set({ gpo_name: e.target.value }, 'gpo_name')}
      />

      <Checkbox
        checked={draft.defender_exclusion}
        onChange={(v) => set({ defender_exclusion: v })}
        label={t('setup.domain.defender')}
        description={t('setup.domain.defenderBody')}
      />

      <div className="flex flex-wrap justify-end gap-2">
        {secondary}
        <Button
          type="submit"
          variant="primary"
          loading={save.isPending}
          disabled={!unsaved && !allowUnchanged}
          leftIcon={<Save className="size-4" aria-hidden />}
        >
          {submitLabel ?? t('common.save')}
        </Button>
      </div>
    </form>
  )
}
