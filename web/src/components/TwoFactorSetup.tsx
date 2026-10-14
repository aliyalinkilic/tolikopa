import { useState } from 'react'
import type { FormEvent } from 'react'
import { Check, Copy, Download, KeyRound, LockKeyhole, ShieldCheck } from 'lucide-react'
import { Button } from './ui/Button'
import { Input } from './ui/Input'
import { Checkbox } from './ui/Checkbox'
import { QrCode } from './QrCode'
import { useTotpEnable, useTotpSetup } from '@/hooks/queries'
import { useToast } from '@/hooks/useToast'
import { errorText } from '@/lib/errors'
import { ApiError } from '@/lib/api'
import { useT } from '@/i18n'
import type { TotpSetup } from '@/lib/types'

/** Copies text and flips `copied` for a moment; reports failure instead of lying. */
export function useCopyText() {
  const t = useT()
  const { push } = useToast()
  const [copied, setCopied] = useState(false)
  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1600)
    } catch {
      push({ title: t('ui.copy.failed'), tone: 'warn' })
    }
  }
  return { copied, copy }
}

/** Groups a base32 secret in fours so it can be typed without losing one's place. */
function groupSecret(secret: string): string {
  return secret.replace(/(.{4})/g, '$1 ').trim()
}

interface RecoveryCodesProps {
  codes: string[]
  username: string
  onDone: () => void
  doneLabel?: string
}

/**
 * Shows recovery codes exactly once: copy, download as .txt, and an explicit
 * "I saved them" tick before the only way out unlocks.
 */
export function RecoveryCodesPanel({ codes, username, onDone, doneLabel }: RecoveryCodesProps) {
  const t = useT()
  const { copied, copy } = useCopyText()
  const [saved, setSaved] = useState(false)

  const asText = () =>
    [t('twofa.codesFileTitle', { user: username }), t('twofa.codesFileNote'), '', ...codes, ''].join('\r\n')

  const download = () => {
    const url = URL.createObjectURL(new Blob([asText()], { type: 'text/plain;charset=utf-8' }))
    const a = document.createElement('a')
    a.href = url
    a.download = `tolikopa-recovery-codes-${username}.txt`
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="space-y-4">
      <div className="flex items-start gap-3">
        <div className="grid size-9 shrink-0 place-items-center rounded-[12px] bg-ok/12 text-ok">
          <KeyRound className="size-[18px]" aria-hidden />
        </div>
        <div className="min-w-0">
          <h3 className="text-[15px] font-semibold text-text">{t('twofa.stepCodes')}</h3>
          <p className="mt-1 text-[13px] text-text-muted">{t('twofa.stepCodesBody')}</p>
        </div>
      </div>

      <ul className="grid grid-cols-2 gap-2 rounded-[var(--radius-md)] border border-border bg-code p-3">
        {codes.map((c) => (
          <li key={c} className="tabular select-all rounded-[8px] px-2 py-1 text-center font-mono text-[14px] text-text">
            {c}
          </li>
        ))}
      </ul>

      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          leftIcon={copied ? <Check className="size-4 text-ok" aria-hidden /> : <Copy className="size-4" aria-hidden />}
          onClick={() => void copy(codes.join('\n'))}
        >
          {copied ? t('common.copied') : t('twofa.copyCodes')}
        </Button>
        <Button size="sm" leftIcon={<Download className="size-4" aria-hidden />} onClick={download}>
          {t('twofa.downloadCodes')}
        </Button>
      </div>

      <Checkbox checked={saved} onChange={setSaved} label={t('twofa.savedAck')} />

      <div className="flex justify-end">
        <Button variant="primary" disabled={!saved} onClick={onDone}>
          {doneLabel ?? t('twofa.done')}
        </Button>
      </div>
    </div>
  )
}

interface TwoFactorSetupProps {
  username: string
  /** Called after the recovery codes were acknowledged. 2FA is already on by then. */
  onDone: () => void
  onCancel?: () => void
}

/**
 * Enrolment: password → QR (+ the secret as text for manual entry) → 6-digit
 * code → recovery codes shown once. The server hands out the secret only against
 * the password, so a stolen session alone cannot enrol someone else's phone. Used by the profile dialog and by the full-screen
 * enrolment page the "require 2FA" policy sends people to.
 */
export function TwoFactorSetup({ username, onDone, onCancel }: TwoFactorSetupProps) {
  const t = useT()
  const setup = useTotpSetup()
  const enable = useTotpEnable()
  const { copied, copy } = useCopyText()
  const [password, setPassword] = useState('')
  const [passwordError, setPasswordError] = useState<string | null>(null)
  const [pending, setPending] = useState<TotpSetup | null>(null)
  const [setupError, setSetupError] = useState<string | null>(null)
  const [code, setCode] = useState('')
  const [codeError, setCodeError] = useState<string | null>(null)
  const [codes, setCodes] = useState<string[] | null>(null)

  const start = (e: FormEvent) => {
    e.preventDefault()
    if (!password) return
    setPasswordError(null)
    setSetupError(null)
    setup.mutate(password, {
      onSuccess: (res) => {
        setPassword('')
        setPending(res)
      },
      onError: (err) => {
        if (err instanceof ApiError && err.code === 'password_incorrect') setPasswordError(errorText(err))
        else setSetupError(errorText(err))
      },
    })
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (code.length !== 6) return
    setCodeError(null)
    enable.mutate(code, {
      onSuccess: (res) => setCodes(res.recovery_codes ?? []),
      onError: (err) => {
        setCodeError(errorText(err))
        setCode('')
      },
    })
  }

  if (codes) {
    return <RecoveryCodesPanel codes={codes} username={username} onDone={onDone} />
  }

  if (setupError) {
    return (
      <div className="space-y-3">
        <p role="alert" className="rounded-[var(--radius-md)] border border-crit/30 bg-crit/10 px-3 py-2 text-[13px] text-crit">
          {t('twofa.setupFailed')} {setupError}
        </p>
        <div className="flex justify-end gap-2">
          {onCancel && (
            <Button variant="ghost" onClick={onCancel}>
              {t('common.cancel')}
            </Button>
          )}
          <Button onClick={() => setSetupError(null)}>{t('common.retry')}</Button>
        </div>
      </div>
    )
  }

  if (!pending) {
    return (
      <form onSubmit={start} className="space-y-4">
        <div className="flex items-start gap-3">
          <div className="grid size-9 shrink-0 place-items-center rounded-[12px] bg-accent/12 text-accent">
            <LockKeyhole className="size-[18px]" aria-hidden />
          </div>
          <div className="min-w-0">
            <h3 className="text-[15px] font-semibold text-text">{t('twofa.stepPassword')}</h3>
            <p className="mt-1 text-[13px] text-text-muted">{t('twofa.stepPasswordBody')}</p>
          </div>
        </div>
        <Input
          label={t('twofa.passwordLabel')}
          type="password"
          autoComplete="current-password"
          autoFocus
          value={password}
          error={passwordError ?? undefined}
          onChange={(e) => {
            setPassword(e.target.value)
            setPasswordError(null)
          }}
        />
        <div className="flex justify-end gap-2">
          {onCancel && (
            <Button variant="ghost" onClick={onCancel} disabled={setup.isPending}>
              {t('common.cancel')}
            </Button>
          )}
          <Button type="submit" variant="primary" loading={setup.isPending} disabled={!password}>
            {t('twofa.continue')}
          </Button>
        </div>
      </form>
    )
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <div className="flex items-start gap-3">
        <div className="grid size-9 shrink-0 place-items-center rounded-[12px] bg-accent/12 text-accent">
          <ShieldCheck className="size-[18px]" aria-hidden />
        </div>
        <div className="min-w-0">
          <h3 className="text-[15px] font-semibold text-text">{t('twofa.stepScan')}</h3>
          <p className="mt-1 text-[13px] text-text-muted">{t('twofa.stepScanBody')}</p>
        </div>
      </div>

      <div className="flex flex-col items-center gap-4 sm:flex-row sm:items-start">
        <QrCode value={pending.otpauth_url} label={t('twofa.qrAlt')} />
        <div className="min-w-0 flex-1 space-y-2">
          <p className="text-[12px] text-text-muted">{t('twofa.manualLabel')}</p>
          <div className="flex items-start gap-2">
            <code className="tabular min-w-0 flex-1 select-all break-words rounded-[var(--radius-sm)] border border-border bg-code px-2.5 py-2 font-mono text-[13px] text-text">
              {groupSecret(pending.secret)}
            </code>
            <Button
              size="icon"
              aria-label={copied ? t('twofa.secretCopied') : t('twofa.copySecret')}
              onClick={() => void copy(pending.secret)}
            >
              {copied ? <Check className="size-4 text-ok" aria-hidden /> : <Copy className="size-4" aria-hidden />}
            </Button>
          </div>
        </div>
      </div>

      <Input
        label={t('twofa.codeLabel')}
        hint={t('twofa.codeHint')}
        error={codeError ?? undefined}
        value={code}
        onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
        inputMode="numeric"
        autoComplete="one-time-code"
        placeholder="000000"
        className="tabular max-w-[11rem] text-center font-mono text-[18px] tracking-[0.3em]"
      />

      <div className="flex justify-end gap-2">
        {onCancel && (
          <Button variant="ghost" onClick={onCancel} disabled={enable.isPending}>
            {t('common.cancel')}
          </Button>
        )}
        <Button type="submit" variant="primary" loading={enable.isPending} disabled={code.length !== 6}>
          {t('twofa.enable')}
        </Button>
      </div>
    </form>
  )
}
