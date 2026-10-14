import { useState } from 'react'
import type { ReactNode } from 'react'
import { Check, CheckCircle2, CircleAlert, Copy, Download, KeyRound, RefreshCw, ShieldCheck } from 'lucide-react'
import { Button } from '@/components/ui/Button'
import type { ButtonProps } from '@/components/ui/Button'
import { useCopyText } from '@/components/TwoFactorSetup'
import { useToast } from '@/hooks/useToast'
import { api } from '@/lib/api'
import { errorText } from '@/lib/errors'
import { formatDateTime } from '@/lib/format'
import { cn } from '@/lib/cn'
import { useLang, useT } from '@/i18n'
import { Trans } from '@/i18n/rich'
import type { ReadinessKey, SetupArtifacts, SetupState } from '@/lib/types'

/** Inline markers shared by the setup sentences: <m> = literal to type, <b> = UI name. */
export const SETUP_TAGS = {
  m: (c: ReactNode) => <span className="break-words font-mono text-[0.95em] text-text">{c}</span>,
  b: (c: ReactNode) => <span className="font-semibold text-text">{c}</span>,
}

const DOWNLOAD_BASE = '/api/admin/setup/download/'

interface DownloadButtonProps extends Omit<ButtonProps, 'onClick' | 'loading'> {
  /** File name under /api/admin/setup/download/. */
  file: string
}

/**
 * Downloads a setup artifact. The endpoints answer failures with a JSON error
 * (503 artifacts_unavailable, 409 setup_incomplete, 404), which a plain
 * `<a download>` would happily save as the file — so the bytes are fetched first
 * and a failure becomes a translated toast instead.
 */
export function DownloadButton({ file, children, leftIcon, ...props }: DownloadButtonProps) {
  const t = useT()
  const lang = useLang()
  const { push } = useToast()
  const [busy, setBusy] = useState(false)

  const run = async () => {
    setBusy(true)
    try {
      // The tools print their own messages in the language the admin is using.
      await api.download(DOWNLOAD_BASE + encodeURIComponent(file), { lang }, file)
    } catch (err) {
      push({ title: t('setup.download.failed'), description: errorText(err), tone: 'crit' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Button {...props} loading={busy} onClick={() => void run()} leftIcon={leftIcon ?? <Download className="size-4" aria-hidden />}>
      {children}
    </Button>
  )
}

/** Copyable command / code block. */
export function CodeBlock({ code, className }: { code: string; className?: string }) {
  const t = useT()
  const { copied, copy } = useCopyText()
  return (
    <div className={cn('relative', className)}>
      <pre className="overflow-x-auto rounded-[var(--radius-md)] border border-border bg-code p-3.5 pr-12 text-[12.5px] leading-relaxed">
        <code className="whitespace-pre font-mono text-text-muted">{code}</code>
      </pre>
      <button
        type="button"
        onClick={() => void copy(code)}
        aria-label={copied ? t('common.copied') : t('setup.copyCode')}
        className="absolute right-2 top-2 grid size-8 place-items-center rounded-[9px] border border-border-2 bg-surface-2/80 text-text-muted transition-colors hover:text-text ring-focus"
      >
        {copied ? <Check className="size-4 text-ok" aria-hidden /> : <Copy className="size-4" aria-hidden />}
      </button>
    </div>
  )
}

const READINESS_ORDER: ReadinessKey[] = ['server_url', 'domain', 'root_ca', 'artifacts']

/** The four server-side preconditions, each with what to do when it is not met. */
export function ReadinessList({
  readiness,
  onRefresh,
  refreshing,
}: {
  readiness: SetupState['readiness']
  onRefresh: () => void
  refreshing: boolean
}) {
  const t = useT()
  const allReady = READINESS_ORDER.every((k) => readiness[k])
  return (
    <div>
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-[14px] font-semibold text-text">{t('setup.readiness.title')}</h3>
        <Button size="sm" variant="ghost" loading={refreshing} onClick={onRefresh} leftIcon={<RefreshCw className="size-3.5" aria-hidden />}>
          {t('setup.readiness.refresh')}
        </Button>
      </div>
      <ul className="mt-2 divide-y divide-border rounded-[var(--radius-md)] border border-border">
        {READINESS_ORDER.map((key) => {
          const ok = readiness[key]
          return (
            <li key={key} className="flex items-start gap-3 px-3.5 py-2.5">
              {ok ? (
                <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-ok" aria-hidden />
              ) : (
                <CircleAlert className="mt-0.5 size-4 shrink-0 text-warn" aria-hidden />
              )}
              <div className="min-w-0 flex-1">
                <p className="text-[13px] font-medium text-text">
                  {t(`setup.readiness.${key}.label`)}
                  <span className={cn('ml-2 text-[12px] font-normal', ok ? 'text-ok' : 'text-warn')}>
                    {ok ? t('setup.readiness.ready') : t('setup.readiness.notReady')}
                  </span>
                </p>
                {!ok && <p className="mt-0.5 text-[12px] text-text-muted">{t(`setup.readiness.${key}.fix`)}</p>}
              </div>
            </li>
          )
        })}
      </ul>
      {allReady && <p className="mt-2 text-[12px] text-text-dim">{t('setup.readiness.allReady')}</p>}
    </div>
  )
}

export function isReady(readiness: SetupState['readiness'] | undefined): boolean {
  return !!readiness && READINESS_ORDER.every((k) => readiness[k])
}

/** Agent version, build time and the SHA-256 the setup tool will also print. */
export function ArtifactInfo({ artifacts }: { artifacts: SetupArtifacts | null }) {
  const t = useT()
  const { copied, copy } = useCopyText()
  if (!artifacts) {
    return <p className="text-[13px] text-warn">{t('setup.status.noArtifacts')}</p>
  }
  return (
    <dl className="space-y-2 text-[13px]">
      <div className="flex flex-wrap gap-x-6 gap-y-2">
        <div>
          <dt className="text-[11px] font-semibold uppercase tracking-[0.12em] text-text-dim">{t('setup.status.version')}</dt>
          <dd className="tabular mt-0.5 font-mono text-text">{artifacts.agent_version || '—'}</dd>
        </div>
        <div>
          <dt className="text-[11px] font-semibold uppercase tracking-[0.12em] text-text-dim">{t('setup.status.builtAt')}</dt>
          <dd className="tabular mt-0.5 text-text">{artifacts.built_at ? formatDateTime(artifacts.built_at) : '—'}</dd>
        </div>
      </div>
      <div>
        <dt className="text-[11px] font-semibold uppercase tracking-[0.12em] text-text-dim">{t('setup.status.sha')}</dt>
        <dd className="mt-1 flex items-start gap-2">
          <code className="tabular min-w-0 flex-1 select-all break-all rounded-[var(--radius-sm)] border border-border bg-code px-2.5 py-1.5 font-mono text-[12px] text-text">
            {artifacts.agent_sha256 || '—'}
          </code>
          <Button
            size="icon"
            aria-label={copied ? t('common.copied') : t('setup.status.copySha')}
            disabled={!artifacts.agent_sha256}
            onClick={() => void copy(artifacts.agent_sha256)}
          >
            {copied ? <Check className="size-4 text-ok" aria-hidden /> : <Copy className="size-4" aria-hidden />}
          </Button>
        </dd>
        <p className="mt-1.5 text-[12px] text-text-dim">{t('setup.status.shaNote')}</p>
      </div>
    </dl>
  )
}

/** What the person running the tool needs on the domain — and why the panel never
 *  asks for a domain password. */
export function RequiredRights({ className }: { className?: string }) {
  const t = useT()
  return (
    <aside className={cn('card p-5', className)}>
      <div className="flex items-center gap-2.5">
        <span className="grid size-9 shrink-0 place-items-center rounded-[12px] bg-glass text-accent" aria-hidden>
          <KeyRound className="size-[18px]" />
        </span>
        <h3 className="text-[15px] font-semibold text-text">{t('setup.rights.title')}</h3>
      </div>
      <p className="mt-3 text-[13px] text-text-muted">{t('setup.rights.intro')}</p>
      <ul className="mt-2 space-y-2 text-[13px] text-text-muted">
        {(['netlogon', 'gpoCreate', 'gpoLink', 'dns', 'machine'] as const).map((k) => (
          <li key={k} className="flex items-start gap-2">
            <span className="mt-[7px] size-1.5 shrink-0 rounded-full bg-accent" aria-hidden />
            <span>
              <Trans k={`setup.rights.${k}`} tags={SETUP_TAGS} />
            </span>
          </li>
        ))}
      </ul>
      <div className="mt-4 flex items-start gap-2.5 rounded-[var(--radius-md)] border border-ok/25 bg-ok/[0.07] p-3">
        <ShieldCheck className="mt-0.5 size-4 shrink-0 text-ok" aria-hidden />
        <div className="min-w-0 text-[12px] text-text-muted">
          <p className="font-semibold text-text">{t('setup.rights.noPasswordTitle')}</p>
          <p className="mt-0.5">{t('setup.rights.noPasswordBody')}</p>
        </div>
      </div>
    </aside>
  )
}

/** Numbered "how to run the tool" list. */
export function RunInstructions() {
  const t = useT()
  return (
    <div>
      <h3 className="text-[14px] font-semibold text-text">{t('setup.run.howTitle')}</h3>
      <ol className="mt-2 space-y-2 text-[13px] text-text-muted">
        {(['step1', 'step2', 'step3', 'step4'] as const).map((k, i) => (
          <li key={k} className="flex items-start gap-2.5">
            <span className="tabular grid size-5 shrink-0 place-items-center rounded-full bg-accent-grad-soft text-[11px] font-bold text-accent">
              {i + 1}
            </span>
            <span className="min-w-0">
              <Trans k={`setup.run.${k}`} tags={SETUP_TAGS} />
            </span>
          </li>
        ))}
      </ol>
      <p className="mt-3 rounded-[var(--radius-md)] border border-border bg-glass px-3 py-2 text-[12px] text-text-muted">
        <Trans k="setup.run.smartscreen" tags={SETUP_TAGS} />
      </p>
    </div>
  )
}
