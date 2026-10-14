import { useId } from 'react'
import { AlertTriangle, Lock } from 'lucide-react'
import { Badge } from '@/components/ui/Badge'
import { Card, CardHeader } from '@/components/ui/Card'
import { REMOVAL_MODE_ORDER, removalModeMeta } from '@/lib/labels'
import type { RemovalMode } from '@/lib/labels'
import { relativeTime } from '@/lib/format'
import { cn } from '@/lib/cn'
import { useT } from '@/i18n'
import { Trans } from '@/i18n/rich'

interface ModeControlProps {
  mode: RemovalMode
  /** The mode currently stored on the server — what the fleet is actually doing. */
  savedMode: RemovalMode
  onChange: (mode: RemovalMode) => void
  /** Turkish reason why "uygulama" is not selectable yet; null = selectable. */
  enforceBlockedReason: string | null
  lastDryRunAt?: string | null
  disabled?: boolean
}

const modeTileTone: Record<RemovalMode, string> = {
  off: 'border-border-2 bg-surface-2/40',
  dry_run: 'border-info/40 bg-info/[0.08]',
  enforce: 'border-crit/40 bg-crit/[0.08]',
}

/** Off / Dry Run / Enforce. Enforce is interlocked behind an observed dry run:
 *  the panel refuses to let an admin go straight to destructive mode. */
export function ModeControl({
  mode,
  savedMode,
  onChange,
  enforceBlockedReason,
  lastDryRunAt,
  disabled,
}: ModeControlProps) {
  const t = useT()
  const name = useId()
  const savedMeta = removalModeMeta(savedMode)
  const willEnforce = mode === 'enforce' && savedMode !== 'enforce'

  return (
    <Card>
      <CardHeader
        title={t('removal.modeControl.title')}
        description={t('removal.modeControl.body')}
        action={
          <Badge tone={savedMeta.token} dot>
            {t('removal.modeControl.saved', { mode: savedMeta.label })}
          </Badge>
        }
      />

      <div role="radiogroup" aria-label={t('removal.modeControl.title')} className="mt-4 grid gap-2.5 sm:grid-cols-3">
        {REMOVAL_MODE_ORDER.map((m) => {
          const meta = removalModeMeta(m)
          const blocked = m === 'enforce' && enforceBlockedReason !== null
          const active = mode === m
          return (
            <label
              key={m}
              className={cn(
                'ring-focus-within relative flex flex-col rounded-[var(--radius-md)] border p-3.5 transition-colors',
                active ? modeTileTone[m] : 'border-border bg-surface-2/20 hover:bg-surface-2/50',
                blocked || disabled ? 'cursor-not-allowed opacity-60' : 'cursor-pointer',
              )}
            >
              <input
                type="radio"
                name={name}
                className="sr-only"
                checked={active}
                disabled={blocked || disabled}
                onChange={() => onChange(m)}
              />
              <span className="flex items-center gap-2">
                <span
                  className={cn(
                    'grid size-4 place-items-center rounded-full border transition-colors',
                    active ? 'border-accent' : 'border-border-2',
                  )}
                  aria-hidden
                >
                  {active && <span className="size-2 rounded-full bg-accent" />}
                </span>
                <span className="text-[14px] font-semibold text-text">{meta.label}</span>
                {blocked && <Lock className="size-3.5 text-text-dim" aria-hidden />}
              </span>
              <span className="mt-1.5 text-[12px] leading-[1.45] text-text-muted">{meta.description}</span>
              {blocked && (
                <span className="mt-2 text-[12px] font-medium text-warn">{enforceBlockedReason}</span>
              )}
            </label>
          )
        })}
      </div>

      {willEnforce && (
        <p className="mt-3 flex items-start gap-2 rounded-[var(--radius-md)] border border-crit/25 bg-crit/[0.07] px-3.5 py-2.5 text-[13px] text-text-muted">
          <AlertTriangle className="mt-px size-4 shrink-0 text-crit" aria-hidden />
          <span>
            {t('removal.modeControl.enforceWarning')}
          </span>
        </p>
      )}

      {lastDryRunAt && (
        <p className="mt-3 text-[12px] text-text-dim">
          <Trans k="removal.modeControl.lastDryRun" values={{ when: relativeTime(lastDryRunAt) }} tags={{ b: (c) => <span className="text-text-muted">{c}</span> }} />
        </p>
      )}

      {/* Mode changes are announced; a destructive switch must not be silent. */}
      <p role="status" aria-live="polite" className="sr-only">
        {mode === 'enforce'
          ? t('removal.modeControl.liveEnforce')
          : t('removal.modeControl.liveMode', { mode: removalModeMeta(mode).label })}
      </p>
    </Card>
  )
}
