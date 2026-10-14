import { useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { Activity, ArrowLeft, KeyRound, Lock, ShieldCheck, User } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { LogoMark, useBrandSubtitle } from '@/components/Logo'
import { LanguagePicker, ThemePicker } from '@/components/PrefControls'
import { Input } from '@/components/ui/Input'
import { Button } from '@/components/ui/Button'
import { useAuth } from '@/hooks/useAuth'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { errorCode, errorText } from '@/lib/errors'
import { fadeUp, staggerContainer } from '@/lib/motion'
import { useT } from '@/i18n'
import type { TKey } from '@/i18n'
import { Trans } from '@/i18n/rich'

const FEATURES: { icon: LucideIcon; title: TKey; text: TKey }[] = [
  { icon: Activity, title: 'login.featLiveTitle', text: 'login.featLiveBody' },
  { icon: ShieldCheck, title: 'login.featPrivacyTitle', text: 'login.featPrivacyBody' },
  { icon: KeyRound, title: 'login.featDeployTitle', text: 'login.featDeployBody' },
]

type Step = { kind: 'password' } | { kind: 'mfa'; token: string; recovery: boolean }

export function Login() {
  const t = useT()
  const { login, verifyMfa } = useAuth()
  const brand = useBrandSubtitle()
  useDocumentTitle(t('nav.login'))

  const [step, setStep] = useState<Step>({ kind: 'password' })
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const onPassword = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)
    setLoading(true)
    try {
      const mfaToken = await login(username, password)
      if (mfaToken) {
        setPassword('')
        setCode('')
        setStep({ kind: 'mfa', token: mfaToken, recovery: false })
        setLoading(false)
      }
      // Signed in: the auth gate swaps this screen out.
    } catch (err) {
      setError(errorText(err, 'login.failed'))
      setLoading(false)
    }
  }

  const onMfa = async (e: FormEvent) => {
    e.preventDefault()
    if (step.kind !== 'mfa') return
    setError(null)
    setLoading(true)
    try {
      await verifyMfa(step.token, code.trim())
    } catch (err) {
      setLoading(false)
      setCode('')
      if (errorCode(err) === 'mfa_token_invalid') {
        // The 5-minute step token is gone: back to the password, and say why.
        setStep({ kind: 'password' })
        setError(t('login.mfaExpired'))
        return
      }
      setError(errorText(err, 'login.failed'))
    }
  }

  const backToPassword = () => {
    setStep({ kind: 'password' })
    setError(null)
    setCode('')
  }

  return (
    <div className="grid min-h-screen lg:grid-cols-[1.05fr_1fr]">
      {/* Brand panel */}
      <div className="relative hidden overflow-hidden border-r border-border bg-bg-elev/50 lg:block">
        <div className="absolute inset-0 -z-10">
          <div className="absolute -left-24 top-1/3 size-[520px] rounded-full bg-accent/10 blur-3xl" />
          <div className="absolute -right-10 bottom-0 size-[420px] rounded-full bg-accent-2/12 blur-3xl" />
        </div>
        {/* concentric sentinel rings — brand signature */}
        <svg className="absolute right-[-120px] top-1/2 -z-10 -translate-y-1/2 opacity-[0.14]" width="620" height="620" viewBox="0 0 620 620" fill="none" aria-hidden>
          {[300, 240, 180, 120, 60].map((r, i) => (
            <circle key={r} cx="310" cy="310" r={r} stroke="var(--color-accent)" strokeWidth={i === 0 ? 1.5 : 1} strokeDasharray={i % 2 ? '3 9' : undefined} />
          ))}
        </svg>

        <motion.div
          variants={staggerContainer(0.08, 0.1)}
          initial="hidden"
          animate="show"
          className="flex h-full flex-col justify-between p-12"
        >
          <motion.div variants={fadeUp} className="flex items-center gap-3">
            <LogoMark size={40} animated />
            <div className="min-w-0">
              <div className="text-[22px] font-bold tracking-[-0.02em] text-text">{t('common.appName')}</div>
              <div className="truncate text-[11px] font-medium uppercase tracking-[0.22em] text-text-dim">{brand}</div>
            </div>
          </motion.div>

          <div>
            <motion.h1 variants={fadeUp} className="max-w-md text-[34px] font-bold leading-[1.15] tracking-[-0.02em] text-text">
              <Trans k="login.heroTitle" tags={{ g: (c) => <span className="text-gradient">{c}</span> }} />
            </motion.h1>
            <motion.p variants={fadeUp} className="mt-3 max-w-md text-[15px] text-text-muted">
              {t('login.heroBody')}
            </motion.p>

            <div className="mt-8 space-y-4">
              {FEATURES.map((f) => (
                <motion.div key={f.title} variants={fadeUp} className="flex items-start gap-3">
                  <div className="grid size-9 shrink-0 place-items-center rounded-[12px] bg-glass text-accent">
                    <f.icon className="size-[18px]" aria-hidden />
                  </div>
                  <div>
                    <p className="text-[14px] font-semibold text-text">{t(f.title)}</p>
                    <p className="text-[13px] text-text-muted">{t(f.text)}</p>
                  </div>
                </motion.div>
              ))}
            </div>
          </div>

          <motion.p variants={fadeUp} className="text-[12px] text-text-dim">
            {t('login.footer', { app: t('common.appName'), tagline: brand })}
          </motion.p>
        </motion.div>
      </div>

      {/* Form */}
      <div className="flex min-h-screen flex-col p-6 sm:p-10">
        <div className="flex flex-wrap items-center justify-end gap-2">
          <LanguagePicker compact hideLabel />
          <ThemePicker compact hideLabel />
        </div>

        <div className="flex flex-1 items-center justify-center py-8">
          <div className="w-full max-w-sm">
            <div className="mb-8 flex items-center gap-3 lg:hidden">
              <LogoMark size={36} />
              <div className="min-w-0">
                <div className="text-[20px] font-bold tracking-[-0.02em] text-text">{t('common.appName')}</div>
                <div className="truncate text-[10px] font-medium uppercase tracking-[0.22em] text-text-dim">{brand}</div>
              </div>
            </div>

            <AnimatePresence mode="wait" initial={false}>
              {step.kind === 'password' ? (
                <StepPanel key="password" title={t('login.title')} subtitle={t('login.subtitle')}>
                  <form onSubmit={onPassword} className="mt-7 space-y-4">
                    <Input
                      label={t('login.username')}
                      autoComplete="username"
                      autoCapitalize="none"
                      spellCheck={false}
                      autoFocus
                      required
                      value={username}
                      onChange={(e) => setUsername(e.target.value)}
                      leftIcon={<User className="size-4" aria-hidden />}
                    />
                    <Input
                      label={t('login.password')}
                      type="password"
                      autoComplete="current-password"
                      required
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      leftIcon={<Lock className="size-4" aria-hidden />}
                    />
                    <ErrorNote message={error} />
                    <Button type="submit" variant="primary" size="lg" loading={loading} className="w-full">
                      {t('login.submit')}
                    </Button>
                  </form>
                </StepPanel>
              ) : (
                <StepPanel
                  key={step.recovery ? 'recovery' : 'totp'}
                  title={t('login.mfaTitle')}
                  subtitle={step.recovery ? t('login.mfaRecoverySubtitle') : t('login.mfaSubtitle')}
                >
                  <p className="mt-3 inline-flex max-w-full items-center gap-2 rounded-full border border-border-2 bg-surface/60 px-3 py-1 text-[12px] text-text-muted">
                    <User className="size-3.5 shrink-0" aria-hidden />
                    <span className="truncate">{t('login.signedInAs', { user: username })}</span>
                  </p>
                  <form onSubmit={onMfa} className="mt-5 space-y-4">
                    {step.recovery ? (
                      <Input
                        label={t('login.mfaRecoveryCode')}
                        autoComplete="off"
                        autoCapitalize="none"
                        spellCheck={false}
                        autoFocus
                        required
                        value={code}
                        onChange={(e) => setCode(e.target.value)}
                        leftIcon={<KeyRound className="size-4" aria-hidden />}
                        placeholder="xxxxx-xxxxx"
                        className="font-mono"
                      />
                    ) : (
                      <Input
                        label={t('login.mfaCode')}
                        inputMode="numeric"
                        autoComplete="one-time-code"
                        autoFocus
                        required
                        value={code}
                        onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                        placeholder="000000"
                        className="tabular text-center font-mono text-[20px] tracking-[0.35em]"
                      />
                    )}
                    <ErrorNote message={error} />
                    <Button
                      type="submit"
                      variant="primary"
                      size="lg"
                      loading={loading}
                      disabled={step.recovery ? code.trim() === '' : code.length !== 6}
                      className="w-full"
                    >
                      {t('login.mfaVerify')}
                    </Button>
                  </form>
                  <div className="mt-5 flex flex-col items-start gap-2 text-[13px]">
                    <button
                      type="button"
                      onClick={() => {
                        setStep({ ...step, recovery: !step.recovery })
                        setCode('')
                        setError(null)
                      }}
                      className="font-medium text-accent hover:underline ring-focus"
                    >
                      {step.recovery ? t('login.mfaUseApp') : t('login.mfaUseRecovery')}
                    </button>
                    <button
                      type="button"
                      onClick={backToPassword}
                      className="inline-flex items-center gap-1.5 text-text-muted hover:text-text ring-focus"
                    >
                      <ArrowLeft className="size-3.5" aria-hidden />
                      {t('login.mfaBack')}
                    </button>
                  </div>
                </StepPanel>
              )}
            </AnimatePresence>
          </div>
        </div>
      </div>
    </div>
  )
}

function StepPanel({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: -6 }}
      transition={{ duration: 0.2, ease: [0.22, 1, 0.36, 1] }}
    >
      <h2 className="text-[22px] font-bold tracking-[-0.01em] text-text">{title}</h2>
      <p className="mt-1 text-[14px] text-text-muted">{subtitle}</p>
      {children}
    </motion.div>
  )
}

function ErrorNote({ message }: { message: string | null }) {
  if (!message) return null
  return (
    <motion.div
      initial={{ opacity: 0, y: -4 }}
      animate={{ opacity: 1, y: 0 }}
      className="rounded-[var(--radius-md)] border border-crit/30 bg-crit/10 px-3 py-2 text-[13px] text-crit"
      role="alert"
    >
      {message}
    </motion.div>
  )
}
