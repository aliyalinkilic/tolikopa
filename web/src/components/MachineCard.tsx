import { useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'
import { motion, useAnimationControls, useReducedMotion } from 'motion/react'
import { Battery, BatteryCharging, Download, Monitor, RefreshCw, Server, TriangleAlert, User as UserIcon } from 'lucide-react'
import { RingGauge } from './RingGauge'
import { StatusBadge } from './StatusBadge'
import { Badge } from './ui/Badge'
import { fadeUp, springy } from '@/lib/motion'
import { BATTERY_CHARGE_THRESHOLDS, CPU_THRESHOLDS, DISK_USED_THRESHOLDS, RAM_THRESHOLDS, metricTone } from '@/lib/theme'
import { displayUser, relativeTime } from '@/lib/format'
import { cn } from '@/lib/cn'
import type { BatteryDTO, FleetMachine } from '@/lib/types'
import { useT } from '@/i18n'
import { formatPercent } from '@/lib/format'

interface MachineCardProps {
  machine: FleetMachine
  pulse?: number
}

export function MachineCard({ machine, pulse = 0 }: MachineCardProps) {
  const t = useT()
  const reduce = useReducedMotion()
  const flash = useAnimationControls()
  const mounted = useRef(false)

  useEffect(() => {
    if (!mounted.current) {
      mounted.current = true
      return
    }
    if (reduce) return
    flash.set({ opacity: 0.9 })
    flash.start({ opacity: 0, transition: { duration: 0.7, ease: 'easeOut' } })
  }, [pulse, reduce, flash])

  const KindIcon = machine.kind === 'linux' ? Server : Monitor
  const diskUsed = 100 - (machine.disk_min_free_pct ?? 100)
  const memPct = (machine.mem_ratio ?? 0) * 100
  const tags = machine.tags ?? []
  const pendingUpdates = machine.pending_updates ?? 0
  const rebootPending = machine.reboot_pending === true
  const battery = machine.battery ?? null

  return (
    <motion.li layout variants={fadeUp} transition={{ layout: springy }} className="list-none">
      <div
        className={cn(
          'card relative overflow-hidden p-4 transition-colors duration-200',
          machine.online ? 'hover:bg-surface-2' : 'opacity-80',
        )}
      >
        <motion.span
          aria-hidden
          initial={{ opacity: 0 }}
          animate={flash}
          className="pointer-events-none absolute inset-0 rounded-[inherit] ring-2 ring-accent/70"
        />

        <div className="flex items-start justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2.5">
            <div className="grid size-8 shrink-0 place-items-center rounded-[10px] bg-glass text-text-muted">
              <KindIcon className="size-4" aria-hidden />
            </div>
            <div className="min-w-0">
              <h3 className="truncate text-[15px] font-semibold text-text">{machine.hostname}</h3>
              <p className="flex items-center gap-1 truncate text-[12px] text-text-muted">
                <UserIcon className="size-3 shrink-0" aria-hidden />
                {displayUser(machine.logon_user)}
              </p>
            </div>
          </div>
          <StatusBadge online={machine.online} sessionState={machine.session_state} />
        </div>

        {tags.length > 0 && (
          <div className="mt-3 flex flex-wrap items-center gap-1">
            {tags.slice(0, 3).map((tag) => (
              <span
                key={tag}
                title={tag}
                className="max-w-[9rem] truncate rounded-full border border-border-2 bg-surface/60 px-2 py-0.5 text-[11px] font-medium text-text-muted"
              >
                {tag}
              </span>
            ))}
            {tags.length > 3 && (
              <span className="tabular text-[11px] text-text-dim">+{tags.length - 3}</span>
            )}
          </div>
        )}

        {(pendingUpdates > 0 || rebootPending || battery?.present === true) && (
          <div className="mt-3 flex flex-wrap items-center gap-1.5">
            {pendingUpdates > 0 && (
              <Badge tone="warn" icon={<Download className="size-3" aria-hidden />}>
                <span className="tabular">{t('ui.machineCard.updates', { count: pendingUpdates })}</span>
              </Badge>
            )}
            {rebootPending && (
              <Badge tone="warn" icon={<RefreshCw className="size-3" aria-hidden />}>
                {t('ui.machineCard.reboot')}
              </Badge>
            )}
            {battery?.present === true && <BatteryChip battery={battery} />}
          </div>
        )}

        <div className="mt-4 grid grid-cols-3 place-items-center gap-2">
          <RingGauge value={machine.cpu_pct} label={t('common.cpu')} thresholds={CPU_THRESHOLDS} size={80} stroke={7} />
          <RingGauge value={memPct} label={t('common.ram')} thresholds={RAM_THRESHOLDS} size={80} stroke={7} />
          <RingGauge value={diskUsed} label={t('common.disk')} thresholds={DISK_USED_THRESHOLDS} size={80} stroke={7} />
        </div>

        <div className="mt-4 flex items-center justify-between gap-2 text-[12px]">
          <span className="truncate text-text-dim">
            {machine.online ? t('common.live') : t('common.lastSeenAgo', { when: relativeTime(machine.last_seen) })}
          </span>
          {machine.open_alerts > 0 ? (
            <Badge tone="crit" icon={<TriangleAlert className="size-3" aria-hidden />}>
              {t('common.alerts', { count: machine.open_alerts })}
            </Badge>
          ) : (
            <Badge tone="ok">{t('common.noIssues')}</Badge>
          )}
        </div>

        <Link
          to={`/machines/${machine.machine_id}`}
          aria-label={t('ui.machineCard.openDetail', { host: machine.hostname })}
          className="absolute inset-0 rounded-[inherit] ring-focus"
        />
      </div>
    </motion.li>
  )
}

/** Compact charge indicator for laptops (present === true). Tinted by charge tone. */
function BatteryChip({ battery }: { battery: BatteryDTO }) {
  const percent = battery.percent ?? null
  const tone = percent == null ? 'neutral' : metricTone(percent, BATTERY_CHARGE_THRESHOLDS)
  const Icon = battery.charging ? BatteryCharging : Battery
  return (
    <Badge tone={tone} icon={<Icon className="size-3" aria-hidden />}>
      <span className="tabular">{percent == null ? '—' : formatPercent(Math.round(percent))}</span>
    </Badge>
  )
}
