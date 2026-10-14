import { useMemo } from 'react'
import { Link } from 'react-router-dom'
import { Info, ShieldAlert, TriangleAlert } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { PageHeader } from '@/components/AppShell'
import { Card } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Select } from '@/components/ui/Select'
import { RangeSelector } from '@/components/RangeSelector'
import { DataTable } from '@/components/DataTable'
import type { Column } from '@/components/DataTable'
import { SkeletonText } from '@/components/Skeleton'
import { useAckAlert, useAlerts, useFleetSummary } from '@/hooks/queries'
import { useRange } from '@/hooks/useRange'
import { useUrlState } from '@/hooks/useUrlState'
import { alertStatusLabel, ruleLabel, severityLabel, severityToken, SEVERITY_ORDER } from '@/lib/labels'
import type { StatusToken } from '@/lib/labels'
import { formatDateTime, relativeTime } from '@/lib/format'
import type { AlertDTO } from '@/lib/types'
import { useT } from '@/i18n'
import { alertMessage } from '@/lib/alertText'
import { chart, tint } from '@/lib/theme'

const severityIcon: Record<StatusToken, LucideIcon> = {
  crit: ShieldAlert,
  warn: TriangleAlert,
  info: Info,
  ok: Info,
  offline: Info,
}

function severityRank(sev: string): number {
  const x = severityToken(sev)
  const idx = SEVERITY_ORDER.indexOf(x)
  return idx === -1 ? 99 : idx
}

export function Alerts() {
  const t = useT()
  const { key: rangeKey, setKey: setRange, range, win, custom, setCustom } = useRange('7d')
  const [status, setStatus] = useUrlState('status', '')
  const [severity, setSeverity] = useUrlState('sev', '')
  const statusOptions = [
    { value: '', label: t('alertsPage.allStatuses') },
    { value: 'open', label: t('alertStatus.open') },
    { value: 'ack', label: t('alertStatus.ack') },
    { value: 'resolved', label: t('alertStatus.resolved') },
  ]
  const severityOptions = [
    { value: '', label: t('alertsPage.allSeverities') },
    { value: 'crit', label: t('severity.crit') },
    { value: 'warn', label: t('severity.warn') },
    { value: 'info', label: t('severity.info') },
  ]

  const { data: summary } = useFleetSummary()
  const { data, isLoading } = useAlerts({ status: status || undefined, from: win.from, to: win.to })
  const ack = useAckAlert()

  const rows = useMemo(() => {
    let list = data ?? []
    if (severity) list = list.filter((a) => severityToken(a.severity) === severity)
    return list
  }, [data, severity])

  const bySev = summary?.alerts_by_severity ?? {}
  const critCount = bySev.critical ?? bySev.crit ?? 0
  const warnCount = bySev.warning ?? bySev.warn ?? 0
  const infoCount = bySev.info ?? bySev.bilgi ?? 0

  const columns: Column<AlertDTO>[] = [
    {
      key: 'severity',
      header: t('alertsPage.severity'),
      sortable: true,
      sortValue: (a) => severityRank(a.severity),
      render: (a) => {
        const tone = severityToken(a.severity)
        const Icon = severityIcon[tone]
        return (
          <Badge tone={tone} icon={<Icon className="size-3" aria-hidden />}>
            {severityLabel(a.severity)}
          </Badge>
        )
      },
    },
    {
      key: 'rule',
      header: t('alertsPage.rule'),
      sortable: true,
      sortValue: (a) => ruleLabel(a.rule),
      render: (a) => <span className="font-medium text-text">{ruleLabel(a.rule)}</span>,
    },
    {
      key: 'machine',
      header: t('common.machine'),
      sortable: true,
      sortValue: (a) => a.hostname ?? a.machine_id,
      render: (a) => (
        <Link to={`/machines/${a.machine_id}`} className="text-text-muted hover:text-accent ring-focus">
          {a.hostname ?? a.machine_id}
        </Link>
      ),
    },
    {
      key: 'message',
      header: t('alertsPage.message'),
      render: (a) => <span className="block max-w-[320px] truncate text-text-muted" title={alertMessage(a)}>{alertMessage(a)}</span>,
    },
    {
      key: 'opened',
      header: t('alertsPage.opened'),
      align: 'right',
      sortable: true,
      sortValue: (a) => new Date(a.opened_at).getTime(),
      render: (a) => (
        <span className="tabular whitespace-nowrap text-text-muted" title={formatDateTime(a.opened_at)}>
          {relativeTime(a.opened_at)}
        </span>
      ),
    },
    {
      key: 'status',
      header: t('common.status'),
      align: 'right',
      render: (a) => {
        const isOpen = a.status === 'open' || a.status === 'firing'
        return isOpen ? (
          <Button
            size="sm"
            variant="secondary"
            loading={ack.isPending && ack.variables === a.id}
            onClick={() => ack.mutate(a.id)}
          >
            {t('ui.alertList.ack')}
          </Button>
        ) : (
          <Badge tone="neutral">{alertStatusLabel(a.status)}</Badge>
        )
      },
    },
  ]

  return (
    <div>
      <PageHeader title={t('nav.alerts')} description={t('alertsPage.subtitle')} />

      {/* Severity summary strip */}
      <div className="mb-6 grid grid-cols-3 gap-4">
        <SummaryTile tone="crit" icon={ShieldAlert} label={t('severity.crit')} count={critCount} />
        <SummaryTile tone="warn" icon={TriangleAlert} label={t('severity.warn')} count={warnCount} />
        <SummaryTile tone="info" icon={Info} label={t('severity.info')} count={infoCount} />
      </div>

      <Card padded={false}>
        <div className="flex flex-col gap-3 border-b border-border p-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex flex-wrap gap-2">
            <div className="w-40">
              <Select options={statusOptions} value={status} onChange={(e) => setStatus(e.target.value)} aria-label={t('common.status')} />
            </div>
            <div className="w-44">
              <Select options={severityOptions} value={severity} onChange={(e) => setSeverity(e.target.value)} aria-label={t('alertsPage.severity')} />
            </div>
          </div>
          <RangeSelector value={rangeKey} onChange={setRange} custom={custom} onCustomChange={setCustom} />
        </div>
        <div className="p-4">
          {isLoading ? (
            <SkeletonText lines={8} />
          ) : (
            <DataTable
              columns={columns}
              rows={rows}
              getRowKey={(a) => String(a.id)}
              initialSort={{ key: 'opened', dir: 'desc' }}
              emptyLabel={t('alertsPage.empty', { range: range.label })}
              className="border-0"
              maxHeight={640}
              pageSize={20}
            />
          )}
        </div>
      </Card>
    </div>
  )
}

function SummaryTile({ tone, icon: Icon, label, count }: { tone: StatusToken; icon: LucideIcon; label: string; count: number }) {
  const color = tone === 'crit' ? chart.crit : tone === 'warn' ? chart.warn : chart.info
  return (
    <Card className="relative overflow-hidden">
      <span className="absolute inset-y-0 left-0 w-1" style={{ backgroundColor: color }} aria-hidden />
      <div className="flex items-center justify-between">
        <div>
          <p className="text-[12px] font-medium uppercase tracking-[0.1em] text-text-dim">{label}</p>
          <p className="tabular mt-1 text-[26px] font-bold text-text">{count}</p>
        </div>
        <div className="grid size-10 place-items-center rounded-[12px]" style={{ backgroundColor: tint(color, 12), color }}>
          <Icon className="size-5" aria-hidden />
        </div>
      </div>
    </Card>
  )
}
