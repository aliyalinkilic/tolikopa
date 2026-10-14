import { useEffect, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { motion } from 'motion/react'
import { KeyRound, LogOut, Palette, RefreshCw, Save, ShieldCheck, ShieldOff, UserRound } from 'lucide-react'
import { PageHeader } from '@/components/AppShell'
import { Card, CardHeader } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Modal } from '@/components/ui/Modal'
import { LanguagePicker, ThemePicker } from '@/components/PrefControls'
import { PasswordRules, passwordAcceptable, passwordTooLong, MAX_PASSWORD_BYTES } from '@/components/PasswordRules'
import { RecoveryCodesPanel, TwoFactorSetup } from '@/components/TwoFactorSetup'
import {
  useChangePassword, useGeneralSettings, useRegenerateRecoveryCodes, useTotpDisable, useUpdateProfile,
} from '@/hooks/queries'
import { useAuth, useIsAdmin } from '@/hooks/useAuth'
import { usePublicSettings } from '@/hooks/usePublicSettings'
import { useToast } from '@/hooks/useToast'
import { errorCode, errorText } from '@/lib/errors'
import { formatDateTime, initialOf } from '@/lib/format'
import { fadeUp, staggerContainer } from '@/lib/motion'
import { useT } from '@/i18n'

export function Profile() {
  const t = useT()
  const { hash } = useLocation()

  // /profile#two-factor (linked from Settings → General) lands on that card.
  useEffect(() => {
    if (!hash) return
    document.getElementById(hash.slice(1))?.scrollIntoView({ block: 'start' })
  }, [hash])

  return (
    <div>
      <PageHeader title={t('profile.title')} description={t('profile.subtitle')} />
      <motion.div
        variants={staggerContainer()}
        initial="hidden"
        animate="show"
        className="grid items-start gap-4 lg:grid-cols-2"
      >
        <motion.div variants={fadeUp} className="space-y-4">
          <AccountCard />
          <PreferencesCard />
        </motion.div>
        <motion.div variants={fadeUp} className="space-y-4">
          <PasswordCard />
          <TwoFactorCard />
          <SessionCard />
        </motion.div>
      </motion.div>
    </div>
  )
}

function SectionIcon({ children }: { children: ReactNode }) {
  return (
    <span className="grid size-9 shrink-0 place-items-center rounded-[12px] bg-glass text-accent" aria-hidden>
      {children}
    </span>
  )
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-[11px] font-semibold uppercase tracking-[0.12em] text-text-dim">{label}</dt>
      <dd className="mt-1 truncate text-[14px] font-medium text-text">{children}</dd>
    </div>
  )
}

// ── Account ────────────────────────────────────────────────────────────────────
function AccountCard() {
  const t = useT()
  const { user, refresh } = useAuth()
  const { push } = useToast()
  const update = useUpdateProfile()
  const saved = user?.display_name ?? ''
  const [name, setName] = useState(saved)

  // Follow the account when /me is refetched (e.g. changed in another tab).
  useEffect(() => {
    setName(saved)
  }, [saved])

  if (!user) return null
  const dirty = name.trim() !== saved

  const save = (e: FormEvent) => {
    e.preventDefault()
    update.mutate(name.trim(), {
      onSuccess: async () => {
        await refresh()
        push({ title: t('profile.saved'), tone: 'ok' })
      },
      onError: (err) => push({ title: t('profile.saveFailed'), description: errorText(err), tone: 'crit' }),
    })
  }

  return (
    <Card>
      <div className="flex items-start gap-3">
        <SectionIcon>
          <UserRound className="size-[18px]" />
        </SectionIcon>
        <CardHeader title={t('profile.accountTitle')} description={t('profile.accountBody')} className="flex-1" />
      </div>

      <div className="mt-5 flex items-center gap-3">
        <span className="grid size-12 shrink-0 place-items-center rounded-full bg-accent-grad text-[18px] font-bold text-on-accent">
          {initialOf(user.display_name || user.username)}
        </span>
        <div className="min-w-0">
          <p className="truncate text-[16px] font-semibold text-text">{user.display_name || user.username}</p>
          <p className="truncate font-mono text-[12px] text-text-dim">{user.username}</p>
        </div>
      </div>

      <dl className="mt-5 grid grid-cols-2 gap-4">
        <Fact label={t('profile.username')}>{user.username}</Fact>
        <Fact label={t('profile.role')}>
          <Badge tone={user.role === 'admin' ? 'accent' : 'neutral'}>
            {t(user.role === 'admin' ? 'common.role.admin' : 'common.role.viewer')}
          </Badge>
        </Fact>
        <Fact label={t('profile.createdAt')}>{user.created_at ? formatDateTime(user.created_at) : '—'}</Fact>
        <Fact label={t('profile.lastLogin')}>
          {user.last_login_at ? formatDateTime(user.last_login_at) : t('profile.never')}
        </Fact>
      </dl>

      <form onSubmit={save} className="mt-5 flex flex-col gap-3 border-t border-border pt-5 sm:flex-row sm:items-start">
        <Input
          label={t('profile.displayName')}
          hint={t('profile.displayNameHint')}
          value={name}
          maxLength={80}
          onChange={(e) => setName(e.target.value)}
          autoComplete="name"
        />
        <Button
          type="submit"
          variant="primary"
          className="sm:mt-[26px]"
          disabled={!dirty}
          loading={update.isPending}
          leftIcon={<Save className="size-4" aria-hidden />}
        >
          {t('common.save')}
        </Button>
      </form>
    </Card>
  )
}

// ── Password ───────────────────────────────────────────────────────────────────
function PasswordCard() {
  const t = useT()
  const { push } = useToast()
  const change = useChangePassword()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [currentError, setCurrentError] = useState<string | null>(null)
  const [nextError, setNextError] = useState<string | null>(null)

  const tooLong = passwordTooLong(next)
  const ready = current !== '' && passwordAcceptable(next) && next === confirm && next !== current

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!ready) return
    setCurrentError(null)
    setNextError(null)
    change.mutate(
      { current_password: current, new_password: next },
      {
        onSuccess: () => {
          setCurrent('')
          setNext('')
          setConfirm('')
          push({ title: t('profile.passwordChanged'), description: t('twofa.otherSessionsNote'), tone: 'ok' })
        },
        onError: (err) => {
          // 400s here are field errors; the session stays valid.
          if (errorCode(err) === 'password_incorrect') setCurrentError(errorText(err))
          else setNextError(errorText(err))
        },
      },
    )
  }

  return (
    <Card>
      <div className="flex items-start gap-3">
        <SectionIcon>
          <KeyRound className="size-[18px]" />
        </SectionIcon>
        <CardHeader title={t('profile.passwordTitle')} description={t('profile.passwordBody')} className="flex-1" />
      </div>
      <form onSubmit={submit} className="mt-5 space-y-4">
        <Input
          label={t('profile.currentPassword')}
          type="password"
          autoComplete="current-password"
          value={current}
          error={currentError ?? undefined}
          onChange={(e) => {
            setCurrent(e.target.value)
            setCurrentError(null)
          }}
        />
        <Input
          label={t('profile.newPassword')}
          type="password"
          autoComplete="new-password"
          value={next}
          aria-describedby="profile-password-rules"
          error={nextError ?? (tooLong ? t('errors.password_too_long', { max: MAX_PASSWORD_BYTES }) : undefined)}
          onChange={(e) => {
            setNext(e.target.value)
            setNextError(null)
          }}
        />
        <Input
          label={t('profile.confirmPassword')}
          type="password"
          autoComplete="new-password"
          value={confirm}
          error={confirm !== '' && confirm !== next ? t('profile.mismatch') : undefined}
          onChange={(e) => setConfirm(e.target.value)}
        />
        <PasswordRules id="profile-password-rules" password={next} confirm={confirm} current={current} />
        <div className="flex justify-end">
          <Button type="submit" variant="primary" disabled={!ready} loading={change.isPending}>
            {t('profile.changePassword')}
          </Button>
        </div>
      </form>
    </Card>
  )
}

// ── Two-factor authentication ──────────────────────────────────────────────────
type TwoFactorDialog = 'setup' | 'disable' | 'regenerate' | null

function TwoFactorCard() {
  const t = useT()
  const { user, refresh } = useAuth()
  const isAdmin = useIsAdmin()
  const { push } = useToast()
  const disable = useTotpDisable()
  const regenerate = useRegenerateRecoveryCodes()
  const [dialog, setDialog] = useState<TwoFactorDialog>(null)
  const [newCodes, setNewCodes] = useState<string[] | null>(null)
  // The policy is only readable by admins (and optionally exposed publicly); for
  // everyone else it is learned from the server's refusal.
  const [refusedByPolicy, setRefusedByPolicy] = useState(false)
  const general = useGeneralSettings(isAdmin)
  const pub = usePublicSettings()

  if (!user) return null
  const enabled = user.totp_enabled === true
  const required = refusedByPolicy || general.data?.require_2fa === true || pub.data?.require_2fa === true

  const close = () => {
    setDialog(null)
    setNewCodes(null)
  }

  return (
    <Card id="two-factor" className="scroll-mt-24">
      <div className="flex items-start gap-3">
        <SectionIcon>
          <ShieldCheck className="size-[18px]" />
        </SectionIcon>
        <CardHeader
          title={t('twofa.title')}
          description={t('profile.twofaBody')}
          className="flex-1"
          action={
            <Badge tone={enabled ? 'ok' : 'neutral'} dot>
              {enabled ? t('profile.twofaOn') : t('profile.twofaOff')}
            </Badge>
          }
        />
      </div>

      <div className="mt-5 flex flex-wrap gap-2">
        {enabled ? (
          <>
            <Button leftIcon={<RefreshCw className="size-4" aria-hidden />} onClick={() => setDialog('regenerate')}>
              {t('profile.twofaRegenerate')}
            </Button>
            {!required && (
              <Button variant="danger" leftIcon={<ShieldOff className="size-4" aria-hidden />} onClick={() => setDialog('disable')}>
                {t('profile.twofaDisable')}
              </Button>
            )}
          </>
        ) : (
          <Button variant="primary" leftIcon={<ShieldCheck className="size-4" aria-hidden />} onClick={() => setDialog('setup')}>
            {t('profile.twofaEnable')}
          </Button>
        )}
      </div>
      {enabled && required && <p className="mt-3 text-[12px] text-text-dim">{t('profile.twofaPolicyNote')}</p>}

      <Modal open={dialog === 'setup'} onClose={close} title={t('profile.twofaSetupTitle')} size="lg">
        <div className="pb-5">
          {dialog === 'setup' && (
            <TwoFactorSetup
              username={user.username}
              onCancel={close}
              onDone={() => {
                close()
                push({ title: t('twofa.enabledToast'), description: t('twofa.otherSessionsNote'), tone: 'ok' })
                void refresh()
              }}
            />
          )}
        </div>
      </Modal>

      <ReauthDialog
        open={dialog === 'disable'}
        onClose={close}
        title={t('profile.disableTitle')}
        body={t('profile.disableBody')}
        confirmLabel={t('profile.disableConfirm')}
        danger
        loading={disable.isPending}
        onSubmit={(body, fail) =>
          disable.mutate(body, {
            onSuccess: () => {
              close()
              push({ title: t('profile.disabledToast'), tone: 'warn' })
              void refresh()
            },
            onError: (err) => {
              if (errorCode(err) === 'mfa_required_by_policy') {
                setRefusedByPolicy(true)
                close()
                push({ title: errorText(err), tone: 'warn' })
                return
              }
              fail(err)
            },
          })
        }
      />

      <ReauthDialog
        open={dialog === 'regenerate' && !newCodes}
        onClose={close}
        title={t('profile.regenerateTitle')}
        body={t('profile.regenerateBody')}
        confirmLabel={t('profile.regenerateConfirm')}
        loading={regenerate.isPending}
        onSubmit={(body, fail) =>
          regenerate.mutate(body, {
            onSuccess: (res) => setNewCodes(res.recovery_codes ?? []),
            onError: fail,
          })
        }
      />

      {/* No close affordance other than the acknowledged "Done": the codes are shown once. */}
      <Modal open={newCodes !== null} onClose={() => undefined} title={t('profile.regenerateTitle')} size="lg" dismissible={false}>
        <div className="pb-5">
          {newCodes && <RecoveryCodesPanel codes={newCodes} username={user.username} onDone={close} />}
        </div>
      </Modal>
    </Card>
  )
}

interface ReauthDialogProps {
  open: boolean
  onClose: () => void
  title: string
  body: string
  confirmLabel: string
  danger?: boolean
  loading: boolean
  /** `fail(err)` puts the server's answer on the right field. */
  onSubmit: (body: { password: string; code: string }, fail: (err: unknown) => void) => void
}

/** Password + current code, required again before a sensitive 2FA change. */
function ReauthDialog({ open, onClose, title, body, confirmLabel, danger, loading, onSubmit }: ReauthDialogProps) {
  const t = useT()
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [passwordError, setPasswordError] = useState<string | null>(null)
  const [codeError, setCodeError] = useState<string | null>(null)

  useEffect(() => {
    if (open) {
      setPassword('')
      setCode('')
      setPasswordError(null)
      setCodeError(null)
    }
  }, [open])

  const fail = (err: unknown) => {
    if (errorCode(err) === 'password_incorrect') setPasswordError(errorText(err))
    else setCodeError(errorText(err))
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!password || !code.trim() || loading) return
    setPasswordError(null)
    setCodeError(null)
    onSubmit({ password, code: code.trim() }, fail)
  }

  return (
    <Modal open={open} onClose={onClose} title={title} description={body} size="sm">
      <form onSubmit={submit} className="space-y-4 pb-5">
        <Input
          label={t('profile.currentPassword')}
          type="password"
          autoComplete="current-password"
          autoFocus
          value={password}
          error={passwordError ?? undefined}
          onChange={(e) => setPassword(e.target.value)}
        />
        <Input
          label={t('profile.codeOrRecovery')}
          autoComplete="one-time-code"
          autoCapitalize="none"
          spellCheck={false}
          value={code}
          error={codeError ?? undefined}
          onChange={(e) => setCode(e.target.value)}
          className="font-mono"
        />
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose} disabled={loading}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" variant={danger ? 'danger' : 'primary'} loading={loading} disabled={!password || !code.trim()}>
            {confirmLabel}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

// ── Preferences ────────────────────────────────────────────────────────────────
function PreferencesCard() {
  const t = useT()
  return (
    <Card>
      <div className="flex items-start gap-3">
        <SectionIcon>
          <Palette className="size-[18px]" />
        </SectionIcon>
        <CardHeader title={t('profile.prefsTitle')} description={t('profile.prefsBody')} className="flex-1" />
      </div>
      <div className="mt-5 space-y-5">
        <ThemePicker hint />
        <LanguagePicker hint />
      </div>
    </Card>
  )
}

// ── Session ────────────────────────────────────────────────────────────────────
function SessionCard() {
  const t = useT()
  const { logout } = useAuth()
  const { data } = usePublicSettings()
  return (
    <Card>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <h3 className="text-[15px] font-semibold text-text">{t('profile.sessionTitle')}</h3>
          <p className="mt-1 text-[13px] text-text-muted">{t('profile.sessionBody')}</p>
        </div>
        <Button leftIcon={<LogOut className="size-4" aria-hidden />} onClick={() => void logout()}>
          {t('shell.signOut')}
        </Button>
      </div>
      {data?.version && (
        <p className="mt-4 border-t border-border pt-3 text-[12px] text-text-dim">
          {t('profile.version')}: <span className="tabular font-mono">{data.version}</span>
        </p>
      )}
    </Card>
  )
}
