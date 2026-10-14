import { ArrowRight, Minus, Plus, Tag } from 'lucide-react'
import { Badge } from '@/components/ui/Badge'
import { itemKey } from '@/lib/remediation'
import type { RemovalDiff } from '@/lib/remediation'
import { removalKindLabel, removalModeMeta } from '@/lib/labels'
import { cn } from '@/lib/cn'
import type { RemediationCatalogItem } from '@/lib/types'
import { useT } from '@/i18n'

interface PolicyDiffProps {
  diff: RemovalDiff
  index: Map<string, RemediationCatalogItem>
  className?: string
}

/** What exactly is about to change, in the admin's words — shown both in the save
 *  bar and again inside the confirm dialog so the two can never disagree. */
export function PolicyDiff({ diff, index, className }: PolicyDiffProps) {
  const t = useT()
  const nameOf = (kind: string, id: string) => {
    const item = index.get(itemKey(kind, id))
    return item ? item.display_name : id
  }

  if (!diff.changed) {
    return <p className={cn('text-[13px] text-text-dim', className)}>{t('removal.diff.none')}</p>
  }

  return (
    <div className={cn('space-y-3 text-[13px]', className)}>
      {diff.modeChanged && (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-text-muted">{t('removal.diff.mode')}</span>
          <Badge tone={removalModeMeta(diff.modeFrom).token}>{removalModeMeta(diff.modeFrom).label}</Badge>
          <ArrowRight className="size-3.5 text-text-dim" aria-hidden />
          <Badge tone={removalModeMeta(diff.modeTo).token} dot>
            {removalModeMeta(diff.modeTo).label}
          </Badge>
        </div>
      )}

      {diff.added.length > 0 && (
        <ChangeList
          tone="add"
          title={t('removal.diff.added', { count: diff.added.length })}
          entries={diff.added.map((e) => `${nameOf(e.kind, e.item_id)} · ${removalKindLabel(e.kind)}`)}
        />
      )}

      {diff.removed.length > 0 && (
        <ChangeList
          tone="remove"
          title={t('removal.diff.removed', { count: diff.removed.length })}
          entries={diff.removed.map((e) => `${nameOf(e.kind, e.item_id)} · ${removalKindLabel(e.kind)}`)}
        />
      )}

      {(diff.tagsAdded.length > 0 || diff.tagsRemoved.length > 0) && (
        <div className="flex flex-wrap items-center gap-2">
          <Tag className="size-3.5 text-text-dim" aria-hidden />
          <span className="text-text-muted">{t('removal.diff.tags')}</span>
          {diff.tagsAdded.map((x) => (
            <span key={`+${x}`} className="rounded-full border border-ok/25 bg-ok/10 px-2 py-0.5 text-[12px] text-ok">
              + {x}
            </span>
          ))}
          {diff.tagsRemoved.map((x) => (
            <span key={`-${x}`} className="rounded-full border border-crit/25 bg-crit/10 px-2 py-0.5 text-[12px] text-crit">
              − {x}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}

function ChangeList({ tone, title, entries }: { tone: 'add' | 'remove'; title: string; entries: string[] }) {
  const Icon = tone === 'add' ? Plus : Minus
  return (
    <div>
      <p
        className={cn(
          'flex items-center gap-1.5 text-[12px] font-semibold uppercase tracking-[0.06em]',
          tone === 'add' ? 'text-crit' : 'text-ok',
        )}
      >
        <Icon className="size-3.5" aria-hidden />
        {title}
      </p>
      <ul className="mt-1.5 space-y-1">
        {entries.map((e) => (
          <li key={e} className="flex items-start gap-2 text-text-muted">
            <span
              className={cn('mt-[7px] size-1.5 shrink-0 rounded-full', tone === 'add' ? 'bg-crit' : 'bg-ok')}
              aria-hidden
            />
            <span className="min-w-0">{e}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}
