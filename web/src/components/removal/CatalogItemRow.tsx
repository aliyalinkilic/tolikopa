import { AlertTriangle, Info } from 'lucide-react'
import { Badge } from '@/components/ui/Badge'
import { Checkbox } from '@/components/ui/Checkbox'
import { presenceLabel } from '@/lib/remediation'
import { removalScopeLabel, riskTierMeta } from '@/lib/labels'
import { cn } from '@/lib/cn'
import type { RemediationCatalogItem } from '@/lib/types'
import { useT } from '@/i18n'

interface CatalogItemRowProps {
  item: RemediationCatalogItem
  selected: boolean
  onToggle: (next: boolean) => void
  disabled?: boolean
}

/** One catalog entry: what it is, how risky it is, and how much of the fleet it
 *  actually touches. The presence count is the honesty anchor — an unknown count
 *  says so rather than rendering a confident zero. */
export function CatalogItemRow({ item, selected, onToggle, disabled }: CatalogItemRowProps) {
  const t = useT()
  const risk = riskTierMeta(item.risk_tier)
  const warn = item.risk_tier === 'review' || item.risk_tier === 'advanced'
  const scope = removalScopeLabel(item.scope)
  const presence = presenceLabel(item)
  const unknownPresence = item.present_count === null || item.present_count === undefined

  return (
    <li
      className={cn(
        'rounded-[var(--radius-md)] border px-3.5 py-3 transition-colors',
        selected ? 'border-accent/25 bg-accent/[0.06]' : 'border-transparent hover:bg-surface-2/50',
      )}
    >
      <div className="flex items-start gap-3">
        <Checkbox
          size="sm"
          checked={selected}
          onChange={onToggle}
          disabled={disabled}
          ariaLabel={`${item.display_name} — ${presence}`}
          className="mt-0.5"
        />

        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className="text-[14px] font-medium text-text">{item.display_name}</span>
            <Badge tone={risk.token} title={risk.description}>
              {risk.label}
            </Badge>
            {item.safe_default && <Badge tone="neutral">{t('removal.catalog.safeDefault')}</Badge>}
            {item.deprecated && <Badge tone="warn">{t('removal.catalog.deprecated')}</Badge>}
          </div>

          <p className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[12px] text-text-dim">
            <span className="truncate font-mono">{item.family}</span>
            {scope && <span className="text-text-dim">· {scope}</span>}
          </p>

          {item.notes && (
            <p
              className={cn(
                'mt-2 flex items-start gap-1.5 rounded-[10px] border px-2.5 py-1.5 text-[12px]',
                warn ? 'border-warn/25 bg-warn/[0.07] text-text-muted' : 'border-border bg-glass text-text-muted',
              )}
            >
              {warn ? (
                <AlertTriangle className="mt-px size-3.5 shrink-0 text-warn" aria-hidden />
              ) : (
                <Info className="mt-px size-3.5 shrink-0 text-text-dim" aria-hidden />
              )}
              <span>{item.notes}</span>
            </p>
          )}
        </div>

        <div className="w-[132px] shrink-0 text-right">
          {unknownPresence ? (
            <span className="text-[12px] text-text-dim">{t('removal.presence.unknown')}</span>
          ) : (
            <>
              <span className="tabular block text-[15px] font-semibold text-text">{item.present_count}</span>
              <span className="block text-[11px] text-text-dim">
                {t(item.kind === 'startup' ? 'removal.presence.startupUnit' : 'removal.presence.appUnit', { count: item.present_count ?? 0 })}
              </span>
              {typeof item.reported_count === 'number' && item.reported_count > 0 && (
                <span
                  className="mt-1.5 block h-1 overflow-hidden rounded-full bg-wash-2"
                  title={t('removal.presence.reported', { count: item.reported_count })}
                  aria-hidden
                >
                  <span
                    className="block h-full rounded-full bg-accent/70"
                    style={{
                      width: `${Math.min(100, Math.round(((item.present_count ?? 0) / item.reported_count) * 100))}%`,
                    }}
                  />
                </span>
              )}
            </>
          )}
        </div>
      </div>
    </li>
  )
}
