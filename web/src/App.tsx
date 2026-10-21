import { Suspense, lazy, useEffect } from 'react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MotionConfig } from 'motion/react'
import { BrowserRouter, Link, Navigate, Route, Routes, useLocation, useParams } from 'react-router-dom'
import { queryClient } from '@/lib/queryClient'
import { AuthProvider, useAuth } from '@/hooks/useAuth'
import { ToastProvider } from '@/hooks/useToast'
import { usePublicSettings, usePublicSettingsSync } from '@/hooks/usePublicSettings'
import { offerWizardOnce, useWizardStep } from '@/lib/wizard'
import { AppShell } from '@/components/AppShell'
import { LogoMark } from '@/components/Logo'
import { Button } from '@/components/ui/Button'
import { Login } from '@/pages/Login'
import { Overview } from '@/pages/Overview'
import { useT } from '@/i18n'

// Chart-heavy / secondary routes are split out of the initial bundle.
const Devices = lazy(() => import('@/pages/Devices').then((m) => ({ default: m.Devices })))
const MachineDetail = lazy(() => import('@/pages/MachineDetail').then((m) => ({ default: m.MachineDetail })))
const Users = lazy(() => import('@/pages/Users').then((m) => ({ default: m.Users })))
const UserDetail = lazy(() => import('@/pages/UserDetail').then((m) => ({ default: m.UserDetail })))
const Departments = lazy(() => import('@/pages/Departments').then((m) => ({ default: m.Departments })))
const Alerts = lazy(() => import('@/pages/Alerts').then((m) => ({ default: m.Alerts })))
const SlowMachines = lazy(() => import('@/pages/SlowMachines').then((m) => ({ default: m.SlowMachines })))
const Settings = lazy(() => import('@/pages/Settings').then((m) => ({ default: m.Settings })))
const Optimization = lazy(() => import('@/pages/Optimization').then((m) => ({ default: m.Optimization })))
const Setup = lazy(() => import('@/pages/Setup').then((m) => ({ default: m.Setup })))
const Profile = lazy(() => import('@/pages/Profile').then((m) => ({ default: m.Profile })))
const Enroll2FA = lazy(() => import('@/pages/Enroll2FA').then((m) => ({ default: m.Enroll2FA })))
const SetupWizard = lazy(() => import('@/pages/SetupWizard').then((m) => ({ default: m.SetupWizard })))

function FullScreenLoader() {
  const t = useT()
  return (
    <div className="grid min-h-screen place-items-center">
      <div className="flex flex-col items-center gap-4">
        <LogoMark size={48} animated />
        <p className="text-[13px] text-text-dim">{t('common.loading')}</p>
      </div>
    </div>
  )
}

function NotFound() {
  const t = useT()
  return (
    <div className="grid place-items-center py-24 text-center">
      <div>
        <p className="text-gradient tabular text-[64px] font-bold leading-none">404</p>
        <p className="mt-2 text-[16px] font-semibold text-text">{t('shell.notFoundTitle')}</p>
        <p className="mt-1 text-[14px] text-text-muted">{t('shell.notFoundBody')}</p>
        <Link to="/" className="mt-5 inline-block">
          <Button variant="primary">{t('shell.backHome')}</Button>
        </Link>
      </div>
    </div>
  )
}

// ── Legacy URLs ────────────────────────────────────────────────────────────────
// The panel used Turkish slugs (/cihazlar, /ayarlar?sekme=…) before it became
// bilingual. Bookmarks and the links in alert e-mails still point at them, so
// every old path redirects to its neutral English successor — query string and
// hash intact, and old parameter names/values translated.
const LEGACY_SETTINGS_TABS: Record<string, string> = {
  uygulamalar: 'removal',
  makineler: 'machines',
}

function LegacyRedirect({ to }: { to: string }) {
  const params = useParams()
  const location = useLocation()
  let pathname = to
  for (const [name, value] of Object.entries(params)) {
    if (name !== '*' && value !== undefined) pathname = pathname.replace(`:${name}`, encodeURIComponent(value))
  }

  const search = new URLSearchParams(location.search)
  const legacyTab = search.get('sekme')
  if (legacyTab !== null) {
    search.delete('sekme')
    if (!search.has('tab')) search.set('tab', LEGACY_SETTINGS_TABS[legacyTab] ?? legacyTab)
  }
  const qs = search.toString()

  return <Navigate to={{ pathname, search: qs ? `?${qs}` : '', hash: location.hash }} replace />
}

// "Apps to Remove" moved out of Settings into its own page. Old links
// (/settings?tab=removal, and /ayarlar?sekme=uygulamalar via LegacyRedirect) land
// on the Optimization page's app catalog; every other settings tab is untouched.
function SettingsRoute() {
  const location = useLocation()
  const search = new URLSearchParams(location.search)
  if (search.get('tab') === 'removal') {
    return <Navigate to={{ pathname: '/optimization', search: '?tab=apps', hash: location.hash }} replace />
  }
  return <Settings />
}

const LEGACY_ROUTES: [from: string, to: string][] = [
  ['/cihazlar', '/devices'],
  // Older still: the fleet list once lived at "/filo".
  ['/filo', '/devices'],
  ['/makineler/:id', '/machines/:id'],
  ['/yavas-makineler', '/slow-machines'],
  ['/kullanicilar', '/users'],
  ['/kullanicilar/:user', '/users/:user'],
  ['/departmanlar', '/departments'],
  ['/alarmlar', '/alerts'],
  ['/ayarlar', '/settings'],
  ['/kurulum', '/setup'],
]

function Gate() {
  const { status, user } = useAuth()
  const pub = usePublicSettings()
  const wizardStep = useWizardStep()
  usePublicSettingsSync()

  const offerWizard =
    status === 'authed' && user?.role === 'admin' && !user.mfa_enrollment_required && pub.data?.setup_complete === false
  useEffect(() => {
    if (offerWizard) offerWizardOnce()
  }, [offerWizard])

  if (status === 'loading') return <FullScreenLoader />

  // First-run wizard. Two ways in, one element so React keeps the same instance
  // when step 2 creates the admin and the session flips from anon to signed in:
  //   • nobody can sign in yet (needs_admin) — it replaces the login screen;
  //   • this browser is mid-wizard (lib/wizard.ts) and the user is an admin.
  // An installation that merely has no completed setup is sent here once per
  // browser (offerWizardOnce); after "finish later" it gets a banner instead.
  const enrolling = status === 'authed' && user?.mfa_enrollment_required === true
  const wizard =
    !enrolling &&
    (status === 'anon'
      ? pub.data?.needs_admin === true
      : wizardStep !== null && user?.role === 'admin')
  if (wizard) {
    return (
      <Suspense fallback={<FullScreenLoader />}>
        <SetupWizard />
      </Suspense>
    )
  }
  // Do not flash the login form on a fresh install while we still do not know.
  if (status === 'anon' && pub.isLoading) return <FullScreenLoader />

  // Signed out: show the login screen in place, whatever the URL. The address is
  // kept, so a deep link (e.g. from an alert e-mail) opens right after sign-in.
  if (status === 'anon') return <Login />

  // "Require 2FA" policy: the session is valid, but the API answers 403 to
  // everything except enrolment. Render ONLY the enrolment screen — no AppShell,
  // so no data queries and no WebSocket are mounted. The URL is left alone: once
  // /me stops reporting the flag, the page the user asked for renders here.
  if (user?.mfa_enrollment_required) {
    return (
      <Suspense fallback={<FullScreenLoader />}>
        <Enroll2FA />
      </Suspense>
    )
  }

  return (
    <Routes>
      <Route path="/login" element={<Navigate to="/" replace />} />
      {LEGACY_ROUTES.map(([from, to]) => (
        <Route key={from} path={from} element={<LegacyRedirect to={to} />} />
      ))}
      <Route element={<AppShell />}>
        <Route path="/" element={<Overview />} />
        <Route path="/devices" element={<Devices />} />
        <Route path="/machines/:id" element={<MachineDetail />} />
        <Route path="/slow-machines" element={<SlowMachines />} />
        <Route path="/users" element={<Users />} />
        <Route path="/users/:user" element={<UserDetail />} />
        <Route path="/departments" element={<Departments />} />
        <Route path="/alerts" element={<Alerts />} />
        <Route path="/optimization" element={<Optimization />} />
        <Route path="/settings" element={<SettingsRoute />} />
        <Route path="/setup" element={<Setup />} />
        <Route path="/profile" element={<Profile />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  )
}

export function App() {
  return (
    // reducedMotion="user": every motion component honours prefers-reduced-motion
    // (transforms are dropped, opacity fades stay) without each one opting in.
    <MotionConfig reducedMotion="user">
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <AuthProvider>
            <ToastProvider>
              <Gate />
            </ToastProvider>
          </AuthProvider>
        </BrowserRouter>
      </QueryClientProvider>
    </MotionConfig>
  )
}
