import { useState } from 'react'
import { Check, Info, ShieldAlert, TriangleAlert } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { Link } from 'react-router-dom'
import { Badge } from './ui/Badge'
import { Button } from './ui/Button'
import { EmptyState } from './EmptyState'
import { Pager } from './Pager'
import { alertStatusLabel, ruleLabel, severityLabel, severityToken, statusColor } from '@/lib/labels'
import type { StatusToken } from '@/lib/labels'
import { alertMessage } from '@/lib/alertText'
import { formatDateTime, relativeTime } from '@/lib/format'
import { tint } from '@/lib/theme'
import { useT } from '@/i18n'
import type { AlertDTO } from '@/lib/types'

const severityIcon: Record<StatusToken, LucideIcon> = {
  crit: ShieldAlert,
  warn: TriangleAlert,
  info: Info,
  ok: Info,
  offline: Info,
}

/** Closed by the engine because work hours ended (detail.resolved_reason). */
function workHoursEnd(a: AlertDTO): boolean {
  return a.status === 'resolved' && (a.params?.resolved_reason ?? a.detail?.resolved_reason) === 'work_hours_end'
}

interface AlertListProps {
  alerts: AlertDTO[]
  onAck?: (id: number) => void
  ackingId?: number | null
  showMachine?: boolean
  emptyLabel?: string
  /** When set, show one page of alerts at a time with a pager below. */
  pageSize?: number
}

export function AlertList({ alerts, onAck, ackingId, showMachine = true, emptyLabel, pageSize }: AlertListProps) {
  const t = useT()
  const [page, setPage] = useState(1)
  if (alerts.length === 0) {
    return <EmptyState icon={Check} title={emptyLabel ?? t('ui.alertList.none')} description={t('ui.alertList.allGood')} />
  }

  const size = pageSize ?? 0
  const pageCount = size > 0 ? Math.max(1, Math.ceil(alerts.length / size)) : 1
  // Clamp rather than reset: an ack or a refetch can shorten the list under the reader.
  const safePage = Math.min(page, pageCount)
  const visible = size > 0 ? alerts.slice((safePage - 1) * size, safePage * size) : alerts

  const list = (
    <ul className="divide-y divide-border">
      {visible.map((a) => {
        const tone = severityToken(a.severity)
        const Icon = severityIcon[tone]
        const color = statusColor[tone]
        const isOpen = a.status === 'open' || a.status === 'firing'
        return (
          <li key={a.id} className="flex items-start gap-3 py-3 first:pt-0 last:pb-0">
            <div
              className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-[10px]"
              style={{ backgroundColor: tint(color, 12), color }}
            >
              <Icon className="size-4" aria-hidden />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                <span className="text-[14px] font-semibold text-text">{ruleLabel(a.rule)}</span>
                <Badge tone={tone}>{severityLabel(a.severity)}</Badge>
                {!isOpen && (
                  <Badge tone="neutral" title={workHoursEnd(a) ? t('alertsPage.resolvedWorkHoursTitle') : undefined}>
                    {workHoursEnd(a) ? t('alertsPage.resolvedWorkHours') : alertStatusLabel(a.status)}
                  </Badge>
                )}
              </div>
              <p className="mt-0.5 truncate text-[13px] text-text-muted">{alertMessage(a)}</p>
              <div className="mt-1 flex flex-wrap items-center gap-x-2 text-[12px] text-text-dim">
                {showMachine && a.hostname && (
                  <Link
                    to={`/machines/${a.machine_id}`}
                    className="font-medium text-text-muted hover:text-accent"
                  >
                    {a.hostname}
                  </Link>
                )}
                {showMachine && a.hostname && <span aria-hidden>·</span>}
                <span title={formatDateTime(a.opened_at)}>{relativeTime(a.opened_at)}</span>
              </div>
            </div>
            {onAck && isOpen && (
              <Button
                size="sm"
                variant="secondary"
                loading={ackingId === a.id}
                onClick={() => onAck(a.id)}
                leftIcon={<Check className="size-3.5" aria-hidden />}
                className="shrink-0"
              >
                {t('ui.alertList.ack')}
              </Button>
            )}
          </li>
        )
      })}
    </ul>
  )

  if (size === 0 || alerts.length <= size) return list
  return (
    <div className="space-y-4">
      {list}
      <Pager page={safePage} pageSize={size} total={alerts.length} onPage={setPage} />
    </div>
  )
}
