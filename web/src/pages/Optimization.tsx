import { useMemo } from 'react'
import { motion, useReducedMotion } from 'motion/react'
import {
  Gauge, LayoutDashboard, ListChecks, Lock, Monitor, PackageX, Power, ServerCog, SlidersHorizontal, Terminal,
} from 'lucide-react'
import { PageHeader } from '@/components/AppShell'
import { Segmented } from '@/components/PrefControls'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Card } from '@/components/ui/Card'
import { Tabs } from '@/components/ui/Tabs'
import { EmptyState } from '@/components/EmptyState'
import { SkeletonCard, SkeletonText } from '@/components/Skeleton'
import { useFleet, useRemediationPosture } from '@/hooks/queries'
import { useIsAdmin } from '@/hooks/useAuth'
import { useUrlState } from '@/hooks/useUrlState'
import { ApiError } from '@/lib/api'
import { removalModeMeta } from '@/lib/labels'
import { formatNumber } from '@/lib/format'
import { useT } from '@/i18n'
import { AppsTab } from '@/pages/optimization/AppsTab'
import { LinuxTab } from '@/pages/optimization/LinuxTab'
import { OverviewTab } from '@/pages/optimization/OverviewTab'
import { PolicySaveBar, StalePolicyNotice } from '@/pages/optimization/PolicySaveBar'
import { PolicyTab } from '@/pages/optimization/PolicyTab'
import { ResultsTab } from '@/pages/optimization/ResultsTab'
import { StartupTab } from '@/pages/optimization/StartupTab'
import { usePolicyEditor } from '@/pages/optimization/usePolicyEditor'
import type { PolicyEditor } from '@/pages/optimization/usePolicyEditor'

type Platform = 'windows' | 'linux'
const WINDOWS_TABS = ['overview', 'policy', 'apps', 'startup', 'results'] as const
type WindowsTab = (typeof WINDOWS_TABS)[number]

/**
 * Fleet optimization: bloatware removal and logon startup control on Windows
 * (desired-state reconciliation over the agent's compiled catalog), plus
 * read-only recommendations for Linux hosts, whose agent cannot act at all.
 *
 * Linkable: /optimization?platform=windows&tab=startup
 * (the old /settings?tab=removal is redirected here in App.tsx).
 */
export function Optimization() {
  const t = useT()
  const isAdmin = useIsAdmin()
  if (!isAdmin) {
    return <EmptyState icon={Lock} title={t('shell.adminRequiredTitle')} description={t('settings.adminRequiredBody')} />
  }
  return <OptimizationBody />
}

function OptimizationBody() {
  const t = useT()
  const reduce = useReducedMotion()
  const [platformParam, setPlatform] = useUrlState('platform', 'windows')
  const [tabParam, setTab] = useUrlState('tab', 'overview')
  const platform: Platform = platformParam === 'linux' ? 'linux' : 'windows'
  const tab: WindowsTab = (WINDOWS_TABS as readonly string[]).includes(tabParam) ? (tabParam as WindowsTab) : 'overview'

  const editor = usePolicyEditor()
  const posture = useRemediationPosture()
  const fleet = useFleet()

  // Platform counts: the posture roll-up when the server has it, the live fleet
  // list otherwise (an older server without /remediation/posture).
  const counts = useMemo(() => {
    const fromPosture = posture.data?.platforms
    if (fromPosture) {
      const get = (k: string) => fromPosture.find((p) => p.kind === k)?.machines ?? 0
      return { windows: get('windows'), linux: get('linux') }
    }
    const list = fleet.data ?? []
    return {
      windows: list.filter((m) => m.kind !== 'linux').length,
      linux: list.filter((m) => m.kind === 'linux').length,
    }
  }, [posture.data, fleet.data])

  const modeMeta = removalModeMeta(editor.savedMode)

  return (
    <div>
      <PageHeader
        title={t('nav.optimization')}
        description={t('optimization.subtitle')}
        action={
          editor.data ? (
            <Badge tone={modeMeta.token} dot pulse={editor.savedMode === 'enforce'} title={t('optimization.modeBadgeTitle')}>
              {t('optimization.modeBadge', { mode: modeMeta.label })}
            </Badge>
          ) : null
        }
      />

      <div className="mb-5 flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <Segmented<Platform>
          label={t('optimization.platformLabel')}
          hideLabel
          value={platform}
          onChange={(p) => setPlatform(p === 'windows' ? '' : p)}
          options={[
            {
              value: 'windows',
              label: t('optimization.platformOption', { name: 'Windows', count: formatNumber(counts.windows) }),
              icon: <Monitor className="size-4" aria-hidden />,
            },
            {
              value: 'linux',
              label: t('optimization.platformOption', { name: 'Linux', count: formatNumber(counts.linux) }),
              icon: <Terminal className="size-4" aria-hidden />,
            },
          ]}
        />
        {platform === 'windows' && (
          <Tabs
            tabs={windowsTabs(t, editor)}
            value={tab}
            onChange={(k) => setTab(k === 'overview' ? '' : k)}
            ariaLabel={t('optimization.tabsLabel')}
            className="w-full lg:w-auto"
          />
        )}
      </div>

      <motion.div
        key={platform === 'linux' ? 'linux' : tab}
        initial={reduce ? false : { opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.2, ease: [0.22, 1, 0.36, 1] }}
      >
        {platform === 'linux' ? (
          <LinuxTab machines={posture.data?.linux} isLoading={posture.isLoading} isError={posture.isError} />
        ) : (
          <WindowsContent
            tab={tab}
            editor={editor}
            posture={posture}
          />
        )}
      </motion.div>
    </div>
  )
}

function windowsTabs(t: ReturnType<typeof useT>, editor: PolicyEditor) {
  const selApps = editor.draft.draft?.apps.length ?? 0
  const selStartup = editor.draft.draft?.startup.length ?? 0
  return [
    { key: 'overview', label: t('optimization.tab.overview'), icon: <LayoutDashboard className="size-4" aria-hidden /> },
    {
      key: 'policy',
      label: t('optimization.tab.policy'),
      icon: <SlidersHorizontal className="size-4" aria-hidden />,
      badge: <PolicyTabBadge editor={editor} />,
    },
    {
      key: 'apps',
      label: t('optimization.tab.apps'),
      icon: <PackageX className="size-4" aria-hidden />,
      badge: selApps > 0 ? <Badge tone="neutral">{selApps}</Badge> : undefined,
    },
    {
      key: 'startup',
      label: t('optimization.tab.startup'),
      icon: <Power className="size-4" aria-hidden />,
      badge: selStartup > 0 ? <Badge tone="neutral">{selStartup}</Badge> : undefined,
    },
    { key: 'results', label: t('optimization.tab.results'), icon: <ListChecks className="size-4" aria-hidden /> },
  ]
}

/** The live mode on the tab itself — an admin should never have to open it to
 *  find out that the fleet is currently enforcing. */
function PolicyTabBadge({ editor }: { editor: PolicyEditor }) {
  if (!editor.data) return null
  if (editor.savedMode === 'off') return null
  const meta = removalModeMeta(editor.savedMode)
  return (
    <Badge tone={meta.token} dot pulse={editor.savedMode === 'enforce'}>
      {meta.label}
    </Badge>
  )
}

function WindowsContent({
  tab,
  editor,
  posture,
}: {
  tab: WindowsTab
  editor: PolicyEditor
  posture: ReturnType<typeof useRemediationPosture>
}) {
  const t = useT()
  const { overview } = editor

  if (overview.isError) {
    const notDeployed = overview.error instanceof ApiError && overview.error.status === 404
    return (
      <EmptyState
        icon={ServerCog}
        title={notDeployed ? t('removal.section.notDeployedTitle') : t('removal.section.loadFailedTitle')}
        description={notDeployed ? t('removal.section.notDeployedBody') : t('removal.section.loadFailedBody')}
        action={
          <Button variant="secondary" onClick={() => overview.refetch()} loading={overview.isFetching}>
            {t('common.retry')}
          </Button>
        }
      />
    )
  }

  // `draft.draft` is null for the first render after the policy lands (the seeding
  // effect has not run yet) — keep the skeleton rather than flashing an error.
  if (!editor.data || !editor.draft.draft) {
    return (
      <div className="space-y-4">
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <SkeletonCard key={i} />
          ))}
        </div>
        <Card>
          <SkeletonText lines={8} />
        </Card>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Announces what the FLEET is doing (saved policy), not what is drafted. */}
      <p role="status" aria-live="polite" className="sr-only">
        {editor.savedMode === 'enforce'
          ? t('removal.section.liveEnforce', { machines: editor.targetedCount, apps: editor.data.policy.apps.length })
          : t('removal.section.liveMode', { mode: removalModeMeta(editor.savedMode).label })}
      </p>

      <StalePolicyNotice editor={editor} />

      {tab === 'overview' && (
        <OverviewTab editor={editor} posture={posture.data} isLoading={posture.isLoading} isError={posture.isError} />
      )}
      {tab === 'policy' && <PolicyTab editor={editor} />}
      {tab === 'apps' && <AppsTab editor={editor} posture={posture.data} postureLoading={posture.isLoading} />}
      {tab === 'startup' && <StartupTab editor={editor} posture={posture.data} postureLoading={posture.isLoading} />}
      {tab === 'results' && <ResultsTab editor={editor} />}

      <PolicySaveBar editor={editor} />

      {tab === 'overview' && (
        <p className="flex items-start gap-2 text-[12px] text-text-dim">
          <Gauge className="mt-px size-3.5 shrink-0" aria-hidden />
          <span>{t('optimization.overview.footnote')}</span>
        </p>
      )}
    </div>
  )
}
