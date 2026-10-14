import { Badge } from './ui/Badge'
import { sessionMeta } from '@/lib/labels'
import { useT } from '@/i18n'

interface StatusBadgeProps {
  online: boolean
  sessionState?: string
  pulse?: boolean
}

/**
 * One primary status pill per machine: offline wins; otherwise the session state
 * (Active / Locked / …). Color is always paired with a label + dot.
 */
export function StatusBadge({ online, sessionState, pulse = true }: StatusBadgeProps) {
  const t = useT()
  if (!online) {
    return (
      <Badge tone="offline" dot>
        {t('common.offline')}
      </Badge>
    )
  }
  const meta = sessionMeta(sessionState ?? 'active')
  return (
    <Badge tone={meta.token} dot pulse={pulse && meta.token === 'ok'}>
      {meta.label}
    </Badge>
  )
}
