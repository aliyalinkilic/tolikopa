import { useState } from 'react'
import { Info, Plus, X } from 'lucide-react'
import { Button } from '@/components/ui/Button'
import { Card, CardHeader } from '@/components/ui/Card'
import { Input } from '@/components/ui/Input'
import { FilterChip } from '@/components/FilterChip'
import type { TagUsage } from '@/lib/remediation'
import { cn } from '@/lib/cn'
import { useT } from '@/i18n'
import { sameText } from '@/lib/format'

interface TagExclusionEditorProps {
  value: string[]
  onChange: (tags: string[]) => void
  /** Tags in use across the fleet — derived client-side, see the note below. */
  universe: TagUsage[]
  excludedMachines: number
  disabled?: boolean
}

const same = sameText
// One fixed fold for the "known tag" lookup (see lib/remediation.ts norm()).
const fold = (s: string) => s.toLocaleLowerCase('tr')

/** Machines carrying any of these tags are never touched by the policy. */
export function TagExclusionEditor({
  value,
  onChange,
  universe,
  excludedMachines,
  disabled,
}: TagExclusionEditorProps) {
  const t = useT()
  const [input, setInput] = useState('')

  const toggle = (tag: string) => {
    if (value.some((x) => same(x, tag))) onChange(value.filter((x) => !same(x, tag)))
    else onChange([...value, tag])
  }

  const add = () => {
    const v = input.trim()
    if (!v) return
    if (!value.some((x) => same(x, v))) onChange([...value, v])
    setInput('')
  }

  const known = new Set(universe.map((u) => fold(u.tag)))

  return (
    <Card>
      <CardHeader
        title={t('removal.tags.title')}
        description={t('removal.tags.body')}
        action={
          <span className="tabular text-[13px] text-text-muted">
            {t('removal.tags.excludedCount', { count: excludedMachines })}
          </span>
        }
      />

      <div className="mt-4 space-y-3">
        <div>
          <span className="mb-1.5 block text-[12px] font-medium text-text-muted">{t('removal.tags.selected')}</span>
          <div className="flex flex-wrap items-center gap-1.5">
            {value.length === 0 && (
              <span className="text-[13px] text-text-dim">
                {t('removal.tags.noneSelected')}
              </span>
            )}
            {value.map((v) => (
              <span
                key={v}
                className={cn(
                  'inline-flex items-center gap-1 rounded-full border py-0.5 pl-2.5 pr-1 text-[12px] font-medium',
                  known.has(fold(v))
                    ? 'border-warn/25 bg-warn/12 text-warn'
                    : 'border-border-2 bg-surface/60 text-text-muted',
                )}
                title={
                  known.has(fold(v))
                    ? undefined
                    : t('removal.tags.unusedTag')
                }
              >
                {v}
                <button
                  type="button"
                  disabled={disabled}
                  onClick={() => onChange(value.filter((x) => x !== v))}
                  aria-label={t('removal.tags.stopExcluding', { tag: v })}
                  className="grid size-4 place-items-center rounded-full transition-colors hover:bg-wash-3 ring-focus disabled:opacity-40"
                >
                  <X className="size-3" aria-hidden />
                </button>
              </span>
            ))}
          </div>
        </div>

        {universe.length > 0 && (
          <div>
            <span className="mb-1.5 block text-[12px] font-medium text-text-muted">{t('removal.tags.fleetTags')}</span>
            <div className="flex flex-wrap gap-1.5" role="group" aria-label={t('removal.tags.fleetTagsGroup')}>
              {universe.map((u) => (
                <FilterChip
                  key={u.tag}
                  active={value.some((x) => same(x, u.tag))}
                  disabled={disabled}
                  onClick={() => toggle(u.tag)}
                  count={u.machines}
                  ariaLabel={t('removal.tags.exclude', { tag: u.tag, count: u.machines })}
                >
                  {u.tag}
                </FilterChip>
              ))}
            </div>
          </div>
        )}

        <div className="flex items-start gap-2">
          <Input
            value={input}
            disabled={disabled}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                add()
              }
            }}
            placeholder={t('removal.tags.addPlaceholder')}
            aria-label={t('removal.tags.addLabel')}
          />
          <Button
            variant="secondary"
            disabled={disabled || !input.trim()}
            leftIcon={<Plus className="size-4" aria-hidden />}
            onClick={add}
          >
            {t('common.add')}
          </Button>
        </div>

        <p className="flex items-start gap-2 text-[12px] text-text-dim">
          <Info className="mt-px size-3.5 shrink-0" aria-hidden />
          <span>
            {t('removal.tags.footnote')}
          </span>
        </p>
      </div>
    </Card>
  )
}
