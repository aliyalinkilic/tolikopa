import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Tag } from 'lucide-react'
import { PageHeader } from '@/components/AppShell'
import { FilterChip } from '@/components/FilterChip'
import { Card } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { DataTable } from '@/components/DataTable'
import type { Column } from '@/components/DataTable'
import { SkeletonText } from '@/components/Skeleton'
import { useTags, useUsers } from '@/hooks/queries'
import { displayUser, relativeTime, splitLogonUser } from '@/lib/format'
import type { UserSummary } from '@/lib/types'
import { useT } from '@/i18n'
import { formatDateTime, initialOf } from '@/lib/format'

export function Users() {
  const t = useT()
  const navigate = useNavigate()
  const [tag, setTag] = useState<string | undefined>(undefined)
  const [untagged, setUntagged] = useState(false)
  // "Untagged" is not a server-side filter, so fetch all and filter client-side.
  const { data, isLoading } = useUsers(tag && !untagged ? tag : undefined)
  const { data: allUsers } = useUsers()
  const { data: tags } = useTags()

  // tag and untagged are mutually exclusive.
  const chooseTag = (x: string | undefined) => {
    setUntagged(false)
    setTag(x)
  }
  const chooseUntagged = () => {
    setTag(undefined)
    setUntagged((v) => !v)
  }
  const untaggedCount = (allUsers ?? []).filter((u) => (u.tags ?? []).length === 0).length
  const rows = untagged ? (data ?? []).filter((u) => (u.tags ?? []).length === 0) : (data ?? [])

  const columns: Column<UserSummary>[] = [
    {
      key: 'user',
      header: t('common.user'),
      // Fixed share of the width: under auto layout every page re-sized the
      // columns to its own longest name, so the table jumped while paging.
      headerClassName: 'w-[32%]',
      sortable: true,
      sortValue: (u) => u.display_name?.trim() || displayUser(u.logon_user),
      render: (u) => {
        const { domain, name } = splitLogonUser(u.logon_user)
        const heading = u.display_name?.trim() || name
        return (
          <div className="flex items-center gap-2.5">
            <span className="grid size-8 shrink-0 place-items-center rounded-full bg-accent-grad-soft text-[13px] font-bold text-accent">
              {initialOf(heading)}
            </span>
            <div className="min-w-0">
              <div className="truncate font-semibold text-text">{heading}</div>
              {domain && <div className="text-[12px] text-text-dim">{domain}\{name}</div>}
            </div>
          </div>
        )
      },
    },
    {
      key: 'tags',
      header: t('usersPage.department'),
      headerClassName: 'w-[26%]',
      render: (u) => {
        const list = u.tags ?? []
        if (list.length === 0) return <span className="text-text-dim">—</span>
        return (
          <div className="flex flex-wrap items-center gap-1">
            {list.slice(0, 3).map((x) => (
              <Badge key={x} tone="neutral">{x}</Badge>
            ))}
            {list.length > 3 && <span className="text-[12px] text-text-dim">+{list.length - 3}</span>}
          </div>
        )
      },
    },
    {
      key: 'status',
      header: t('common.status'),
      headerClassName: 'w-[13%]',
      sortable: true,
      sortValue: (u) => (u.online ? 0 : 1),
      render: (u) =>
        u.online ? (
          <Badge tone="ok" dot pulse>
            {t('common.online')}
          </Badge>
        ) : (
          <Badge tone="offline" dot>
            {t('common.offline')}
          </Badge>
        ),
    },
    {
      key: 'machine',
      header: t('usersPage.currentMachine'),
      headerClassName: 'w-[15%]',
      sortable: true,
      sortValue: (u) => u.current_machine ?? '',
      render: (u) => <span className="text-text-muted">{u.current_machine || '—'}</span>,
    },
    {
      key: 'last_seen',
      header: t('common.lastSeen'),
      align: 'right',
      sortable: true,
      sortValue: (u) => new Date(u.last_seen).getTime(),
      render: (u) => (
        <span className="tabular text-text-muted" title={formatDateTime(u.last_seen)}>
          {u.online ? t('common.now') : relativeTime(u.last_seen)}
        </span>
      ),
    },
  ]

  return (
    <div>
      <PageHeader
        title={t('nav.users')}
        description={t('usersPage.subtitle')}
        action={<Badge tone="neutral">{t('common.users', { count: rows.length })}</Badge>}
      />

      {tags && tags.length > 0 && (
        <div className="mb-4 flex items-center gap-2 overflow-x-auto pb-1" role="group" aria-label={t('usersPage.deptFilter')}>
          <Tag className="size-4 shrink-0 text-text-dim" aria-hidden />
          <FilterChip active={tag === undefined && !untagged} onClick={() => chooseTag(undefined)}>
            {t('common.all')}
          </FilterChip>
          <FilterChip active={untagged} onClick={chooseUntagged} count={untaggedCount}>
            {t('common.untagged')}
          </FilterChip>
          {tags.map((x) => (
            <FilterChip key={x.tag} active={tag === x.tag && !untagged} onClick={() => chooseTag(x.tag)} count={x.user_count}>
              {x.tag}
            </FilterChip>
          ))}
        </div>
      )}

      <Card padded={false} className="p-0">
        {isLoading ? (
          <div className="p-5">
            <SkeletonText lines={8} />
          </div>
        ) : (
          <DataTable
            columns={columns}
            rows={rows}
            getRowKey={(u) => u.logon_user}
            onRowClick={(u) => navigate(`/users/${encodeURIComponent(u.logon_user)}`)}
            initialSort={{ key: 'status', dir: 'asc' }}
            emptyLabel={untagged ? t('usersPage.emptyUntagged') : tag ? t('usersPage.emptyDept') : t('usersPage.empty')}
            className="border-0"
            pageSize={25}
            pagerClassName="px-4 pb-3.5"
          />
        )}
      </Card>
    </div>
  )
}
