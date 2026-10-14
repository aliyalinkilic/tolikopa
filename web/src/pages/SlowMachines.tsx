import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { motion } from 'motion/react'
import { Info, ShieldCheck } from 'lucide-react'
import { PageHeader } from '@/components/AppShell'
import { EmptyState } from '@/components/EmptyState'
import { SkeletonCard } from '@/components/Skeleton'
import { Badge } from '@/components/ui/Badge'
import { Card } from '@/components/ui/Card'
import { useFleet, useSlowMachines } from '@/hooks/queries'
import { deriveSlowMachines } from '@/lib/slowness'
import { slowReasonMeta, slowSeverityLabel, slowSeverityToken } from '@/lib/labels'
import { displayUser, formatBytes, formatPercent, relativeTime } from '@/lib/format'
import { fadeUp, staggerContainer } from '@/lib/motion'
import { cn } from '@/lib/cn'
import type { SlowMachine } from '@/lib/types'
import { useT } from '@/i18n'

const DAY_OPTIONS = [7, 14, 30] as const

export function SlowMachines() {
  const t = useT()
  const [days, setDays] = useState<number>(7)

  const report = useSlowMachines(days, 50)
  // The fleet query is already warm from the sidebar summary; it costs nothing
  // extra and lets the page render before (or without) the report endpoint.
  const fleet = useFleet()

  const usingFallback = !report.data && !report.isLoading
  const machines: SlowMachine[] = useMemo(() => {
    if (report.data) return report.data.machines
    return fleet.data ? deriveSlowMachines(fleet.data, 50) : []
  }, [report.data, fleet.data])

  const counts = useMemo(() => {
    let crit = 0
    let warn = 0
    for (const m of machines) {
      if (m.severity === 'crit') crit++
      else if (m.severity === 'warn') warn++
    }
    return { crit, warn, total: machines.length }
  }, [machines])

  const loading = report.isLoading || (usingFallback && fleet.isLoading)

  return (
    <div>
      <PageHeader
        title={t('nav.slowMachines')}
        description={t('slow.subtitle')}
        action={
          <div className="flex items-center gap-1.5" role="group" aria-label={t('slow.rangeGroup')}>
            {DAY_OPTIONS.map((d) => (
              <button
                key={d}
                type="button"
                onClick={() => setDays(d)}
                aria-pressed={days === d}
                disabled={usingFallback}
                className={cn(
                  'rounded-full border px-3 py-1 text-[13px] font-medium transition-colors ring-focus',
                  'disabled:cursor-not-allowed disabled:opacity-40',
                  days === d
                    ? 'border-accent/30 bg-accent/12 text-accent'
                    : 'border-border-2 bg-surface/60 text-text-muted hover:text-text',
                )}
              >
                {t('common.days', { count: d })}
              </button>
            ))}
          </div>
        }
      />

      {usingFallback && (
        <Card className="mb-5 border-warn/25 bg-warn/[0.06]">
          <div className="flex items-start gap-3">
            <Info className="mt-0.5 size-4 shrink-0 text-warn" aria-hidden />
            <div>
              <p className="text-[14px] font-medium text-text">{t('slow.snapshotTitle')}</p>
              <p className="mt-1 text-[13px] text-text-muted">
                {t('slow.snapshotBody')}
              </p>
            </div>
          </div>
        </Card>
      )}

      <div className="mb-6 grid gap-4 sm:grid-cols-3">
        <StatTile label={t('slowness.severity.crit')} value={counts.crit} tone="crit" />
        <StatTile label={t('slowness.severity.warn')} value={counts.warn} tone="warn" />
        <StatTile label={t('slow.evaluated')} value={counts.total} tone="neutral" />
      </div>

      {loading ? (
        <div className="grid gap-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <SkeletonCard key={i} />
          ))}
        </div>
      ) : machines.length === 0 ? (
        <EmptyState
          icon={ShieldCheck}
          title={t('slow.noneTitle')}
          description={t('slow.noneBody')}
        />
      ) : (
        <motion.ul variants={staggerContainer()} initial="hidden" animate="show" className="grid gap-3">
          {machines.map((m) => (
            <MachineRow key={m.machine_id} machine={m} />
          ))}
        </motion.ul>
      )}
    </div>
  )
}

function StatTile({ label, value, tone }: { label: string; value: number; tone: 'crit' | 'warn' | 'neutral' }) {
  const color = tone === 'crit' ? 'text-crit' : tone === 'warn' ? 'text-warn' : 'text-text'
  return (
    <Card>
      <p className="text-[11px] font-semibold uppercase tracking-[0.08em] text-text-dim">{label}</p>
      <p className={cn('tabular mt-2 text-[28px] font-semibold leading-none', color)}>{value}</p>
    </Card>
  )
}

function MachineRow({ machine: m }: { machine: SlowMachine }) {
  const t = useT()
  const tone = slowSeverityToken(m.severity)

  return (
    <motion.li variants={fadeUp}>
      <Link
        to={`/machines/${m.machine_id}`}
        className="ring-focus block rounded-[var(--radius-lg)]"
        aria-label={t('slow.rowLabel', { host: m.hostname, score: m.score })}
      >
        <Card interactive className="flex flex-col gap-4 sm:flex-row sm:items-center">
          <ScoreDial score={m.score} tone={tone} />

          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="truncate text-[15px] font-semibold text-text">{m.hostname}</h3>
              <Badge tone={tone}>{slowSeverityLabel(m.severity)}</Badge>
              {!m.online && <Badge tone="offline">{t('common.offline')}</Badge>}
            </div>

            <p className="mt-0.5 truncate text-[13px] text-text-muted">
              {m.logon_user ? displayUser(m.logon_user) : t('common.noSession')}
              <span className="text-text-dim">{t('slow.lastSeen', { when: relativeTime(m.last_seen) })}</span>
            </p>

            <ul className="mt-3 flex flex-wrap gap-1.5">
              {m.reasons.map((r) => {
                const meta = slowReasonMeta(r.key)
                const Icon = meta.icon
                const detail = meta.describe(r.value)
                return (
                  <li key={r.key}>
                    <span
                      title={t('slow.reasonPoints', { reason: meta.label, points: r.points, count: r.points })}
                      className="inline-flex items-center gap-1.5 rounded-full border border-border-2 bg-surface/60 px-2.5 py-1 text-[12px] text-text-muted"
                    >
                      <Icon className="size-3.5 text-text-dim" aria-hidden />
                      <span className="text-text">{meta.label}</span>
                      {detail && <span className="tabular text-text-dim">{detail}</span>}
                    </span>
                  </li>
                )
              })}
            </ul>
          </div>

          <dl className="grid shrink-0 grid-cols-2 gap-x-6 gap-y-1 text-[12px] sm:w-44">
            {m.mem_total > 0 && (
              <Fact label={t('common.ram')} value={formatBytes(m.mem_total, 0)} />
            )}
            {m.cpu_cores ? <Fact label={t('slow.cores')} value={String(m.cpu_cores)} /> : null}
            {m.disk_used_pct > 0 && (
              <Fact label={m.disk_mount || t('common.disk')} value={formatPercent(Math.round(m.disk_used_pct))} />
            )}
            {m.uptime_days >= 1 && (
              <Fact label={t('slow.uptime')} value={t('common.days', { count: Math.round(m.uptime_days) })} />
            )}
          </dl>
        </Card>
      </Link>
    </motion.li>
  )
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="truncate text-[11px] uppercase tracking-[0.06em] text-text-dim">{label}</dt>
      <dd className="tabular truncate font-medium text-text">{value}</dd>
    </div>
  )
}

/** Compact circular score readout. The number carries the meaning; the arc and
 *  color only reinforce it, so this stays legible without color perception. */
function ScoreDial({ score, tone }: { score: number; tone: 'ok' | 'warn' | 'crit' | 'offline' | 'info' }) {
  const t = useT()
  const pct = Math.max(0, Math.min(100, score))
  const stroke = tone === 'crit' ? 'var(--color-crit)' : tone === 'warn' ? 'var(--color-warn)' : 'var(--color-ok)'
  const r = 26
  const circ = 2 * Math.PI * r

  return (
    <div className="relative grid size-[68px] shrink-0 place-items-center">
      <svg className="absolute inset-0 -rotate-90" viewBox="0 0 68 68" aria-hidden>
        <circle cx="34" cy="34" r={r} fill="none" stroke="var(--color-border-2)" strokeWidth="5" />
        <circle
          cx="34"
          cy="34"
          r={r}
          fill="none"
          stroke={stroke}
          strokeWidth="5"
          strokeLinecap="round"
          strokeDasharray={circ}
          strokeDashoffset={circ * (1 - pct / 100)}
        />
      </svg>
      <div className="text-center leading-none">
        <span className="tabular text-[19px] font-semibold text-text">{score}</span>
        <span className="mt-0.5 block text-[9px] uppercase tracking-[0.08em] text-text-dim">{t('slow.score')}</span>
      </div>
    </div>
  )
}
