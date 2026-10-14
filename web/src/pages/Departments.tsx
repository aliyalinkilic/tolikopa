import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { motion } from 'motion/react'
import { Building2, FileDown, Inbox, Lock, Users2 } from 'lucide-react'
import { PageHeader } from '@/components/AppShell'
import { Card, CardHeader } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { RangeSelector } from '@/components/RangeSelector'
import { DataTable } from '@/components/DataTable'
import type { Column } from '@/components/DataTable'
import { CategoryDonut } from '@/components/CategoryDonut'
import { EmptyState } from '@/components/EmptyState'
import { Skeleton, SkeletonText } from '@/components/Skeleton'
import { useTagSummary, useTags } from '@/hooks/queries'
import { useRange } from '@/hooks/useRange'
import { catMeta } from '@/lib/labels'
import { displayUser, formatDate, formatMinutes, formatNumber, splitLogonUser } from '@/lib/format'
import { fadeUp, staggerContainer } from '@/lib/motion'
import { cn } from '@/lib/cn'
import type { TagUserSummary } from '@/lib/types'
import { useT } from '@/i18n'

export function Departments() {
  const t = useT()
  const navigate = useNavigate()
  const { key: rangeKey, setKey: setRange, win, custom, setCustom } = useRange('7d')
  const { data: tags, isLoading: tagsLoading, isError: tagsError } = useTags()
  const [selected, setSelected] = useState<string | undefined>(undefined)

  // Land on the first department so the page is never blank when data exists.
  useEffect(() => {
    if (!selected && tags && tags.length > 0) setSelected(tags[0].tag)
  }, [tags, selected])

  const { data: summary, isLoading: sumLoading } = useTagSummary(selected, win)

  const users = summary?.users ?? []
  const totalActive = users.reduce((a, u) => a + u.active_minutes, 0)

  const columns: Column<TagUserSummary>[] = [
    {
      key: 'user',
      header: t('common.user'),
      sortable: true,
      sortValue: (u) => u.display_name?.trim() || displayUser(u.logon_user),
      render: (u) => {
        const { domain, name } = splitLogonUser(u.logon_user)
        const heading = u.display_name?.trim() || name
        return (
          <div className="min-w-0">
            <div className="truncate font-semibold text-text">{heading}</div>
            <div className="truncate text-[12px] text-text-dim">{domain ? `${domain}\\${name}` : name}</div>
          </div>
        )
      },
    },
    {
      key: 'active',
      header: t('departments.activeTime'),
      align: 'right',
      sortable: true,
      sortValue: (u) => u.active_minutes,
      render: (u) => <span className="tabular font-semibold text-text">{formatMinutes(u.active_minutes)}</span>,
    },
    {
      key: 'top',
      header: t('departments.topCategory'),
      sortable: true,
      sortValue: (u) => (u.top_category ? catMeta(u.top_category).label : ''),
      render: (u) => {
        if (!u.top_category) return <span className="text-text-dim">—</span>
        const meta = catMeta(u.top_category)
        const Icon = meta.icon
        return (
          <span className="inline-flex items-center gap-1.5 text-[13px] text-text-muted">
            <span className="size-2 rounded-full" style={{ backgroundColor: meta.color }} aria-hidden />
            <Icon className="size-3.5 text-text-dim" aria-hidden />
            {meta.label}
          </span>
        )
      },
    },
  ]

  return (
    <div>
      <div className="no-print">
        <PageHeader
          title={t('nav.departments')}
          description={t('departments.subtitle')}
          action={<RangeSelector value={rangeKey} onChange={setRange} custom={custom} onCustomChange={setCustom} />}
        />
      </div>

      <div className="grid gap-4 lg:grid-cols-[260px_1fr]">
        {/* Department list */}
        <div className="no-print">
          <Card padded={false} className="p-2">
            {tagsLoading ? (
              <div className="p-3">
                <SkeletonText lines={6} />
              </div>
            ) : tagsError ? (
              <EmptyState icon={Lock} title={t('departments.loadFailedTitle')} description={t('departments.loadFailedBody')} />
            ) : !tags || tags.length === 0 ? (
              <EmptyState icon={Building2} title={t('departments.noneTitle')} description={t('departments.noneBody')} />
            ) : (
              <ul className="space-y-1" role="listbox" aria-label={t('nav.departments')}>
                {tags.map((x) => {
                  const active = x.tag === selected
                  return (
                    <li key={x.tag}>
                      <button
                        type="button"
                        role="option"
                        aria-selected={active}
                        onClick={() => setSelected(x.tag)}
                        className={cn(
                          'flex w-full items-center gap-2.5 rounded-[var(--radius-md)] px-3 py-2.5 text-left text-[14px] transition-colors ring-focus',
                          active ? 'bg-accent-grad-soft text-text' : 'text-text-muted hover:bg-glass hover:text-text',
                        )}
                      >
                        <Building2 className={cn('size-4 shrink-0', active && 'text-accent')} aria-hidden />
                        <span className="flex-1 truncate font-medium">{x.tag}</span>
                        <span className="tabular text-[12px] text-text-dim">{x.user_count}</span>
                      </button>
                    </li>
                  )
                })}
              </ul>
            )}
          </Card>
        </div>

        {/* Selected department report */}
        <div>
          {!selected ? (
            <EmptyState icon={Building2} title={t('departments.pickTitle')} description={t('departments.pickBody')} />
          ) : (
            <>
              <div className="no-print mb-4 flex flex-wrap items-center justify-between gap-3">
                <h2 className="text-[18px] font-bold tracking-[-0.01em] text-text">{selected}</h2>
                <Button
                  variant="secondary"
                  leftIcon={<FileDown className="size-4" aria-hidden />}
                  onClick={() => window.print()}
                  disabled={sumLoading || users.length === 0}
                >
                  {t('departments.downloadPdf')}
                </Button>
              </div>

              <div className="print-area space-y-4">
                {/* Print-only report header */}
                <div className="print-only mb-4 border-b border-border pb-3">
                  <div className="flex items-start justify-between gap-4">
                    <div>
                      <div className="text-[11px] font-semibold uppercase tracking-[0.16em]">{t('departments.reportTitle')}</div>
                      <div className="text-[22px] font-bold">{selected}</div>
                    </div>
                    <div className="text-right text-[11px] leading-5">
                      <div>{t('departments.period', { from: formatDate(win.from), to: formatDate(win.to) })}</div>
                      <div>{t('departments.generated', { date: formatDate(new Date()) })}</div>
                    </div>
                  </div>
                </div>

                {sumLoading ? (
                  <ReportSkeleton />
                ) : users.length === 0 ? (
                  <EmptyState icon={Inbox} title={t('departments.emptyTitle')} description={t('departments.emptyBody')} />
                ) : (
                  <motion.div variants={staggerContainer(0.05)} initial="hidden" animate="show" className="space-y-4">
                    <motion.div variants={fadeUp} className="grid grid-cols-2 gap-4 sm:grid-cols-3">
                      <StatTile label={t('common.user')} value={formatNumber(users.length)} icon={<Users2 className="size-4" aria-hidden />} />
                      <StatTile label={t('departments.totalActive')} value={formatMinutes(totalActive)} accent />
                      <StatTile label={t('departments.perPerson')} value={formatMinutes(users.length ? totalActive / users.length : 0)} />
                    </motion.div>

                    <motion.div variants={fadeUp}>
                      <Card className="print-card">
                        <CardHeader title={t('departments.categoriesTitle')} description={t('departments.categoriesBody')} />
                        <div className="mt-5">
                          {summary && summary.category_totals.length > 0 ? (
                            <CategoryDonut data={summary.category_totals} />
                          ) : (
                            <EmptyState icon={Inbox} title={t('ui.chart.noCategory')} />
                          )}
                        </div>
                      </Card>
                    </motion.div>

                    <motion.div variants={fadeUp}>
                      <Card padded={false} className="print-card">
                        <CardHeader title={t('nav.users')} description={t('departments.membersBody')} className="p-5" />
                        <div className="px-5 pb-5">
                          <DataTable
                            columns={columns}
                            rows={users}
                            getRowKey={(u) => u.logon_user}
                            onRowClick={(u) => navigate(`/users/${encodeURIComponent(u.logon_user)}`)}
                            initialSort={{ key: 'active', dir: 'desc' }}
                            emptyLabel={t('departments.noUsers')}
                            className="border-0"
                          />
                        </div>
                      </Card>
                    </motion.div>
                  </motion.div>
                )}
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}

function StatTile({ label, value, icon, accent }: { label: string; value: string; icon?: ReactNode; accent?: boolean }) {
  return (
    <Card className="print-card">
      <div className="flex items-start justify-between gap-2">
        <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-text-dim">{label}</p>
        {icon && <span className="text-text-dim">{icon}</span>}
      </div>
      <p className={cn('tabular mt-2 text-[24px] font-bold leading-none', accent ? 'text-gradient' : 'text-text')}>{value}</p>
    </Card>
  )
}

function ReportSkeleton() {
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <Card key={i}>
            <Skeleton className="h-3 w-20" />
            <Skeleton className="mt-3 h-7 w-24" />
          </Card>
        ))}
      </div>
      <Card>
        <Skeleton className="h-4 w-40" />
        <Skeleton className="mt-4 h-44 w-full" />
      </Card>
    </div>
  )
}
