import { useState } from 'react'
import { Check, EyeOff, Plus, X } from 'lucide-react'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { SkeletonText } from '@/components/Skeleton'
import { useCrashApps } from '@/hooks/queries'
import { formatNumber } from '@/lib/format'
import { useT } from '@/i18n'

/** Server limits (server/internal/api/crash_apps.go). */
const MAX_APPS = 100
const MAX_LEN = 128

/** The form the alert engine matches: trimmed, lower-case, no trailing ".exe". */
export function canonicalApp(s: string): string {
  return s.trim().toLowerCase().replace(/\.exe$/, '')
}

/**
 * Settings → Alert Thresholds → crash_storm ignore list. Edits the rule's
 * `ignore_apps` in the parent's draft; the section's Save button writes it.
 * Suggestions come from recent crash events (GET /api/admin/crash-apps).
 */
export function CrashIgnoreEditor({ value, onChange }: { value: string[]; onChange: (next: string[]) => void }) {
  const t = useT()
  const [text, setText] = useState('')
  const [error, setError] = useState<string | undefined>()
  const { data, isLoading, isError } = useCrashApps('7d', 15)

  const has = (app: string) => value.includes(canonicalApp(app))

  const add = (raw: string): boolean => {
    const app = canonicalApp(raw)
    if (!app) return false
    if (/[/\\]/.test(app) || app.length > MAX_LEN || /[\u0000-\u001f\u007f]/.test(app)) {
      setError(t('settings.rules.ignore.invalid'))
      return false
    }
    if (has(app)) return true
    if (value.length >= MAX_APPS) {
      setError(t('settings.rules.ignore.tooMany', { max: MAX_APPS }))
      return false
    }
    setError(undefined)
    onChange([...value, app])
    return true
  }

  const submit = () => {
    if (add(text)) setText('')
  }

  const apps = data?.apps ?? []

  return (
    <div className="grid gap-5 lg:grid-cols-2">
      <div>
        <h3 className="text-[14px] font-semibold text-text">{t('settings.rules.ignore.title')}</h3>
        <p className="mt-1 text-[12px] text-text-dim">{t('settings.rules.ignore.body')}</p>
        <div className="mt-3">
          {value.length === 0 ? (
            <p className="text-[12px] text-text-muted">{t('settings.rules.ignore.empty')}</p>
          ) : (
            <ul className="flex flex-wrap gap-2">
              {value.map((app) => (
                <li key={app}>
                  <span className="inline-flex items-center gap-1 rounded-full border border-accent/40 bg-accent/10 py-1 pl-3 pr-1 font-mono text-[12px] text-text">
                    {app}
                    <button
                      type="button"
                      className="grid size-5 place-items-center rounded-full text-text-dim hover:text-crit ring-focus"
                      onClick={() => onChange(value.filter((x) => x !== app))}
                      aria-label={t('settings.rules.ignore.remove', { app })}
                    >
                      <X className="size-3.5" aria-hidden />
                    </button>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
        <div className="mt-3 flex items-start gap-2">
          <Input
            aria-label={t('settings.rules.ignore.placeholder')}
            placeholder={t('settings.rules.ignore.placeholder')}
            value={text}
            maxLength={MAX_LEN + 4}
            error={error}
            onChange={(e) => {
              setText(e.target.value)
              setError(undefined)
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                submit()
              }
            }}
          />
          <Button
            type="button"
            variant="secondary"
            leftIcon={<Plus className="size-4" aria-hidden />}
            onClick={submit}
            disabled={!text.trim()}
          >
            {t('settings.rules.ignore.add')}
          </Button>
        </div>
      </div>

      <div>
        <h3 className="text-[13px] font-semibold text-text-muted">{t('settings.rules.ignore.suggestionsTitle')}</h3>
        <div className="mt-2">
          {isLoading ? (
            <SkeletonText lines={4} />
          ) : isError ? (
            <p role="alert" className="text-[12px] text-crit">
              {t('settings.rules.ignore.suggestionsFailed')}
            </p>
          ) : apps.length === 0 ? (
            <p className="text-[12px] text-text-dim">{t('settings.rules.ignore.suggestionsEmpty')}</p>
          ) : (
            <table className="w-full text-left text-[13px]">
              <thead>
                <tr className="border-b border-border text-[11px] font-semibold uppercase tracking-[0.08em] text-text-dim">
                  <th className="py-2 pr-3">{t('settings.rules.ignore.colApp')}</th>
                  <th className="px-3 text-right">{t('settings.rules.ignore.colCrashes')}</th>
                  <th className="px-3 text-right">{t('settings.rules.ignore.colMachines')}</th>
                  <th className="pl-3" />
                </tr>
              </thead>
              <tbody>
                {apps.map((a) => {
                  const ignored = has(a.app)
                  return (
                    <tr key={a.app} className="border-b border-border/70 last:border-0">
                      <td className="max-w-0 truncate py-2 pr-3 font-mono text-[12px] text-text" title={a.app}>
                        {a.app}
                      </td>
                      <td className="tabular px-3 text-right text-text">{formatNumber(a.crashes)}</td>
                      <td className="tabular px-3 text-right text-text-muted">{formatNumber(a.machines)}</td>
                      <td className="py-1.5 pl-3 text-right">
                        {ignored ? (
                          <span className="inline-flex items-center gap-1 text-[12px] text-text-dim">
                            <Check className="size-3.5" aria-hidden />
                            {t('settings.rules.ignore.ignored')}
                          </span>
                        ) : (
                          <Button
                            type="button"
                            size="sm"
                            variant="ghost"
                            leftIcon={<EyeOff className="size-3.5" aria-hidden />}
                            onClick={() => add(a.app)}
                            aria-label={t('settings.rules.ignore.ignoreOneFor', { app: a.app })}
                          >
                            {t('settings.rules.ignore.ignoreOne')}
                          </Button>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  )
}
