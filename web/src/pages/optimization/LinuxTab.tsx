import { useMemo } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { motion } from 'motion/react'
import {
  Check, Cpu, HardDrive, Lightbulb, MemoryStick, RotateCw, Server, ShieldAlert, Terminal, TriangleAlert,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { Badge } from '@/components/ui/Badge'
import { Card, CardHeader } from '@/components/ui/Card'
import { DataTable } from '@/components/DataTable'
import type { Column } from '@/components/DataTable'
import { EmptyState } from '@/components/EmptyState'
import { KpiCard } from '@/components/KpiCard'
import { SkeletonCard } from '@/components/Skeleton'
import { fadeUp, staggerContainer } from '@/lib/motion'
import { formatPercent, formatRatioPercent } from '@/lib/format'
import type { LinuxAdvice, LinuxAdvisoryMachine } from '@/lib/types'
import { useT } from '@/i18n'
import { CapabilityMatrix } from './CapabilityMatrix'
import type { TKey } from '@/i18n'

/** Must match server advisoryDays (remediation_posture.go). */
const ADVISORY_DAYS = 7

const ADVICE_ORDER = ['smart', 'disk_critical', 'disk_low', 'mem_pressure', 'cpu_sustained', 'long_uptime'] as const
type AdviceKey = (typeof ADVICE_ORDER)[number]

const ADVICE_ICON: Record<AdviceKey, LucideIcon> = {
  smart: ShieldAlert,
  disk_critical: HardDrive,
  disk_low: HardDrive,
  mem_pressure: MemoryStick,
  cpu_sustained: Cpu,
  long_uptime: RotateCw,
}

const isAdviceKey = (k: string): k is AdviceKey => (ADVICE_ORDER as readonly string[]).includes(k)

interface Props {
  machines: LinuxAdvisoryMachine[] | undefined
  isLoading: boolean
  isError: boolean
}

/**
 * Linux hosts: ADVISORY ONLY. The Linux agent has no hygiene backend (its
 * actuator is a fail-closed stub) and the server never pushes anything to a host,
 * so this view only turns metrics the agent already sends into recommendations an
 * admin can act on by hand.
 */
export function LinuxTab({ machines, isLoading, isError }: Props) {
  const t = useT()
  const navigate = useNavigate()
  const hosts = useMemo(() => machines ?? [], [machines])

  const groups = useMemo(() => {
    const byKey = new Map<AdviceKey, { host: LinuxAdvisoryMachine; advice: LinuxAdvice }[]>()
    for (const host of hosts) {
      for (const advice of host.advice) {
        if (!isAdviceKey(advice.key)) continue
        const list = byKey.get(advice.key) ?? []
        list.push({ host, advice })
        byKey.set(advice.key, list)
      }
    }
    return ADVICE_ORDER.filter((k) => byKey.has(k)).map((k) => ({ key: k, entries: byKey.get(k)! }))
  }, [hosts])

  const withAdvice = hosts.filter((h) => h.advice.length > 0).length
  const critical = hosts.filter((h) => h.advice.some((a) => a.severity === 'crit')).length

  const valueText = (a: LinuxAdvice, host: LinuxAdvisoryMachine): string => {
    switch (a.key) {
      case 'disk_critical':
      case 'disk_low':
        return t('optimization.linux.valueDisk', { mount: host.disk_mount || '/', value: formatPercent(a.value) })
      case 'mem_pressure':
        return formatRatioPercent(a.value)
      case 'cpu_sustained':
        return formatPercent(a.value)
      case 'long_uptime':
        return t('optimization.linux.uptimeDays', { count: Math.floor(a.value) })
      default:
        return ''
    }
  }

  const columns: Column<LinuxAdvisoryMachine>[] = [
    {
      key: 'host',
      header: t('optimization.linux.colHost'),
      sortable: true,
      sortValue: (r) => r.hostname,
      render: (r) => (
        <span className="flex items-center gap-2">
          <span
            className={r.online ? 'size-2 rounded-full bg-ok' : 'size-2 rounded-full bg-offline'}
            aria-label={r.online ? t('optimization.linux.online') : t('optimization.linux.offline')}
            role="img"
          />
          <span className="font-medium text-text">{r.hostname || r.machine_id}</span>
        </span>
      ),
    },
    {
      key: 'cpu',
      header: t('optimization.linux.colCpu'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.cpu_avg ?? -1,
      render: (r) => <span className="tabular text-text-muted">{r.cpu_avg === null ? '—' : formatPercent(r.cpu_avg)}</span>,
    },
    {
      key: 'mem',
      header: t('optimization.linux.colMem'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.mem_ratio_avg ?? -1,
      render: (r) => (
        <span className="tabular text-text-muted">{r.mem_ratio_avg === null ? '—' : formatRatioPercent(r.mem_ratio_avg)}</span>
      ),
    },
    {
      key: 'disk',
      header: t('optimization.linux.colDisk'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.disk_used_pct ?? -1,
      render: (r) => (
        <span className="tabular text-text-muted">
          {r.disk_used_pct === null ? '—' : t('optimization.linux.valueDisk', { mount: r.disk_mount || '/', value: formatPercent(r.disk_used_pct) })}
        </span>
      ),
    },
    {
      key: 'uptime',
      header: t('optimization.linux.colUptime'),
      align: 'right',
      sortable: true,
      sortValue: (r) => r.uptime_days,
      render: (r) => (
        <span className="tabular text-text-muted">
          {r.uptime_days > 0 ? t('optimization.linux.uptimeDays', { count: Math.floor(r.uptime_days) }) : '—'}
        </span>
      ),
    },
    {
      key: 'advice',
      header: t('optimization.linux.colAdvice'),
      sortable: true,
      sortValue: (r) => r.advice.length,
      render: (r) =>
        r.advice.length === 0 ? (
          <span className="text-[12px] text-text-dim">
            {r.sample_hours < 12
              ? t('optimization.linux.thinSample', { hours: r.sample_hours })
              : t('optimization.linux.noAdvice')}
          </span>
        ) : (
          <span className="flex flex-wrap gap-1">
            {r.advice.filter((a) => isAdviceKey(a.key)).map((a) => (
              <Badge key={a.key} tone={a.severity === 'crit' ? 'crit' : 'warn'}>
                {t(`optimization.linux.advice.${a.key}.short` as TKey)}
              </Badge>
            ))}
          </span>
        ),
    },
  ]

  if (isError && !machines) {
    return (
      <EmptyState
        icon={Server}
        title={t('optimization.overview.loadFailedTitle')}
        description={t('optimization.overview.loadFailedBody')}
      />
    )
  }

  return (
    <div className="space-y-4">
      <Card className="border-info/25 bg-info/[0.06]">
        <div className="flex items-start gap-3">
          <Lightbulb className="mt-0.5 size-5 shrink-0 text-info" aria-hidden />
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="text-[15px] font-semibold text-text">{t('optimization.linux.introTitle')}</h3>
              <Badge tone="info">{t('optimization.linux.advisoryBadge')}</Badge>
            </div>
            <p className="mt-1 text-[13px] text-text-muted">{t('optimization.linux.introBody')}</p>
          </div>
        </div>
      </Card>

      {isLoading ? (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <SkeletonCard key={i} />
          ))}
        </div>
      ) : hosts.length === 0 ? (
        <EmptyState icon={Terminal} title={t('optimization.linux.emptyTitle')} description={t('optimization.linux.emptyBody')} />
      ) : (
        <>
          <motion.div
            variants={staggerContainer()}
            initial="hidden"
            animate="show"
            className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4"
          >
            <KpiCard label={t('optimization.linux.kpiHosts')} value={hosts.length} icon={Server} tone="info" />
            <KpiCard
              label={t('optimization.linux.kpiWithAdvice')}
              value={withAdvice}
              icon={Lightbulb}
              tone={withAdvice > 0 ? 'warn' : 'ok'}
            />
            <KpiCard
              label={t('optimization.linux.kpiCritical')}
              value={critical}
              icon={TriangleAlert}
              tone={critical > 0 ? 'crit' : 'ok'}
            />
            <KpiCard label={t('optimization.linux.kpiHealthy')} value={hosts.length - withAdvice} icon={Check} tone="ok" />
          </motion.div>

          <Card>
            <CardHeader
              title={t('optimization.linux.recTitle')}
              description={t('optimization.linux.recBody', { days: ADVISORY_DAYS })}
            />
            {groups.length === 0 ? (
              <p className="mt-4 text-[13px] text-text-muted">{t('optimization.linux.recNone')}</p>
            ) : (
              <motion.ul
                variants={staggerContainer()}
                initial="hidden"
                animate="show"
                className="mt-4 grid gap-3 lg:grid-cols-2"
              >
                {groups.map(({ key, entries }) => {
                  const Icon = ADVICE_ICON[key]
                  const crit = entries.some((e) => e.advice.severity === 'crit')
                  return (
                    <motion.li
                      key={key}
                      variants={fadeUp}
                      className="rounded-[var(--radius-md)] border border-border bg-surface-2/40 p-4"
                    >
                      <div className="flex items-start justify-between gap-3">
                        <div className="flex items-center gap-2">
                          <Icon className={crit ? 'size-4 text-crit' : 'size-4 text-warn'} aria-hidden />
                          <h4 className="text-[14px] font-semibold text-text">
                            {t(`optimization.linux.advice.${key}.title` as TKey)}
                          </h4>
                        </div>
                        <Badge tone={crit ? 'crit' : 'warn'}>
                          {t('optimization.linux.affected', { count: entries.length })}
                        </Badge>
                      </div>
                      <p className="mt-2 text-[13px] text-text-muted">
                        {t(`optimization.linux.advice.${key}.body` as TKey, { days: ADVISORY_DAYS })}
                      </p>
                      <p className="mt-2 text-[13px] text-text">
                        <span className="font-medium">{t('optimization.linux.whatToDo')} </span>
                        {t(`optimization.linux.advice.${key}.action` as TKey)}
                      </p>
                      <ul className="mt-3 flex flex-wrap gap-1.5">
                        {entries.map(({ host, advice }) => {
                          const v = valueText(advice, host)
                          return (
                            <li key={host.machine_id}>
                              <Link
                                to={`/machines/${encodeURIComponent(host.machine_id)}`}
                                className="inline-flex items-center gap-1.5 rounded-full border border-border-2 bg-wash px-2.5 py-0.5 text-[12px] text-text hover:bg-wash-2 ring-focus"
                              >
                                <span className="font-medium">{host.hostname || host.machine_id}</span>
                                {v && <span className="tabular text-text-muted">{v}</span>}
                              </Link>
                            </li>
                          )
                        })}
                      </ul>
                    </motion.li>
                  )
                })}
              </motion.ul>
            )}
          </Card>

          <Card padded={false}>
            <CardHeader
              title={t('optimization.linux.hostsTitle')}
              description={t('optimization.linux.hostsBody', { days: ADVISORY_DAYS })}
              className="p-5 pb-3"
            />
            <div className="px-5 pb-5">
              <DataTable
                columns={columns}
                rows={hosts}
                getRowKey={(r) => r.machine_id}
                onRowClick={(r) => navigate(`/machines/${encodeURIComponent(r.machine_id)}`)}
                initialSort={{ key: 'advice', dir: 'desc' }}
                pageSize={15}
                className="border-0"
              />
            </div>
          </Card>
        </>
      )}

      <CapabilityMatrix />
    </div>
  )
}
