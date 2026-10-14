import { Check, Info, ShieldAlert, TriangleAlert } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { Link } from 'react-router-dom'
import { Badge } from './ui/Badge'
import { Button } from './ui/Button'
import { EmptyState } from './EmptyState'
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

interface AlertListProps {
  alerts: AlertDTO[]
  onAck?: (id: number) => void
  ackingId?: number | null
  showMachine?: boolean
  emptyLabel?: string
}

export function AlertList({ alerts, onAck, ackingId, showMachine = true, emptyLabel }: AlertListProps) {
  const t = useT()
  if (alerts.length === 0) {
    return <EmptyState icon={Check} title={emptyLabel ?? t('ui.alertList.none')} description={t('ui.alertList.allGood')} />
  }

  return (
    <ul className="divide-y divide-border">
      {alerts.map((a) => {
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
                {!isOpen && <Badge tone="neutral">{alertStatusLabel(a.status)}</Badge>}
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
}
