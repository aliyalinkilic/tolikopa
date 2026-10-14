import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { ChevronDown, ChevronLeft, ChevronRight, ChevronUp, ChevronsUpDown } from 'lucide-react'
import { cn } from '@/lib/cn'
import { EmptyState } from './EmptyState'
import { Inbox } from 'lucide-react'
import { useT } from '@/i18n'
import { compareText } from '@/lib/format'

export interface Column<T> {
  key: string
  header: ReactNode
  render: (row: T) => ReactNode
  sortable?: boolean
  sortValue?: (row: T) => string | number
  align?: 'left' | 'right' | 'center'
  className?: string
  headerClassName?: string
}

type SortDir = 'asc' | 'desc'

interface DataTableProps<T> {
  columns: Column<T>[]
  rows: T[]
  getRowKey: (row: T) => string
  onRowClick?: (row: T) => void
  initialSort?: { key: string; dir: SortDir }
  emptyLabel?: string
  maxHeight?: number
  className?: string
  /** When set, render one page of rows at a time with a compact pager. Undefined = no pagination. */
  pageSize?: number
}

const alignClass: Record<'left' | 'right' | 'center', string> = {
  left: 'text-left',
  right: 'text-right',
  center: 'text-center',
}

export function DataTable<T>({
  columns,
  rows,
  getRowKey,
  onRowClick,
  initialSort,
  emptyLabel,
  maxHeight,
  className,
  pageSize,
}: DataTableProps<T>) {
  const t = useT()
  const [sort, setSort] = useState<{ key: string; dir: SortDir } | null>(initialSort ?? null)
  const [page, setPage] = useState(1)

  const sorted = useMemo(() => {
    if (!sort) return rows
    const col = columns.find((c) => c.key === sort.key)
    if (!col?.sortValue) return rows
    const accessor = col.sortValue
    const factor = sort.dir === 'asc' ? 1 : -1
    return [...rows].sort((a, b) => {
      const av = accessor(a)
      const bv = accessor(b)
      if (typeof av === 'number' && typeof bv === 'number') return (av - bv) * factor
      return compareText(String(av), String(bv)) * factor
    })
  }, [rows, sort, columns])

  // Reset to the first page whenever the data set or sort changes.
  useEffect(() => {
    setPage(1)
  }, [rows, sort])

  const size = pageSize ?? 0
  const pageCount = size > 0 ? Math.max(1, Math.ceil(sorted.length / size)) : 1
  const safePage = Math.min(page, pageCount)
  const visibleRows = size > 0 ? sorted.slice((safePage - 1) * size, safePage * size) : sorted
  const showPager = size > 0 && sorted.length > size
  const rangeStart = sorted.length === 0 ? 0 : (safePage - 1) * size + 1
  const rangeEnd = size > 0 ? Math.min(safePage * size, sorted.length) : sorted.length

  const toggleSort = (key: string) => {
    setSort((prev) => {
      if (prev?.key !== key) return { key, dir: 'asc' }
      if (prev.dir === 'asc') return { key, dir: 'desc' }
      return null
    })
  }

  if (rows.length === 0) {
    return <EmptyState icon={Inbox} title={emptyLabel ?? t('ui.table.empty')} />
  }

  const table = (
    <div
      className={cn('overflow-auto rounded-[var(--radius-md)] border border-border', className)}
      style={maxHeight ? { maxHeight } : undefined}
    >
      <table className="w-full border-collapse text-left text-[13px]">
        <thead className="sticky top-0 z-10">
          <tr className="bg-bg-elev/95 backdrop-blur">
            {columns.map((c) => {
              const active = sort?.key === c.key
              const ariaSort = active ? (sort?.dir === 'asc' ? 'ascending' : 'descending') : 'none'
              return (
                <th
                  key={c.key}
                  aria-sort={c.sortable ? ariaSort : undefined}
                  className={cn(
                    'whitespace-nowrap border-b border-border px-3.5 py-2.5 text-[11px] font-semibold uppercase tracking-[0.08em] text-text-dim',
                    alignClass[c.align ?? 'left'],
                    c.headerClassName,
                  )}
                >
                  {c.sortable ? (
                    <button
                      type="button"
                      onClick={() => toggleSort(c.key)}
                      className={cn(
                        // `uppercase` again: a <button> does not inherit text-transform from the <th>,
                        // which left sortable headers in a different case from plain ones.
                        'inline-flex items-center gap-1 uppercase transition-colors hover:text-text ring-focus',
                        c.align === 'right' && 'flex-row-reverse',
                        active && 'text-text',
                      )}
                    >
                      {c.header}
                      {active ? (
                        sort?.dir === 'asc' ? (
                          <ChevronUp className="size-3.5" aria-hidden />
                        ) : (
                          <ChevronDown className="size-3.5" aria-hidden />
                        )
                      ) : (
                        <ChevronsUpDown className="size-3.5 opacity-50" aria-hidden />
                      )}
                    </button>
                  ) : (
                    c.header
                  )}
                </th>
              )
            })}
          </tr>
        </thead>
        <tbody>
          {visibleRows.map((row) => (
            <tr
              key={getRowKey(row)}
              onClick={onRowClick ? () => onRowClick(row) : undefined}
              onKeyDown={
                onRowClick
                  ? (e) => {
                      if (e.key === 'Enter') onRowClick(row)
                    }
                  : undefined
              }
              tabIndex={onRowClick ? 0 : undefined}
              className={cn(
                'border-b border-border/70 transition-colors last:border-0',
                onRowClick && 'cursor-pointer hover:bg-surface-2 ring-focus',
              )}
            >
              {columns.map((c) => (
                <td
                  key={c.key}
                  className={cn('px-3.5 py-2.5 text-text', alignClass[c.align ?? 'left'], c.className)}
                >
                  {c.render(row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )

  if (!showPager) return table

  return (
    <div className="space-y-3">
      {table}
      <div className="flex items-center justify-between gap-3 px-1 text-[12px] text-text-muted">
        <span className="tabular">
          {rangeStart}–{rangeEnd} / {sorted.length}
        </span>
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            onClick={() => setPage((p) => Math.max(1, p - 1))}
            disabled={safePage <= 1}
            aria-label={t('ui.table.prevPage')}
            className="inline-flex items-center gap-1 rounded-[9px] border border-border-2 bg-surface/60 px-2.5 py-1 font-medium text-text-muted transition-colors hover:text-text disabled:pointer-events-none disabled:opacity-40 ring-focus"
          >
            <ChevronLeft className="size-3.5" aria-hidden />
            {t('ui.table.prev')}
          </button>
          <span className="tabular px-1.5 text-text-dim">
            {safePage} / {pageCount}
          </span>
          <button
            type="button"
            onClick={() => setPage((p) => Math.min(pageCount, p + 1))}
            disabled={safePage >= pageCount}
            aria-label={t('ui.table.nextPage')}
            className="inline-flex items-center gap-1 rounded-[9px] border border-border-2 bg-surface/60 px-2.5 py-1 font-medium text-text-muted transition-colors hover:text-text disabled:pointer-events-none disabled:opacity-40 ring-focus"
          >
            {t('ui.table.next')}
            <ChevronRight className="size-3.5" aria-hidden />
          </button>
        </div>
      </div>
    </div>
  )
}
