import { ChevronLeft, ChevronRight } from 'lucide-react'
import { cn } from '@/lib/cn'
import { useT } from '@/i18n'

interface PagerProps {
  /** 1-based current page, already clamped to [1, pageCount]. */
  page: number
  pageSize: number
  total: number
  onPage: (page: number) => void
  className?: string
}

const btn =
  'inline-flex items-center gap-1 whitespace-nowrap rounded-[9px] border border-border-2 bg-surface/60 px-2.5 py-1 font-medium text-text-muted transition-colors hover:text-text disabled:pointer-events-none disabled:opacity-40 ring-focus'

/** Compact "1–20 / 57  ‹ 1 / 3 ›" pager shared by DataTable and list views. */
export function Pager({ page, pageSize, total, onPage, className }: PagerProps) {
  const t = useT()
  const pageCount = Math.max(1, Math.ceil(total / pageSize))
  const rangeStart = total === 0 ? 0 : (page - 1) * pageSize + 1
  const rangeEnd = Math.min(page * pageSize, total)
  const totalDigits = String(total).length
  const pageDigits = String(pageCount).length

  return (
    <div className={cn('flex flex-wrap items-center justify-between gap-x-3 gap-y-2 px-1 text-[12px] text-text-muted', className)}>
      <span
        className="tabular whitespace-nowrap"
        style={{ minWidth: `${totalDigits * 3 + 4}ch` }}
        aria-live="polite"
      >
        {rangeStart}–{rangeEnd} / {total}
      </span>
      <div className="ml-auto flex shrink-0 items-center gap-1.5">
        <button
          type="button"
          onClick={() => onPage(Math.max(1, page - 1))}
          disabled={page <= 1}
          aria-label={t('ui.table.prevPage')}
          className={btn}
        >
          <ChevronLeft className="size-3.5" aria-hidden />
          <span className="hidden sm:inline">{t('ui.table.prev')}</span>
        </button>
        <span
          className="tabular whitespace-nowrap px-1.5 text-center text-text-dim"
          style={{ minWidth: `${pageDigits * 2 + 3}ch` }}
        >
          {page} / {pageCount}
        </span>
        <button
          type="button"
          onClick={() => onPage(Math.min(pageCount, page + 1))}
          disabled={page >= pageCount}
          aria-label={t('ui.table.nextPage')}
          className={btn}
        >
          <span className="hidden sm:inline">{t('ui.table.next')}</span>
          <ChevronRight className="size-3.5" aria-hidden />
        </button>
      </div>
    </div>
  )
}
