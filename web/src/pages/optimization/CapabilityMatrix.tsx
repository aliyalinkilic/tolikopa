import { Link } from 'react-router-dom'
import { Check, Lightbulb, Minus } from 'lucide-react'
import { Badge } from '@/components/ui/Badge'
import { Card, CardHeader } from '@/components/ui/Card'
import { useT } from '@/i18n'

/** What each platform can do here. Linux actuation would need an agent change
 *  AND an amendment to docs/AGENT.md first — said plainly, not hidden. */
export function CapabilityMatrix() {
  const t = useT()
  const rows: { label: string; win: 'yes' | 'no' | 'advisory'; linux: 'yes' | 'no' | 'advisory'; winNote?: string }[] = [
    { label: t('optimization.cap.apps'), win: 'yes', linux: 'no' },
    { label: t('optimization.cap.startup'), win: 'yes', linux: 'no' },
    { label: t('optimization.cap.advice'), win: 'advisory', linux: 'advisory', winNote: t('optimization.cap.winAdviceNote') },
  ]
  const cell = (v: 'yes' | 'no' | 'advisory') =>
    v === 'yes' ? (
      <Badge tone="ok" icon={<Check className="size-3" aria-hidden />}>{t('optimization.cap.yes')}</Badge>
    ) : v === 'advisory' ? (
      <Badge tone="info" icon={<Lightbulb className="size-3" aria-hidden />}>{t('optimization.cap.advisory')}</Badge>
    ) : (
      <Badge tone="neutral" icon={<Minus className="size-3" aria-hidden />}>{t('optimization.cap.no')}</Badge>
    )
  return (
    <Card>
      <CardHeader title={t('optimization.cap.title')} description={t('optimization.cap.body')} />
      <div className="mt-4 overflow-x-auto">
        <table className="w-full text-left text-[13px]">
          <thead>
            <tr className="border-b border-border text-[11px] font-semibold uppercase tracking-[0.08em] text-text-dim">
              <th scope="col" className="py-2.5 pr-3">{t('optimization.cap.feature')}</th>
              <th scope="col" className="px-3">Windows</th>
              <th scope="col" className="px-3">Linux</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.label} className="border-b border-border/70 last:border-0">
                <th scope="row" className="py-3 pr-3 font-medium text-text">{r.label}</th>
                <td className="px-3">
                  <span className="flex flex-wrap items-center gap-2">
                    {cell(r.win)}
                    {r.winNote && (
                      <Link to="/slow-machines" className="text-[12px] text-accent hover:underline ring-focus">
                        {r.winNote}
                      </Link>
                    )}
                  </span>
                </td>
                <td className="px-3">{cell(r.linux)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="mt-3 text-[12px] text-text-dim">{t('optimization.cap.linuxNote')}</p>
    </Card>
  )
}

