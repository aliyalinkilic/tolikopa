import { useEffect, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { AnimatePresence, motion } from 'motion/react'
import {
  ArrowLeft, ArrowRight, Check, CheckCircle2, Loader2, LogOut, PartyPopper, ShieldCheck, Sparkles,
} from 'lucide-react'
import { LogoMark, Wordmark, useBrandSubtitle } from '@/components/Logo'
import { LanguagePicker, ThemePicker } from '@/components/PrefControls'
import { PasswordRules, passwordAcceptable } from '@/components/PasswordRules'
import { TwoFactorSetup } from '@/components/TwoFactorSetup'
import { ToastHost } from '@/components/Toast'
import { SkeletonText } from '@/components/Skeleton'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { DomainConfigForm, ServerUrlForm } from '@/components/setup/ConfigForms'
import {
  ArtifactInfo, CodeBlock, DownloadButton, ReadinessList, RequiredRights, RunInstructions, SETUP_TAGS, isReady,
} from '@/components/setup/parts'
import { useCompleteSetup, useSetupState } from '@/hooks/queries'
import { useAuth } from '@/hooks/useAuth'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { useToast } from '@/hooks/useToast'
import { api } from '@/lib/api'
import { errorCode, errorText } from '@/lib/errors'
import { WIZARD_STEPS, setWizardStep, useWizardStep } from '@/lib/wizard'
import type { WizardStep } from '@/lib/wizard'
import { cn } from '@/lib/cn'
import { useLang, useT } from '@/i18n'
import { Trans } from '@/i18n/rich'
import type { LoginResponse, SetupAdminRequest, SetupState } from '@/lib/types'

const USERNAME_RE = /^[\p{L}\p{N}._@-]{1,64}$/u
const POLL_MS = 4000

/** "abcd1234…" → "ABCD-1234-…" (16 characters, dashes inserted while typing). */
function formatSetupCode(raw: string): string {
  return raw
    .toUpperCase()
    .replace(/[^A-Z0-9]/g, '')
    .slice(0, 16)
    .replace(/(.{4})(?=.)/g, '$1-')
}

/** The furthest step the server state allows. */
function furthest(state: SetupState): WizardStep {
  if (!state.readiness.server_url) return 'server'
  if (!state.readiness.domain) return 'domain'
  return 'verify'
}

const idx = (s: WizardStep) => WIZARD_STEPS.indexOf(s)

/**
 * First-run setup. Shown instead of the login screen while no admin exists, and
 * again — for the admin who started it — until they finish or postpone. Steps 1–2
 * run without a session; from step 3 on the current step is derived from what the
 * server already has (address saved? domain saved?), with the browser's stored
 * step as a hint, so a reload lands where the admin left off.
 */
export function SetupWizard() {
  const t = useT()
  const { status, user, logout } = useAuth()
  const brand = useBrandSubtitle()
  const stored = useWizardStep()
  const navigate = useNavigate()
  const authed = status === 'authed'

  const [preStep, setPreStep] = useState<'welcome' | 'account'>('welcome')
  const [offer2fa, setOffer2fa] = useState(false)

  // Decide the step first (needs the stored hint only), then poll only on "verify".
  const hint: WizardStep | null = authed ? stored : null
  const setup = useSetupState({ enabled: authed, poll: authed && hint === 'verify' ? POLL_MS : false })

  let step: WizardStep
  if (!authed) step = preStep
  else if (offer2fa) step = 'account'
  else if (!setup.data) step = hint && idx(hint) >= idx('server') ? hint : 'server'
  else {
    const max = furthest(setup.data)
    const wanted = hint && idx(hint) >= idx('server') ? hint : max === 'verify' ? 'download' : max
    step = idx(wanted) > idx(max) ? max : wanted
  }

  useDocumentTitle(`${t('wizard.title')} — ${t(`wizard.steps.${step}`)}`)

  // A stale marker (setup was completed elsewhere): nothing left to do here.
  const completed = authed && !!setup.data?.completed_at
  useEffect(() => {
    if (completed) setWizardStep(null)
  }, [completed])

  const go = (next: WizardStep) => setWizardStep(next)
  const leave = () => {
    setWizardStep(null)
    navigate('/', { replace: true })
  }

  return (
    <div className="flex min-h-screen flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 px-5 py-4 sm:px-8">
        <div className="flex min-w-0 items-center gap-2.5">
          <LogoMark size={30} />
          <div className="min-w-0 leading-none">
            <Wordmark className="block text-[18px]" />
            <div className="mt-0.5 truncate text-[10px] font-medium uppercase tracking-[0.22em] text-text-dim">{brand}</div>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <LanguagePicker compact hideLabel />
          <ThemePicker compact hideLabel />
          {authed && !offer2fa && (
            <Button size="sm" variant="ghost" onClick={leave}>
              {t('wizard.finishLater')}
            </Button>
          )}
          {authed && (
            <Button size="sm" variant="ghost" leftIcon={<LogOut className="size-4" aria-hidden />} onClick={() => void logout()}>
              {t('shell.signOut')}
            </Button>
          )}
        </div>
      </header>

      <main className="mx-auto w-full max-w-5xl flex-1 px-4 pb-14 pt-2 sm:px-6">
        <Stepper current={step} />

        <AnimatePresence mode="wait" initial={false}>
          <motion.div
            key={offer2fa ? 'twofa' : step}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={{ duration: 0.2, ease: [0.22, 1, 0.36, 1] }}
            className="mt-6"
          >
            {step === 'welcome' && <WelcomeStep onStart={() => setPreStep('account')} />}

            {step === 'account' && !offer2fa && (
              <AccountStep
                onBack={() => setPreStep('welcome')}
                onCreated={() => {
                  setWizardStep('server')
                  setOffer2fa(true)
                }}
              />
            )}

            {step === 'account' && offer2fa && (
              <TwoFactorOffer username={user?.username ?? ''} onDone={() => setOffer2fa(false)} />
            )}

            {authed && !offer2fa && step !== 'account' && step !== 'welcome' && (
              <>
                {setup.isLoading ? (
                  <Panel>
                    <SkeletonText lines={6} />
                  </Panel>
                ) : !setup.data ? (
                  <Panel>
                    <p role="alert" className="text-[14px] text-crit">
                      {t('wizard.loadFailed')} {setup.error ? errorText(setup.error) : ''}
                    </p>
                    <div className="mt-4 flex justify-end gap-2">
                      <Button onClick={() => void setup.refetch()} loading={setup.isFetching}>
                        {t('common.retry')}
                      </Button>
                    </div>
                  </Panel>
                ) : (
                  <AuthedSteps
                    step={step}
                    state={setup.data}
                    refetch={() => void setup.refetch()}
                    refetching={setup.isFetching}
                    go={go}
                    leave={leave}
                  />
                )}
              </>
            )}
          </motion.div>
        </AnimatePresence>
      </main>

      <ToastHost />
    </div>
  )
}

// ── Chrome ─────────────────────────────────────────────────────────────────────
function Stepper({ current }: { current: WizardStep }) {
  const t = useT()
  const at = idx(current)
  return (
    <nav aria-label={t('wizard.progress')}>
      <p className="mb-2 text-[12px] font-medium text-text-dim sm:hidden">
        {t('wizard.stepOf', { n: at + 1, total: WIZARD_STEPS.length })} · <span className="text-text">{t(`wizard.steps.${current}`)}</span>
      </p>
      <ol className="flex items-center gap-1.5 sm:gap-2">
        {WIZARD_STEPS.map((s, i) => {
          const done = i < at
          const active = i === at
          return (
            <li key={s} className="flex min-w-0 flex-1 flex-col gap-2" aria-current={active ? 'step' : undefined}>
              <span
                className={cn('h-1 rounded-full transition-colors', done || active ? 'bg-accent-grad' : 'bg-wash-2', active && 'opacity-100', done && 'opacity-60')}
                aria-hidden
              />
              <span className="hidden items-center gap-1.5 sm:flex">
                <span
                  className={cn(
                    'tabular grid size-5 shrink-0 place-items-center rounded-full text-[11px] font-bold',
                    done ? 'bg-ok/15 text-ok' : active ? 'bg-accent-grad text-on-accent' : 'bg-wash-2 text-text-dim',
                  )}
                >
                  {done ? <Check className="size-3" aria-hidden /> : i + 1}
                </span>
                <span className={cn('truncate text-[12px] font-medium', active ? 'text-text' : 'text-text-dim')}>
                  {t(`wizard.steps.${s}`)}
                </span>
              </span>
            </li>
          )
        })}
      </ol>
    </nav>
  )
}

function Panel({ children, className }: { children: ReactNode; className?: string }) {
  return <section className={cn('card p-6 sm:p-8', className)}>{children}</section>
}

function StepHeading({ title, body }: { title: string; body?: ReactNode }) {
  return (
    <div className="mb-6">
      <h1 className="text-[22px] font-bold tracking-[-0.01em] text-text">{title}</h1>
      {body && <p className="mt-1.5 max-w-2xl text-[14px] text-text-muted">{body}</p>}
    </div>
  )
}

function BackButton({ onClick }: { onClick: () => void }) {
  const t = useT()
  return (
    <Button variant="ghost" onClick={onClick} leftIcon={<ArrowLeft className="size-4" aria-hidden />}>
      {t('wizard.back')}
    </Button>
  )
}

// ── 1. Welcome ─────────────────────────────────────────────────────────────────
function WelcomeStep({ onStart }: { onStart: () => void }) {
  const t = useT()
  return (
    <Panel className="mx-auto max-w-2xl">
      <div className="mb-5 grid size-12 place-items-center rounded-[16px] bg-accent-grad-soft text-accent">
        <Sparkles className="size-6" aria-hidden />
      </div>
      <StepHeading title={t('wizard.welcome.title')} body={t('wizard.welcome.body')} />
      <ol className="space-y-3">
        {(['point1', 'point2', 'point3'] as const).map((k, i) => (
          <li key={k} className="flex items-start gap-3 text-[14px] text-text-muted">
            <span className="tabular grid size-6 shrink-0 place-items-center rounded-full bg-accent-grad-soft text-[12px] font-bold text-accent">
              {i + 1}
            </span>
            <span>{t(`wizard.welcome.${k}`)}</span>
          </li>
        ))}
      </ol>
      <p className="mt-5 flex items-start gap-2.5 rounded-[var(--radius-md)] border border-ok/25 bg-ok/[0.07] p-3 text-[13px] text-text-muted">
        <ShieldCheck className="mt-0.5 size-4 shrink-0 text-ok" aria-hidden />
        {t('wizard.welcome.privacy')}
      </p>
      <div className="mt-6">
        <LanguagePicker />
      </div>
      <div className="mt-6 flex justify-end">
        <Button variant="primary" size="lg" onClick={onStart}>
          {t('wizard.welcome.start')}
          <ArrowRight className="size-4" aria-hidden />
        </Button>
      </div>
    </Panel>
  )
}

// ── 2. Setup code + first admin ────────────────────────────────────────────────
type AccountField = 'code' | 'username' | 'display_name' | 'password'

function AccountStep({ onBack, onCreated }: { onBack: () => void; onCreated: () => void }) {
  const t = useT()
  const lang = useLang()
  const qc = useQueryClient()
  const { startSession } = useAuth()
  const [code, setCode] = useState('')
  const [username, setUsername] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [errors, setErrors] = useState<Partial<Record<AccountField, string>>>({})
  const [formError, setFormError] = useState<string | null>(null)
  const [alreadyDone, setAlreadyDone] = useState(false)
  const [busy, setBusy] = useState(false)

  const codeOk = code.replace(/-/g, '').length === 16
  const usernameOk = USERNAME_RE.test(username.trim())
  const ready = codeOk && usernameOk && passwordAcceptable(password) && password === confirm

  const clear = (field: AccountField) => setErrors((e) => ({ ...e, [field]: undefined }))

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!ready || busy) return
    setErrors({})
    setFormError(null)
    setBusy(true)
    const body: SetupAdminRequest = {
      setup_code: code,
      username: username.trim(),
      password,
      lang,
      ...(displayName.trim() ? { display_name: displayName.trim() } : {}),
    }
    try {
      const res = await api.post<LoginResponse>('/api/setup/admin', body)
      onCreated() // mark the wizard before the session flips the gate
      await startSession(res)
      void qc.invalidateQueries({ queryKey: ['public-settings'] })
    } catch (err) {
      setBusy(false)
      const c = errorCode(err)
      const text = errorText(err)
      if (c === 'setup_code_invalid') setErrors({ code: text })
      else if (c === 'password_too_weak' || c === 'password_too_long') setErrors({ password: text })
      else if (c === 'setup_already_done') setAlreadyDone(true)
      else if (c === 'invalid_request') {
        const field = (err as { params?: { field?: unknown } }).params?.field
        if (field === 'username') setErrors({ username: t('settings.users.usernameInvalid') })
        else if (field === 'display_name') setErrors({ display_name: text })
        else setFormError(text)
      } else setFormError(text)
    }
  }

  if (alreadyDone) {
    return (
      <Panel className="mx-auto max-w-2xl">
        <StepHeading title={t('wizard.account.title')} />
        <p role="alert" className="rounded-[var(--radius-md)] border border-warn/30 bg-warn/10 px-3 py-2 text-[13px] text-warn">
          {t('wizard.account.alreadyDone')}
        </p>
        <div className="mt-5 flex justify-end">
          {/* Refetching the public settings clears needs_admin, and the gate shows the login screen. */}
          <Button variant="primary" onClick={() => void qc.invalidateQueries({ queryKey: ['public-settings'] })}>
            {t('wizard.account.goLogin')}
          </Button>
        </div>
      </Panel>
    )
  }

  return (
    <Panel className="mx-auto max-w-2xl">
      <StepHeading title={t('wizard.account.title')} body={t('wizard.account.body')} />
      <form onSubmit={(e) => void submit(e)} className="space-y-4" noValidate>
        <div>
          <Input
            label={t('wizard.account.code')}
            value={code}
            error={errors.code}
            onChange={(e) => {
              setCode(formatSetupCode(e.target.value))
              clear('code')
            }}
            placeholder="XXXX-XXXX-XXXX-XXXX"
            autoComplete="off"
            autoCapitalize="characters"
            spellCheck={false}
            autoFocus
            aria-describedby="setup-code-hint"
            className="tabular font-mono text-[16px] tracking-[0.12em]"
          />
          <p id="setup-code-hint" className="mt-1.5 text-[12px] text-text-dim">
            <Trans k="wizard.account.codeHint" tags={SETUP_TAGS} />
          </p>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <Input
            label={t('wizard.account.username')}
            hint={t('wizard.account.usernameHint')}
            error={errors.username ?? (username.trim() !== '' && !usernameOk ? t('settings.users.usernameInvalid') : undefined)}
            value={username}
            maxLength={64}
            onChange={(e) => {
              setUsername(e.target.value)
              clear('username')
            }}
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
          />
          <Input
            label={t('wizard.account.displayName')}
            hint={t('wizard.account.optional')}
            error={errors.display_name}
            value={displayName}
            maxLength={80}
            onChange={(e) => {
              setDisplayName(e.target.value)
              clear('display_name')
            }}
            autoComplete="name"
          />
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <Input
            label={t('wizard.account.password')}
            type="password"
            error={errors.password}
            value={password}
            onChange={(e) => {
              setPassword(e.target.value)
              clear('password')
            }}
            autoComplete="new-password"
          />
          <Input
            label={t('wizard.account.confirm')}
            type="password"
            error={confirm !== '' && confirm !== password ? t('wizard.account.mismatch') : undefined}
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            autoComplete="new-password"
          />
        </div>
        <PasswordRules password={password} confirm={confirm} />

        {formError && (
          <p role="alert" className="rounded-[var(--radius-md)] border border-crit/30 bg-crit/10 px-3 py-2 text-[13px] text-crit">
            {formError}
          </p>
        )}

        <div className="flex flex-wrap justify-between gap-2 pt-1">
          <BackButton onClick={onBack} />
          <Button type="submit" variant="primary" loading={busy} disabled={!ready}>
            {t('wizard.account.create')}
          </Button>
        </div>
      </form>
    </Panel>
  )
}

function TwoFactorOffer({ username, onDone }: { username: string; onDone: () => void }) {
  const t = useT()
  const { refresh } = useAuth()
  const { push } = useToast()
  const [started, setStarted] = useState(false)
  return (
    <Panel className="mx-auto max-w-2xl">
      <StepHeading title={t('wizard.twofa.title')} body={t('wizard.twofa.body')} />
      {started ? (
        <TwoFactorSetup
          username={username}
          onCancel={onDone}
          onDone={() => {
            push({ title: t('twofa.enabledToast'), tone: 'ok' })
            void refresh()
            onDone()
          }}
        />
      ) : (
        <div className="flex flex-wrap justify-end gap-2">
          <Button variant="ghost" onClick={onDone}>
            {t('wizard.twofa.skip')}
          </Button>
          <Button variant="primary" onClick={() => setStarted(true)} leftIcon={<ShieldCheck className="size-4" aria-hidden />}>
            {t('wizard.twofa.enable')}
          </Button>
        </div>
      )}
    </Panel>
  )
}

// ── 3–6 (signed in) ────────────────────────────────────────────────────────────
function AuthedSteps({
  step,
  state,
  refetch,
  refetching,
  go,
  leave,
}: {
  step: WizardStep
  state: SetupState
  refetch: () => void
  refetching: boolean
  go: (s: WizardStep) => void
  leave: () => void
}) {
  const t = useT()
  const { push } = useToast()
  const complete = useCompleteSetup()
  const ready = isReady(state.readiness)
  const total = state.fleet?.total ?? 0

  if (step === 'server') {
    return (
      <Panel className="mx-auto max-w-2xl">
        <StepHeading title={t('setup.server.title')} body={t('setup.server.body')} />
        <ServerUrlForm
          config={state.config}
          candidates={state.server_url_candidates ?? []}
          submitLabel={t('wizard.saveAndNext')}
          allowUnchanged
          onSaved={() => go('domain')}
        />
      </Panel>
    )
  }

  if (step === 'domain') {
    return (
      <div className="grid items-start gap-4 lg:grid-cols-[1.55fr_1fr]">
        <Panel>
          <StepHeading title={t('setup.domain.title')} body={t('setup.domain.body')} />
          <DomainConfigForm
            config={state.config}
            submitLabel={t('wizard.saveAndNext')}
            allowUnchanged
            secondary={<BackButton onClick={() => go('server')} />}
            onSaved={() => go('download')}
          />
        </Panel>
        <RequiredRights />
      </div>
    )
  }

  if (step === 'download') {
    return (
      <div className="grid items-start gap-4 lg:grid-cols-[1.55fr_1fr]">
        <Panel>
          <StepHeading title={t('setup.run.title')} body={t('setup.run.body')} />
          <div className="space-y-6">
            <ReadinessList readiness={state.readiness} onRefresh={refetch} refreshing={refetching} />
            <div>
              <h3 className="mb-2 text-[14px] font-semibold text-text">{t('setup.status.agentBuild')}</h3>
              <ArtifactInfo artifacts={state.artifacts} />
            </div>
            <DownloadButton file="tolikopa-setup.zip" variant="primary" size="lg" disabled={!ready} className="w-full sm:w-auto">
              {t('setup.run.download')}
            </DownloadButton>
            <RunInstructions />
          </div>
          <div className="mt-7 flex flex-wrap justify-between gap-2 border-t border-border pt-5">
            <BackButton onClick={() => go('domain')} />
            <Button variant="primary" onClick={() => go('verify')}>
              {t('wizard.next')}
              <ArrowRight className="size-4" aria-hidden />
            </Button>
          </div>
        </Panel>
        <RequiredRights />
      </div>
    )
  }

  // verify
  const finish = () =>
    complete.mutate(undefined, {
      onSuccess: () => {
        push({ title: t('wizard.finished'), tone: 'ok' })
        leave()
      },
      // 409 setup_incomplete names what is missing (errorText spells it out).
      onError: (err) => push({ title: t('wizard.finishFailed'), description: errorText(err), tone: 'crit' }),
    })

  return (
    <Panel className="mx-auto max-w-2xl">
      <StepHeading title={t('wizard.verify.title')} body={t('wizard.verify.body')} />
      <CodeBlock code={'gpupdate /force\nStart-ScheduledTask -TaskName TolikopaAgent'} />

      <div
        role="status"
        aria-live="polite"
        className={cn(
          'mt-5 flex items-start gap-3 rounded-[var(--radius-lg)] border p-4',
          total > 0 ? 'border-ok/30 bg-ok/[0.08]' : 'border-border bg-surface-2/40',
        )}
      >
        {total > 0 ? (
          <span className="grid size-10 shrink-0 place-items-center rounded-full bg-ok/15 text-ok">
            <PartyPopper className="size-5" aria-hidden />
          </span>
        ) : (
          <span className="grid size-10 shrink-0 place-items-center rounded-full bg-wash-2 text-text-muted">
            <Loader2 className="size-5 animate-spin" aria-hidden />
          </span>
        )}
        <div className="min-w-0">
          <p className="text-[15px] font-semibold text-text">
            {total > 0 ? t('wizard.verify.found', { count: total }) : t('wizard.verify.waiting')}
          </p>
          <p className="mt-0.5 text-[13px] text-text-muted">
            {total > 0 ? t('wizard.verify.foundBody') : t('wizard.verify.waitingBody')}
          </p>
        </div>
      </div>

      {total === 0 && (
        <p className="mt-3 text-[12px] text-text-dim">
          <Trans k="wizard.verify.logHint" tags={SETUP_TAGS} />
        </p>
      )}

      <div className="mt-7 flex flex-wrap justify-between gap-2 border-t border-border pt-5">
        <BackButton onClick={() => go('download')} />
        <div className="flex flex-wrap gap-2">
          <Button variant="ghost" onClick={leave}>
            {t('wizard.finishLater')}
          </Button>
          <Button
            variant={total > 0 ? 'primary' : 'secondary'}
            loading={complete.isPending}
            onClick={finish}
            leftIcon={<CheckCircle2 className="size-4" aria-hidden />}
          >
            {t('wizard.finish')}
          </Button>
        </div>
      </div>
    </Panel>
  )
}
