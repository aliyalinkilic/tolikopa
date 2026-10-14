import { useState } from 'react'
import type { ReactNode } from 'react'
import {
  ArrowUpCircle, CheckCircle2, ChevronDown, Globe, HardDriveDownload, KeyRound, Lock, Network, RotateCcw, Server,
  ServerCog, TriangleAlert, Wrench,
} from 'lucide-react'
import { PageHeader } from '@/components/AppShell'
import { Card, CardHeader } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { EmptyState } from '@/components/EmptyState'
import { SkeletonCard } from '@/components/Skeleton'
import { DomainConfigForm, ServerUrlForm, dcSuffix } from '@/components/setup/ConfigForms'
import { ArtifactInfo, CodeBlock, DownloadButton, RequiredRights, RunInstructions, SETUP_TAGS, isReady } from '@/components/setup/parts'
import { useReopenSetup, useSetupState } from '@/hooks/queries'
import { useIsAdmin } from '@/hooks/useAuth'
import { useToast } from '@/hooks/useToast'
import { errorText } from '@/lib/errors'
import { formatDateTime } from '@/lib/format'
import { setWizardStep } from '@/lib/wizard'
import { cn } from '@/lib/cn'
import { useT } from '@/i18n'
import { Trans } from '@/i18n/rich'
import type { SetupState } from '@/lib/types'

const BINARY_PATH = 'C:\\ProgramData\\Tolikopa\\tolikopa-agent.exe'
const LOG_PATH = 'C:\\ProgramData\\Tolikopa\\logs\\agent.log'

export function Setup() {
  const t = useT()
  const isAdmin = useIsAdmin()
  const { data, isLoading, isError, error, refetch, isFetching } = useSetupState({ enabled: isAdmin })

  if (!isAdmin) {
    return <EmptyState icon={Lock} title={t('shell.adminRequiredTitle')} description={t('settings.adminRequiredBody')} />
  }

  return (
    <div>
      <PageHeader title={t('nav.setup')} description={t('setup.subtitle')} />
      {isLoading ? (
        <div className="grid gap-4 lg:grid-cols-2">
          <SkeletonCard />
          <SkeletonCard />
        </div>
      ) : isError || !data ? (
        <EmptyState
          icon={ServerCog}
          title={t('setup.loadFailedTitle')}
          description={error ? errorText(error) : t('setup.loadFailedBody')}
          action={
            <Button onClick={() => void refetch()} loading={isFetching}>
              {t('common.retry')}
            </Button>
          }
        />
      ) : (
        <div className="space-y-4">
          <StatusSection state={data} />
          <UpdateCard state={data} />
          <DomainSection state={data} />
          <AdvancedSection state={data} />
          <MaintenanceSection />
        </div>
      )}
    </div>
  )
}

function Label({ children }: { children: ReactNode }) {
  return <dt className="text-[11px] font-semibold uppercase tracking-[0.12em] text-text-dim">{children}</dt>
}

// ── Status ─────────────────────────────────────────────────────────────────────
function StatusSection({ state }: { state: SetupState }) {
  const t = useT()
  const fleet = state.fleet
  const versions = [...(fleet?.by_agent_version ?? [])].sort((a, b) => b.count - a.count)
  const max = Math.max(1, ...versions.map((v) => v.count))
  const currentVersion = state.artifacts?.agent_version ?? ''

  return (
    <div className="grid items-start gap-4 lg:grid-cols-2">
      <Card>
        <div className="flex flex-wrap items-center justify-between gap-3">
          {state.completed_at ? (
            <Badge tone="ok" icon={<CheckCircle2 className="size-3.5" aria-hidden />}>
              {t('setup.status.completed')}
            </Badge>
          ) : (
            <Badge tone="warn" icon={<TriangleAlert className="size-3.5" aria-hidden />}>
              {t('setup.status.notCompleted')}
            </Badge>
          )}
          {state.completed_at ? (
            <span className="tabular text-[12px] text-text-dim">
              {t('setup.status.completedAt', { when: formatDateTime(state.completed_at) })}
            </span>
          ) : (
            <Button size="sm" variant="primary" onClick={() => setWizardStep('server')}>
              {t('setup.status.continueWizard')}
            </Button>
          )}
        </div>

        <dl className="mt-5 space-y-4">
          <div>
            <Label>{t('setup.status.serverUrl')}</Label>
            <dd className="mt-1 flex items-center gap-2 text-[14px]">
              <Server className="size-4 shrink-0 text-text-dim" aria-hidden />
              {state.config.server_url ? (
                <span className="break-all font-mono text-text">{state.config.server_url}</span>
              ) : (
                <span className="text-warn">{t('setup.status.notSet')}</span>
              )}
            </dd>
          </div>
          <div>
            <Label>{t('setup.status.ingestToken')}</Label>
            <dd className="mt-1 flex items-center gap-2 text-[14px]">
              <KeyRound className="size-4 shrink-0 text-text-dim" aria-hidden />
              <span className="tabular break-all font-mono text-text">{state.ingest_token_masked || '••••••••••••'}</span>
            </dd>
            <p className="mt-1 text-[12px] text-text-dim">{t('setup.status.tokenNote')}</p>
          </div>
        </dl>

        <div className="mt-5 border-t border-border pt-5">
          <h3 className="mb-2 text-[14px] font-semibold text-text">{t('setup.status.agentBuild')}</h3>
          <ArtifactInfo artifacts={state.artifacts} />
        </div>
      </Card>

      <Card>
        <CardHeader
          title={t('setup.status.fleetTitle')}
          description={t('setup.status.fleetNote')}
          action={
            <Badge tone="neutral">
              <span className="tabular">{t('common.machines', { count: fleet?.total ?? 0 })}</span>
            </Badge>
          }
        />
        {versions.length === 0 ? (
          <p className="mt-4 text-[13px] text-text-muted">{t('setup.status.fleetEmpty')}</p>
        ) : (
          <ul className="mt-4 space-y-3">
            {versions.map((v) => {
              const isCurrent = v.version !== '' && v.version === currentVersion
              return (
                <li key={v.version || 'unknown'}>
                  <div className="flex items-center justify-between gap-3 text-[13px]">
                    <span className="flex min-w-0 items-center gap-2">
                      <span className={cn('truncate', v.version ? 'font-mono text-text' : 'text-text-muted')}>
                        {v.version || t('setup.status.unknownVersion')}
                      </span>
                      {v.version !== '' && (
                        <Badge tone={isCurrent ? 'ok' : 'warn'}>{isCurrent ? t('setup.status.current') : t('setup.status.outdated')}</Badge>
                      )}
                    </span>
                    <span className="tabular shrink-0 font-semibold text-text">{v.count}</span>
                  </div>
                  <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-wash-2" aria-hidden>
                    <div
                      className={cn('h-full rounded-full', isCurrent ? 'bg-ok' : v.version ? 'bg-warn' : 'bg-offline')}
                      style={{ width: `${Math.max(3, (v.count / max) * 100)}%` }}
                    />
                  </div>
                </li>
              )
            })}
          </ul>
        )}
        {versions.length > 0 && (
          <dl className="mt-5 grid grid-cols-3 gap-3 border-t border-border pt-4 text-center">
            {(
              [
                ['total', fleet.total, 'text-text'],
                ['current', fleet.current, 'text-ok'],
                ['outdated', fleet.outdated, fleet.outdated > 0 ? 'text-warn' : 'text-text'],
              ] as const
            ).map(([key, value, tone]) => (
              <div key={key}>
                <Label>{t(`setup.status.${key}`)}</Label>
                <dd className={cn('tabular mt-1 text-[20px] font-bold', tone)}>{value}</dd>
              </div>
            ))}
          </dl>
        )}
      </Card>
    </div>
  )
}

// ── Update ─────────────────────────────────────────────────────────────────────
function UpdateCard({ state }: { state: SetupState }) {
  const t = useT()
  const available = state.update_available
  return (
    <Card className={available ? 'border-accent/40 bg-accent/[0.05]' : undefined}>
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="flex min-w-0 items-start gap-3">
          <span
            className={cn(
              'grid size-10 shrink-0 place-items-center rounded-[12px]',
              available ? 'bg-accent-grad text-on-accent' : 'bg-ok/12 text-ok',
            )}
            aria-hidden
          >
            {available ? <ArrowUpCircle className="size-5" /> : <CheckCircle2 className="size-5" />}
          </span>
          <div className="min-w-0">
            <p className="text-[11px] font-semibold uppercase tracking-[0.14em] text-text-dim">{t('setup.update.title')}</p>
            <h3 className="mt-0.5 text-[16px] font-semibold text-text">
              {available ? t('setup.update.available', { count: state.fleet?.outdated ?? 0 }) : t('setup.update.upToDate')}
            </h3>
            {available ? (
              <ol className="mt-2 space-y-1 text-[13px] text-text-muted">
                {(['how1', 'how2', 'how3'] as const).map((k, i) => (
                  <li key={k} className="flex items-start gap-2">
                    <span className="tabular shrink-0 font-semibold text-text-dim">{i + 1}.</span>
                    <span>{t(`setup.update.${k}`)}</span>
                  </li>
                ))}
              </ol>
            ) : (
              <p className="mt-1 text-[13px] text-text-muted">{t('setup.update.upToDateBody')}</p>
            )}
          </div>
        </div>
        {available && (
          <DownloadButton file="tolikopa-update.exe" variant="primary" className="shrink-0">
            {t('setup.update.download')}
          </DownloadButton>
        )}
      </div>
    </Card>
  )
}

// ── Domain configuration ───────────────────────────────────────────────────────
function DomainSection({ state }: { state: SetupState }) {
  const t = useT()
  const [dirty, setDirty] = useState(false)
  const ready = isReady(state.readiness)
  return (
    <div className="grid items-start gap-4 lg:grid-cols-[1.55fr_1fr]">
      <div className="space-y-4">
        <Card>
          <div className="flex items-start gap-3">
            <span className="grid size-9 shrink-0 place-items-center rounded-[12px] bg-glass text-accent" aria-hidden>
              <Globe className="size-[18px]" />
            </span>
            <CardHeader title={t('setup.server.title')} description={t('setup.server.body')} className="flex-1" />
          </div>
          <div className="mt-5">
            <ServerUrlForm config={state.config} candidates={state.server_url_candidates ?? []} />
          </div>
        </Card>

        <Card>
          <div className="flex items-start gap-3">
            <span className="grid size-9 shrink-0 place-items-center rounded-[12px] bg-glass text-accent" aria-hidden>
              <Network className="size-[18px]" />
            </span>
            <CardHeader title={t('setup.domain.title')} description={t('setup.domain.body')} className="flex-1" />
          </div>
          <div className="mt-5">
            <DomainConfigForm config={state.config} onDirtyChange={setDirty} />
          </div>
        </Card>

        <Card>
          <CardHeader title={t('setup.domain.redownloadTitle')} description={t('setup.domain.redownloadBody')} />
          <div className="mt-4 flex flex-wrap items-center gap-3">
            <DownloadButton file="tolikopa-setup.exe" variant="primary" disabled={dirty || !ready}>
              {t('setup.run.download')}
            </DownloadButton>
            {dirty && <span className="text-[12px] text-warn">{t('setup.domain.unsaved')}</span>}
          </div>
          <div className="mt-5 border-t border-border pt-5">
            <RunInstructions />
          </div>
        </Card>
      </div>
      <RequiredRights className="lg:sticky lg:top-20" />
    </div>
  )
}

// ── Advanced: manual files + manual GPO steps ─────────────────────────────────
const FILES = [
  ['tolikopa-agent.exe', 'setup.advanced.agentWindows'],
  ['tolikopa-agent-linux', 'setup.advanced.agentLinux'],
  ['bootstrap.ps1', 'setup.advanced.bootstrap'],
  ['config.json', 'setup.advanced.config'],
] as const

function AdvancedSection({ state }: { state: SetupState }) {
  const t = useT()
  const [open, setOpen] = useState(false)
  // Paths are built from the configured domain — never a hard-coded one.
  const fqdn = state.config.domain_fqdn || t('setup.domainPlaceholder')
  const share = `\\\\${fqdn}\\NETLOGON\\Tolikopa`
  const target = state.config.target_dn || dcSuffix(state.config.domain_fqdn) || t('setup.domainPlaceholder')
  const gpo = state.config.gpo_name || 'Tolikopa Agent'

  const copyCmd = `$dst = "${share}"
New-Item -ItemType Directory -Force -Path $dst | Out-Null
Copy-Item .\\tolikopa-agent.exe, .\\bootstrap.ps1, .\\config.json -Destination $dst -Force`
  const taskArgs = `-WindowStyle Hidden -NonInteractive -ExecutionPolicy Bypass -File ${share}\\bootstrap.ps1`
  const testCmd = `gpupdate /force
Start-ScheduledTask -TaskName TolikopaAgent
Get-Content ${LOG_PATH} -Tail 20`

  return (
    <Card padded={false}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        aria-controls="setup-advanced"
        className="flex w-full items-center gap-3 rounded-[inherit] p-5 text-left ring-focus"
      >
        <span className="grid size-9 shrink-0 place-items-center rounded-[12px] bg-glass text-text-muted" aria-hidden>
          <HardDriveDownload className="size-[18px]" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-[15px] font-semibold text-text">{t('setup.advanced.title')}</span>
          <span className="mt-0.5 block text-[13px] text-text-muted">{t('setup.advanced.body')}</span>
        </span>
        <ChevronDown className={cn('size-5 shrink-0 text-text-dim transition-transform', open && 'rotate-180')} aria-hidden />
      </button>

      {open && (
        <div id="setup-advanced" className="space-y-6 border-t border-border p-5">
          <div>
            <h3 className="text-[14px] font-semibold text-text">{t('setup.advanced.files')}</h3>
            <ul className="mt-3 grid gap-2 sm:grid-cols-2">
              {FILES.map(([file, label]) => (
                <li key={file} className="flex items-center justify-between gap-3 rounded-[var(--radius-md)] border border-border px-3 py-2">
                  <span className="min-w-0">
                    <span className="block truncate font-mono text-[13px] text-text">{file}</span>
                    <span className="block text-[12px] text-text-dim">{t(label)}</span>
                  </span>
                  <DownloadButton file={file} size="sm" aria-label={`${t('common.download')} ${file}`}>
                    {t('common.download')}
                  </DownloadButton>
                </li>
              ))}
            </ul>
            <p className="mt-2 flex items-start gap-2 text-[12px] text-warn">
              <TriangleAlert className="mt-px size-3.5 shrink-0" aria-hidden />
              {t('setup.advanced.configNote')}
            </p>
          </div>

          <div>
            <h3 className="text-[14px] font-semibold text-text">{t('setup.advanced.manualTitle')}</h3>
            <ol className="mt-3 space-y-4 text-[13px] text-text-muted">
              <ManualStep n={1}>
                <p>{t('setup.advanced.m1')}</p>
                <CodeBlock code={copyCmd} className="mt-2" />
              </ManualStep>
              <ManualStep n={2}>
                <p>
                  <Trans k="setup.advanced.m2" values={{ gpo }} tags={SETUP_TAGS} />
                </p>
              </ManualStep>
              <ManualStep n={3}>
                <p>
                  <Trans k="setup.advanced.m3" tags={SETUP_TAGS} />
                </p>
                <CodeBlock code={taskArgs} className="mt-2" />
              </ManualStep>
              <ManualStep n={4}>
                <p>
                  <Trans k="setup.advanced.m4" values={{ target }} tags={SETUP_TAGS} />
                </p>
              </ManualStep>
              <ManualStep n={5}>
                <p>{t('setup.advanced.m5')}</p>
                <CodeBlock code={testCmd} className="mt-2" />
              </ManualStep>
            </ol>
          </div>
        </div>
      )}
    </Card>
  )
}

function ManualStep({ n, children }: { n: number; children: ReactNode }) {
  return (
    <li className="flex items-start gap-3">
      <span className="tabular grid size-6 shrink-0 place-items-center rounded-full bg-accent-grad-soft text-[12px] font-bold text-accent">
        {n}
      </span>
      <div className="min-w-0 flex-1">{children}</div>
    </li>
  )
}

// ── Maintenance ────────────────────────────────────────────────────────────────
function MaintenanceSection() {
  const t = useT()
  const { push } = useToast()
  const reopen = useReopenSetup()
  const [confirm, setConfirm] = useState(false)

  return (
    <Card>
      <div className="flex items-start gap-3">
        <span className="grid size-9 shrink-0 place-items-center rounded-[12px] bg-glass text-text-muted" aria-hidden>
          <Wrench className="size-[18px]" />
        </span>
        <CardHeader title={t('setup.maintenance.title')} className="flex-1" />
      </div>

      <div className="mt-5 grid gap-6 lg:grid-cols-2">
        <div className="min-w-0">
          <h3 className="text-[14px] font-semibold text-text">{t('setup.maintenance.removeTitle')}</h3>
          <p className="mt-1 text-[13px] text-text-muted">
            <Trans k="setup.maintenance.removeBody" tags={SETUP_TAGS} />
          </p>
          <CodeBlock code={`& "${BINARY_PATH}" uninstall`} className="mt-2" />
        </div>
        <div className="min-w-0">
          <h3 className="text-[14px] font-semibold text-text">{t('setup.maintenance.pathsTitle')}</h3>
          <dl className="mt-2 space-y-2 text-[13px]">
            <div>
              <Label>{t('setup.maintenance.binary')}</Label>
              <dd className="mt-0.5 break-all font-mono text-text">{BINARY_PATH}</dd>
            </div>
            <div>
              <Label>{t('setup.maintenance.log')}</Label>
              <dd className="mt-0.5 break-all font-mono text-text">{LOG_PATH}</dd>
            </div>
          </dl>
        </div>
      </div>

      <div className="mt-6 flex flex-col gap-3 border-t border-border pt-5 sm:flex-row sm:items-center sm:justify-between">
        <div className="min-w-0">
          <h3 className="text-[14px] font-semibold text-text">{t('setup.maintenance.reopenTitle')}</h3>
          <p className="mt-1 text-[13px] text-text-muted">{t('setup.maintenance.reopenBody')}</p>
        </div>
        <Button className="shrink-0" leftIcon={<RotateCcw className="size-4" aria-hidden />} onClick={() => setConfirm(true)}>
          {t('setup.maintenance.reopen')}
        </Button>
      </div>

      <ConfirmDialog
        open={confirm}
        onClose={() => setConfirm(false)}
        tone="primary"
        title={t('setup.maintenance.reopenTitle')}
        description={t('setup.maintenance.reopenBody')}
        confirmLabel={t('setup.maintenance.reopenConfirm')}
        loading={reopen.isPending}
        onConfirm={() =>
          reopen.mutate(undefined, {
            onSuccess: () => {
              setConfirm(false)
              setWizardStep('server') // the gate swaps the app for the wizard
            },
            onError: (err) => push({ title: t('setup.maintenance.reopenFailed'), description: errorText(err), tone: 'crit' }),
          })
        }
      />
    </Card>
  )
}
