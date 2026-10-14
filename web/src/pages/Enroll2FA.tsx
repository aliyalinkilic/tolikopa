import { useState } from 'react'
import { LogOut, ShieldCheck, User } from 'lucide-react'
import { LogoMark, Wordmark, useBrandSubtitle } from '@/components/Logo'
import { LanguagePicker, ThemePicker } from '@/components/PrefControls'
import { TwoFactorSetup } from '@/components/TwoFactorSetup'
import { ToastHost } from '@/components/Toast'
import { Button } from '@/components/ui/Button'
import { useAuth } from '@/hooks/useAuth'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { useToast } from '@/hooks/useToast'
import { useT } from '@/i18n'

/**
 * Mandatory 2FA enrolment (the "require 2FA" policy). The session is real, but the
 * API refuses everything except the enrolment calls, so nothing else of the app is
 * mounted here: no sidebar, no data queries, no WebSocket. When the recovery codes
 * are acknowledged, /me is refetched; the route guard lifts and whatever URL the
 * user came for renders in place.
 */
export function Enroll2FA() {
  const t = useT()
  const { user, logout, refresh } = useAuth()
  const { push } = useToast()
  const brand = useBrandSubtitle()
  const [started, setStarted] = useState(false)
  useDocumentTitle(t('enroll.title'))

  const username = user?.username ?? ''

  const finish = async () => {
    push({ title: t('twofa.enabledToast'), tone: 'ok' })
    await refresh()
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
          <Button size="sm" variant="ghost" leftIcon={<LogOut className="size-4" aria-hidden />} onClick={() => void logout()}>
            {t('shell.signOut')}
          </Button>
        </div>
      </header>

      <main className="flex flex-1 items-center justify-center px-4 pb-12 pt-4">
        <div className="card w-full max-w-xl p-6 sm:p-8">
          <div className="flex items-start gap-3">
            <div className="grid size-11 shrink-0 place-items-center rounded-[14px] bg-accent-grad-soft text-accent">
              <ShieldCheck className="size-5" aria-hidden />
            </div>
            <div className="min-w-0">
              <h1 className="text-[22px] font-bold tracking-[-0.01em] text-text">{t('enroll.title')}</h1>
              <p className="mt-1.5 text-[14px] text-text-muted">{t('enroll.body')}</p>
              <p className="mt-3 inline-flex max-w-full items-center gap-2 rounded-full border border-border-2 bg-surface-2/60 px-3 py-1 text-[12px] text-text-muted">
                <User className="size-3.5 shrink-0" aria-hidden />
                <span className="truncate">{t('enroll.signedInAs', { user: username })}</span>
              </p>
            </div>
          </div>

          <div className="mt-6 border-t border-border pt-6">
            {started ? (
              <TwoFactorSetup username={username} onDone={() => void finish()} />
            ) : (
              <div className="flex justify-end">
                <Button variant="primary" size="lg" onClick={() => setStarted(true)}>
                  {t('enroll.start')}
                </Button>
              </div>
            )}
          </div>
        </div>
      </main>

      <ToastHost />
    </div>
  )
}
