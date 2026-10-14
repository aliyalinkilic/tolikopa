import { Suspense, useEffect, useRef, useState } from 'react'
import type { CSSProperties, FormEvent, ReactNode } from 'react'
import { Link, NavLink, useLocation, useNavigate, useOutlet } from 'react-router-dom'
import { AnimatePresence, motion } from 'motion/react'
import {
  BellRing, Building2, Gauge, LayoutDashboard, LogOut, Menu, Monitor, PanelLeft, Rocket, Search, Settings, UserRound,
  TriangleAlert, Users, X,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { Logo, LogoMark } from './Logo'
import { ToastHost } from './Toast'
import { Badge } from './ui/Badge'
import { useAuth, useIsAdmin } from '@/hooks/useAuth'
import { useAlertsStream } from '@/hooks/useAlertsStream'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { usePublicSettings } from '@/hooks/usePublicSettings'
import { useWsStatus } from '@/hooks/useWsStatus'
import { useFleetSummary, useSetupState } from '@/hooks/queries'
import { routeVariants } from '@/lib/motion'
import { displayUser, initialOf } from '@/lib/format'
import { cn } from '@/lib/cn'
import { useLang, useT } from '@/i18n'
import type { TKey } from '@/i18n'

interface NavItem {
  to: string
  label: TKey
  icon: LucideIcon
  adminOnly?: boolean
  end?: boolean
}

const NAV: NavItem[] = [
  { to: '/', label: 'nav.overview', icon: LayoutDashboard, end: true },
  { to: '/devices', label: 'nav.devices', icon: Monitor },
  { to: '/slow-machines', label: 'nav.slowMachines', icon: Gauge },
  { to: '/users', label: 'nav.users', icon: Users },
  { to: '/departments', label: 'nav.departments', icon: Building2 },
  { to: '/alerts', label: 'nav.alerts', icon: BellRing },
  // Open to everyone: the General tab holds each user's theme and language.
  { to: '/settings', label: 'nav.settings', icon: Settings },
  { to: '/setup', label: 'nav.setup', icon: Rocket, adminOnly: true },
]

/** First path segment → page name, for the document title. */
const TITLES: Record<string, TKey> = {
  '': 'nav.overview',
  devices: 'nav.devices',
  machines: 'nav.machineDetail',
  'slow-machines': 'nav.slowMachines',
  users: 'nav.users',
  departments: 'nav.departments',
  alerts: 'nav.alerts',
  settings: 'nav.settings',
  setup: 'nav.setup',
  profile: 'nav.profile',
}

const SIDEBAR_KEY = 'tolikopa-sidebar'

export function AppShell() {
  const t = useT()
  const lang = useLang()
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(SIDEBAR_KEY) === '1')
  const [mobileOpen, setMobileOpen] = useState(false)
  const location = useLocation()
  const outlet = useOutlet()

  useAlertsStream()

  const segments = location.pathname.split('/').filter(Boolean)
  const section = segments[0] ?? ''
  const titleKey: TKey = section === 'users' && segments.length > 1 ? 'nav.userDetail' : (TITLES[section] ?? 'nav.notFound')
  useDocumentTitle(t(titleKey))

  useEffect(() => {
    localStorage.setItem(SIDEBAR_KEY, collapsed ? '1' : '0')
  }, [collapsed])

  // Close the mobile drawer on navigation.
  useEffect(() => {
    setMobileOpen(false)
  }, [location.pathname])

  return (
    <div className="flex min-h-screen">
      {/* Desktop rail */}
      <aside
        className={cn(
          'no-print sticky top-0 hidden h-screen shrink-0 flex-col border-r border-border bg-bg-elev/70 lg:flex',
          collapsed ? 'w-[76px]' : 'w-[248px]',
          'transition-[width] duration-200',
        )}
      >
        <SidebarContent collapsed={collapsed} onToggle={() => setCollapsed((v) => !v)} />
      </aside>

      {/* Mobile drawer */}
      <AnimatePresence>
        {mobileOpen && (
          <div className="no-print fixed inset-0 z-50 lg:hidden">
            <motion.div
              className="absolute inset-0 bg-scrim backdrop-blur-sm"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              onClick={() => setMobileOpen(false)}
            />
            <motion.aside
              className="absolute inset-y-0 left-0 flex w-[264px] flex-col border-r border-border bg-bg-elev"
              initial={{ x: '-100%' }}
              animate={{ x: 0 }}
              exit={{ x: '-100%' }}
              transition={{ type: 'spring', stiffness: 320, damping: 34 }}
            >
              <SidebarContent collapsed={false} onClose={() => setMobileOpen(false)} />
            </motion.aside>
          </div>
        )}
      </AnimatePresence>

      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar onMenu={() => setMobileOpen(true)} />
        <SetupBanner />
        <main className="flex-1">
          <div className="mx-auto w-full max-w-[1440px] px-4 py-6 sm:px-6 lg:px-8">
            <AnimatePresence mode="wait">
              {/* Keyed on the language too: a switch remounts the page, so anything a
                  page memoized or kept in state is rebuilt in the new language. */}
              <motion.div
                key={`${location.pathname}|${lang}`}
                variants={routeVariants}
                initial="hidden"
                animate="show"
                exit="exit"
              >
                <Suspense fallback={<RouteFallback />}>{outlet}</Suspense>
              </motion.div>
            </AnimatePresence>
          </div>
        </main>
      </div>

      <ToastHost />
    </div>
  )
}

function SidebarContent({
  collapsed,
  onToggle,
  onClose,
}: {
  collapsed: boolean
  onToggle?: () => void
  onClose?: () => void
}) {
  const t = useT()
  const isAdmin = useIsAdmin()
  const { data: summary } = useFleetSummary()
  const { data: pub } = usePublicSettings()
  const alertTotal = summary ? Object.values(summary.alerts_by_severity ?? {}).reduce((a, b) => a + b, 0) : 0
  const alertTone = (summary?.alerts_by_severity?.critical ?? summary?.alerts_by_severity?.crit ?? 0) > 0 ? 'crit' : 'warn'

  return (
    <>
      <div className={cn('flex h-16 items-center border-b border-border', collapsed ? 'justify-center px-2' : 'justify-between px-4')}>
        {collapsed ? <LogoMark size={30} /> : <Logo subtitle />}
        {onClose ? (
          <button
            type="button"
            onClick={onClose}
            aria-label={t('shell.closeMenu')}
            className="grid size-8 shrink-0 place-items-center rounded-full text-text-muted hover:bg-wash-2 hover:text-text ring-focus"
          >
            <X className="size-4" aria-hidden />
          </button>
        ) : null}
      </div>

      <nav className="flex-1 space-y-1 overflow-y-auto p-3" aria-label={t('shell.mainNav')}>
        {NAV.filter((n) => !n.adminOnly || isAdmin).map((item) => {
          const badge = item.to === '/alerts' && alertTotal > 0 ? alertTotal : undefined
          const label = t(item.label)
          return (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              title={collapsed ? label : undefined}
              aria-label={collapsed ? label : undefined}
              className={({ isActive }) =>
                cn(
                  'group relative flex items-center gap-3 rounded-[var(--radius-md)] px-3 py-2.5 text-[14px] font-medium transition-colors ring-focus',
                  collapsed && 'justify-center px-0',
                  isActive ? 'bg-accent-grad-soft text-text' : 'text-text-muted hover:bg-glass hover:text-text',
                )
              }
            >
              {({ isActive }) => (
                <>
                  {isActive && (
                    <span className="absolute left-0 top-1/2 h-5 w-1 -translate-y-1/2 rounded-r-full bg-accent-grad" aria-hidden />
                  )}
                  <item.icon className={cn('size-[18px] shrink-0', isActive && 'text-accent')} aria-hidden />
                  {!collapsed && <span className="flex-1">{label}</span>}
                  {!collapsed && badge !== undefined && (
                    <Badge tone={alertTone as 'crit' | 'warn'}>
                      <span className="tabular">{badge}</span>
                    </Badge>
                  )}
                  {collapsed && badge !== undefined && (
                    <span className="absolute right-1.5 top-1.5 size-2 rounded-full bg-crit" aria-hidden />
                  )}
                </>
              )}
            </NavLink>
          )
        })}
      </nav>

      {(onToggle || pub?.version) && (
        <div className="border-t border-border p-3">
          {onToggle && (
            <button
              type="button"
              onClick={onToggle}
              aria-label={collapsed ? t('shell.expandSidebar') : t('shell.collapseSidebar')}
              className={cn(
                'flex w-full items-center gap-3 rounded-[var(--radius-md)] px-3 py-2 text-[13px] font-medium text-text-muted transition-colors hover:bg-glass hover:text-text ring-focus',
                collapsed && 'justify-center px-0',
              )}
            >
              <PanelLeft className={cn('size-[18px] transition-transform', collapsed && 'rotate-180')} aria-hidden />
              {!collapsed && <span>{t('shell.collapse')}</span>}
            </button>
          )}
          {!collapsed && pub?.version && (
            <p className="tabular mt-1 truncate px-3 text-[11px] text-text-dim" title={t('shell.version', { version: pub.version })}>
              {t('shell.version', { version: pub.version })}
            </p>
          )}
        </div>
      )}
    </>
  )
}

const BANNER_KEY = 'tolikopa-setup-banner-dismissed'

/**
 * "Setup is not finished" — for admins of an installation that has an admin but no
 * completed setup (upgraded from before the wizard, or "finish later"). It never
 * blocks the app and can be dismissed for the browser session.
 */
function SetupBanner() {
  const t = useT()
  const isAdmin = useIsAdmin()
  const location = useLocation()
  const { data } = useSetupState({ enabled: isAdmin })
  const [dismissed, setDismissed] = useState(() => sessionStorage.getItem(BANNER_KEY) === '1')

  if (!isAdmin || !data || data.completed_at || dismissed || location.pathname === '/setup') return null

  return (
    <div className="no-print flex items-center gap-3 border-b border-warn/25 bg-warn/10 px-4 py-2 text-[13px] sm:px-6" role="status">
      <TriangleAlert className="size-4 shrink-0 text-warn" aria-hidden />
      <p className="min-w-0 flex-1 text-text">
        {t('setup.banner.text')}{' '}
        <Link to="/setup" className="font-semibold text-accent hover:underline ring-focus">
          {t('setup.banner.link')}
        </Link>
      </p>
      <button
        type="button"
        aria-label={t('setup.banner.dismiss')}
        onClick={() => {
          sessionStorage.setItem(BANNER_KEY, '1')
          setDismissed(true)
        }}
        className="grid size-7 shrink-0 place-items-center rounded-full text-text-muted hover:bg-wash-2 hover:text-text ring-focus"
      >
        <X className="size-4" aria-hidden />
      </button>
    </div>
  )
}

function TopBar({ onMenu }: { onMenu: () => void }) {
  const t = useT()
  const navigate = useNavigate()
  const connected = useWsStatus()
  const [query, setQuery] = useState('')

  const onSearch = (e: FormEvent) => {
    e.preventDefault()
    navigate(query.trim() ? `/devices?q=${encodeURIComponent(query.trim())}` : '/devices')
  }

  return (
    <header className="glass no-print sticky top-0 z-30 flex h-16 items-center gap-3 border-b border-border px-4 sm:px-6">
      <button
        type="button"
        onClick={onMenu}
        aria-label={t('shell.openMenu')}
        className="grid size-9 place-items-center rounded-[var(--radius-md)] text-text-muted hover:bg-glass hover:text-text ring-focus lg:hidden"
      >
        <Menu className="size-5" aria-hidden />
      </button>

      <form onSubmit={onSearch} className="relative hidden max-w-sm flex-1 sm:block" role="search">
        <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-text-dim" aria-hidden />
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t('shell.searchPlaceholder')}
          aria-label={t('shell.search')}
          className="h-9 w-full rounded-full border border-border-2 bg-surface/60 pl-9 pr-3 text-[13px] text-text placeholder:text-text-dim outline-none transition focus:border-accent/60 focus:ring-2 focus:ring-accent/25"
        />
      </form>

      <div className="ml-auto flex items-center gap-2 sm:gap-3">
        <LiveStatus connected={connected} />
        <UserMenu />
      </div>
    </header>
  )
}

function LiveStatus({ connected }: { connected: boolean }) {
  const t = useT()
  return (
    <span
      className={cn(
        'inline-flex items-center gap-2 rounded-full border px-2.5 py-1 text-[12px] font-medium',
        connected ? 'border-ok/25 bg-ok/10 text-ok' : 'border-warn/25 bg-warn/10 text-warn',
      )}
      title={connected ? t('shell.liveOn') : t('shell.liveOff')}
    >
      <span
        className={cn('size-1.5 rounded-full', connected ? 'bg-ok live-dot' : 'bg-warn')}
        style={{ '--pulse-color': 'currentColor' } as CSSProperties}
        aria-hidden
      />
      <span className="hidden sm:inline">{connected ? t('common.live') : t('shell.connecting')}</span>
      <span className="sr-only sm:hidden">{connected ? t('shell.liveOn') : t('shell.liveOff')}</span>
    </span>
  )
}

function UserMenu() {
  const t = useT()
  const { user, logout } = useAuth()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const name = user?.display_name?.trim() || displayUser(user?.username ?? '')
  const itemClass =
    'flex w-full items-center gap-2 rounded-[var(--radius-md)] px-3 py-2 text-[13px] font-medium text-text-muted transition-colors hover:bg-glass hover:text-text ring-focus'

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t('shell.userMenu')}
        className="flex items-center gap-2 rounded-full border border-border-2 bg-surface/60 py-1 pl-1 pr-2.5 transition-colors hover:bg-surface-2 ring-focus"
      >
        <span className="grid size-7 place-items-center rounded-full bg-accent-grad text-[13px] font-bold text-on-accent">
          {initialOf(name)}
        </span>
        <span className="hidden max-w-[140px] truncate text-[13px] font-medium text-text sm:inline">{name}</span>
      </button>

      <AnimatePresence>
        {open && (
          <motion.div
            role="menu"
            initial={{ opacity: 0, y: -6, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: -6, scale: 0.98 }}
            transition={{ duration: 0.14 }}
            className="glass absolute right-0 top-[calc(100%+8px)] w-60 rounded-[var(--radius-lg)] border border-border-2 p-1.5 shadow-[var(--shadow-pop)]"
          >
            <div className="px-3 py-2">
              <p className="truncate text-[13px] font-semibold text-text">{name}</p>
              <p className="truncate text-[12px] text-text-dim">
                {user?.display_name ? `${user.username} · ` : ''}
                {t(user?.role === 'admin' ? 'common.role.admin' : 'common.role.viewer')}
              </p>
            </div>
            <div className="my-1 h-px bg-border" />
            <Link to="/profile" role="menuitem" onClick={() => setOpen(false)} className={itemClass}>
              <UserRound className="size-4" aria-hidden />
              {t('shell.profile')}
            </Link>
            <button
              type="button"
              role="menuitem"
              onClick={() => {
                setOpen(false)
                void logout()
              }}
              className={itemClass}
            >
              <LogOut className="size-4" aria-hidden />
              {t('shell.signOut')}
            </button>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}

function RouteFallback() {
  return (
    <div className="grid min-h-[55vh] place-items-center">
      <LogoMark size={40} animated />
    </div>
  )
}

export function PageHeader({
  title,
  description,
  action,
  breadcrumb,
}: {
  title: ReactNode
  description?: ReactNode
  action?: ReactNode
  breadcrumb?: ReactNode
}) {
  return (
    <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div className="min-w-0">
        {breadcrumb && <div className="mb-1.5">{breadcrumb}</div>}
        <h1 className="text-[24px] font-bold tracking-[-0.01em] text-text">{title}</h1>
        {description && <p className="mt-1 text-[14px] text-text-muted">{description}</p>}
      </div>
      {action && <div className="flex shrink-0 items-center gap-2">{action}</div>}
    </div>
  )
}
