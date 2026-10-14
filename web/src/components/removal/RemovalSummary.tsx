import { AlertTriangle, PackageX, ShieldOff, Signal, Target } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { Card } from '@/components/ui/Card'
import { cn } from '@/lib/cn'
import type { BlastRadius } from '@/lib/remediation'
import type { RemediationPosture } from '@/lib/types'
import { useT } from '@/i18n'
import { Trans } from '@/i18n/rich'

interface RemovalSummaryProps {
  posture: RemediationPosture
  radius: BlastRadius
  /** False while /api/fleet is still loading — counts would be misleading. */
  fleetKnown: boolean
}

/**
 * Blast radius + the two posture signals that explain silent no-ops. Without
 * these an admin ticks twenty apps, saves, and watches nothing happen.
 */
export function RemovalSummary({ posture, radius, fleetKnown }: RemovalSummaryProps) {
  const t = useT()
  const drift = posture.drift.stale_machines
  const killed = posture.kill_switch_off

  return (
    <div className="space-y-3">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Tile
          icon={Target}
          label={t('removal.summary.targeted')}
          value={fleetKnown ? radius.targeted : null}
          tone="accent"
          hint={
            fleetKnown
              ? t('removal.summary.targetedHint', { fleet: radius.fleet, excluded: radius.excluded, online: radius.targetedOnline })
              : t('removal.summary.fleetLoading')
          }
        />
        <Tile
          icon={Signal}
          label={t('removal.summary.reporting')}
          value={posture.reporting_machines}
          tone="neutral"
          hint={t('removal.summary.reportingHint', { count: posture.applied_machines })}
        />
        <Tile
          icon={PackageX}
          label={t('removal.summary.drift')}
          value={drift}
          tone={drift > 0 ? 'warn' : 'ok'}
          hint={
            drift > 0
              ? t('removal.summary.driftHint', { count: drift })
              : t('removal.summary.driftNone')
          }
        />
        <Tile
          icon={ShieldOff}
          label={t('removal.summary.killed')}
          value={killed}
          tone={killed > 0 ? 'warn' : 'ok'}
          hint={
            killed > 0
              ? t('removal.summary.killedHint', { count: killed })
              : t('removal.summary.killedNone')
          }
        />
      </div>

      {(drift > 0 || killed > 0) && (
        <p className="flex items-start gap-2 rounded-[var(--radius-md)] border border-warn/25 bg-warn/[0.06] px-3.5 py-2.5 text-[13px] text-text-muted">
          <AlertTriangle className="mt-px size-4 shrink-0 text-warn" aria-hidden />
          <span>
            <Trans k="removal.summary.silentNote" tags={{ b: (c) => <strong className="font-medium text-text">{c}</strong> }} />
          </span>
        </p>
      )}
    </div>
  )
}

const toneClass: Record<'accent' | 'ok' | 'warn' | 'neutral', string> = {
  accent: 'text-accent',
  ok: 'text-ok',
  warn: 'text-warn',
  neutral: 'text-text',
}

function Tile({
  icon: Icon,
  label,
  value,
  hint,
  tone,
}: {
  icon: LucideIcon
  label: string
  value: number | null
  hint: string
  tone: 'accent' | 'ok' | 'warn' | 'neutral'
}) {
  return (
    <Card>
      <div className="flex items-center gap-2">
        <Icon className={cn('size-4', toneClass[tone])} aria-hidden />
        <p className="text-[11px] font-semibold uppercase tracking-[0.08em] text-text-dim">{label}</p>
      </div>
      <p className={cn('tabular mt-2 text-[26px] font-semibold leading-none', toneClass[tone])}>
        {value === null ? '—' : value}
      </p>
      <p className="mt-2 text-[12px] leading-[1.45] text-text-muted">{hint}</p>
    </Card>
  )
}
