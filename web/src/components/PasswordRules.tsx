import { Check, Circle } from 'lucide-react'
import { useT } from '@/i18n'
import { cn } from '@/lib/cn'

/** Server rule (docs/API.md §2.2): 10–72; the upper bound is bytes (bcrypt). */
export const MIN_PASSWORD = 10
export const MAX_PASSWORD_BYTES = 72

export function passwordTooLong(password: string): boolean {
  return new TextEncoder().encode(password).length > MAX_PASSWORD_BYTES
}

export function passwordAcceptable(password: string): boolean {
  return password.length >= MIN_PASSWORD && !passwordTooLong(password)
}

interface PasswordRulesProps {
  password: string
  /** Pass to add the "both match" rule. */
  confirm?: string
  /** Pass to add the "differs from current" rule. */
  current?: string
  id?: string
}

/** Live checklist under a new-password field. Icon + text, never colour alone. */
export function PasswordRules({ password, confirm, current, id }: PasswordRulesProps) {
  const t = useT()
  const rules: { ok: boolean; label: string }[] = [
    { ok: password.length >= MIN_PASSWORD, label: t('ui.password.ruleLength', { count: MIN_PASSWORD }) },
  ]
  if (current !== undefined) {
    rules.push({ ok: password.length > 0 && password !== current, label: t('ui.password.ruleDifferent') })
  }
  if (confirm !== undefined) {
    rules.push({ ok: password.length > 0 && password === confirm, label: t('ui.password.ruleMatch') })
  }
  return (
    <ul id={id} className="space-y-1">
      {rules.map((r) => (
        <li key={r.label} className={cn('flex items-center gap-2 text-[12px]', r.ok ? 'text-ok' : 'text-text-dim')}>
          {r.ok ? <Check className="size-3.5 shrink-0" aria-hidden /> : <Circle className="size-3.5 shrink-0" aria-hidden />}
          {r.label}
        </li>
      ))}
    </ul>
  )
}
