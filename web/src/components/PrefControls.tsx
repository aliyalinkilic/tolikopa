import { useId, useRef } from 'react'
import type { KeyboardEvent, ReactNode } from 'react'
import { Monitor, Moon, Sun } from 'lucide-react'
import { LANGS, LANG_NAMES, useLang, useT } from '@/i18n'
import type { Lang } from '@/i18n'
import { THEME_PREFS, setLangPref, setThemePref, useThemePref } from '@/lib/prefs'
import type { ThemePref } from '@/lib/prefs'
import { cn } from '@/lib/cn'

interface Option<V extends string> {
  value: V
  label: string
  icon?: ReactNode
  /** BCP 47 tag of the label itself, when it differs from the page language. */
  lang?: string
}

interface SegmentedProps<V extends string> {
  label: string
  /** Hide the visible label (it stays the group's accessible name). */
  hideLabel?: boolean
  hint?: string
  options: Option<V>[]
  value: V
  onChange: (value: V) => void
  compact?: boolean
}

/** Radio group drawn as a segmented control. Arrow keys move the selection. */
export function Segmented<V extends string>({ label, hideLabel, hint, options, value, onChange, compact }: SegmentedProps<V>) {
  const id = useId()
  const refs = useRef<(HTMLButtonElement | null)[]>([])

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const idx = options.findIndex((o) => o.value === value)
    let next = idx
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = (idx + 1) % options.length
    else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') next = (idx - 1 + options.length) % options.length
    else return
    e.preventDefault()
    onChange(options[next].value)
    refs.current[next]?.focus()
  }

  return (
    <div>
      <span id={`${id}-label`} className={hideLabel ? 'sr-only' : 'mb-1.5 block text-[13px] font-medium text-text-muted'}>
        {label}
      </span>
      <div
        role="radiogroup"
        aria-labelledby={`${id}-label`}
        aria-describedby={hint ? `${id}-hint` : undefined}
        onKeyDown={onKeyDown}
        className="inline-flex max-w-full flex-wrap gap-1 rounded-[var(--radius-md)] border border-border bg-bg-elev/60 p-1"
      >
        {options.map((o, i) => {
          const active = o.value === value
          return (
            <button
              key={o.value}
              ref={(el) => {
                refs.current[i] = el
              }}
              type="button"
              role="radio"
              aria-checked={active}
              tabIndex={active ? 0 : -1}
              lang={o.lang}
              onClick={() => onChange(o.value)}
              className={cn(
                'inline-flex items-center gap-2 rounded-[10px] font-medium transition-colors ring-focus',
                compact ? 'px-2.5 py-1 text-[12px]' : 'px-3.5 py-1.5 text-[13px]',
                active ? 'bg-surface-2 text-text shadow-[var(--shadow-raised)]' : 'text-text-muted hover:text-text',
              )}
            >
              {o.icon}
              {o.label}
            </button>
          )
        })}
      </div>
      {hint && (
        <p id={`${id}-hint`} className="mt-1.5 text-[12px] text-text-dim">
          {hint}
        </p>
      )}
    </div>
  )
}

const THEME_ICON: Record<ThemePref, ReactNode> = {
  dark: <Moon className="size-4" aria-hidden />,
  light: <Sun className="size-4" aria-hidden />,
  system: <Monitor className="size-4" aria-hidden />,
}

interface PickerProps {
  compact?: boolean
  hideLabel?: boolean
  hint?: boolean
}

/** Dark / Light / System. Applies instantly; persisted by lib/prefs.ts. */
export function ThemePicker({ compact, hideLabel, hint }: PickerProps) {
  const t = useT()
  const value = useThemePref()
  return (
    <Segmented<ThemePref>
      label={t('common.theme.label')}
      hideLabel={hideLabel}
      hint={hint ? t('common.theme.hint') : undefined}
      compact={compact}
      value={value}
      onChange={setThemePref}
      options={THEME_PREFS.map((p) => ({ value: p, label: t(`common.theme.${p}`), icon: THEME_ICON[p] }))}
    />
  )
}

/** One option per language, each shown in its own language (endonym). */
export function LanguagePicker({ compact, hideLabel, hint }: PickerProps) {
  const t = useT()
  const value = useLang()
  return (
    <Segmented<Lang>
      label={t('common.language.label')}
      hideLabel={hideLabel}
      hint={hint ? t('common.language.hint') : undefined}
      compact={compact}
      value={value}
      onChange={setLangPref}
      options={LANGS.map((l) => ({ value: l, label: LANG_NAMES[l], lang: l }))}
    />
  )
}
