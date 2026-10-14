import { useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { motion, useReducedMotion } from 'motion/react'
import {
  Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis,
} from 'recharts'
import { Activity, AppWindow, ChevronLeft, Inbox, Monitor, Pencil, Plus, Save, Tag, UserX, X } from 'lucide-react'
import { Card, CardHeader } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Modal } from '@/components/ui/Modal'
import { RangeSelector } from '@/components/RangeSelector'
import { Timeline } from '@/components/Timeline'
import { DataTable } from '@/components/DataTable'
import type { Column } from '@/components/DataTable'
import { TimeSeriesChart } from '@/components/TimeSeriesChart'
import type { ChartRow } from '@/components/TimeSeriesChart'
import { CategoryDonut } from '@/components/CategoryDonut'
import { ActivityHeatmap } from '@/components/ActivityHeatmap'
import { EmptyState } from '@/components/EmptyState'
import { Skeleton, SkeletonText } from '@/components/Skeleton'
import { useRange } from '@/hooks/useRange'
import {
  useUpdateUserProfile, useUserActivity, useUserApps, useUserCategories, useUserLogons, useUserNetwork,
  useUsers, useUserTimeline,
} from '@/hooks/queries'
import { useIsAdmin } from '@/hooks/useAuth'
import { useToast } from '@/hooks/useToast'
import { chart } from '@/lib/theme'
import { fadeUp, staggerContainer } from '@/lib/motion'
import { cn } from '@/lib/cn'
import {
  displayUser, formatDate, formatDateTime, formatDayTime, formatDuration, formatMinutes, formatShortDate,
  formatSpan, formatTime, relativeTime, splitLogonUser,
} from '@/lib/format'
import type { AppUsage, DailyActivity, NetPoint, UserLogon } from '@/lib/types'
import { useT } from '@/i18n'
import { useResolveColor } from '@/lib/theme'
import { formatPercent, initialOf, sameText } from '@/lib/format'
import type { TFn } from '@/i18n'


function toNetRows(net?: NetPoint[]): ChartRow[] {
  return (net ?? []).map((p) => ({ t: p.t, recv: p.recv, sent: p.sent }))
}

export function UserDetail() {
  const t = useT()
  const { user: rawUser } = useParams()
  const user = rawUser ?? ''
  const isAdmin = useIsAdmin()
  const { key: rangeKey, setKey: setRange, win, custom, setCustom } = useRange()

  const { data: users } = useUsers()
  const summary = users?.find((u) => u.logon_user === user)

  const { data: activity, isLoading: actLoading } = useUserActivity(user, win)
  const { data: timeline, isLoading: tlLoading } = useUserTimeline(user, win)
  const { data: network, isLoading: netLoading } = useUserNetwork(user, win)
  const { data: categories, isLoading: catLoading } = useUserCategories(user, win)
  const { data: apps, isLoading: appsLoading } = useUserApps(user, win)

  const { data: logons, isLoading: logonsLoading } = useUserLogons(user)
  const logonDevices = useMemo(() => summarizeDevices(logons ?? []), [logons])

  const [editorOpen, setEditorOpen] = useState(false)

  if (!user) {
    return <EmptyState icon={UserX} title={t('userDetail.notSpecified')} />
  }

  const { domain, name } = splitLogonUser(user)
  const tags = summary?.tags ?? []
  const heading = summary?.display_name?.trim() || displayUser(user)
  const activeMin = activity?.total_active_minutes ?? 0
  const loggedMin = activity?.total_loggedon_minutes ?? 0
  const ratio = loggedMin > 0 ? Math.round((activeMin / loggedMin) * 100) : 0

  return (
    <div>
      <Link to="/users" className="mb-3 inline-flex items-center gap-1 text-[13px] text-text-muted hover:text-accent ring-focus">
        <ChevronLeft className="size-4" aria-hidden />
        {t('nav.users')}
      </Link>

      <Card className="mb-6">
        <div className="flex flex-wrap items-center gap-4">
          <span className="grid size-14 shrink-0 place-items-center rounded-full bg-accent-grad-soft text-[22px] font-bold text-accent">
            {initialOf(name)}
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2.5">
              <h1 className="text-[22px] font-bold tracking-[-0.01em] text-text">{heading}</h1>
              {summary?.online ? (
                <Badge tone="ok" dot pulse>
                  {t('common.online')}
                </Badge>
              ) : (
                <Badge tone="offline" dot>
                  {t('common.offline')}
                </Badge>
              )}
            </div>
            <p className="mt-1 text-[13px] text-text-muted">
              {domain ? `${domain}\\${name} · ` : ''}
              {summary?.current_machine ? t('userDetail.currentOn', { machine: summary.current_machine }) : t('userDetail.noActiveMachine')}
              {summary && !summary.online ? ` · ${t('common.lastSeenAgo', { when: relativeTime(summary.last_seen) })}` : ''}
            </p>
            {tags.length > 0 && (
              <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
                {tags.map((tag) => (
                  <Badge key={tag} tone="accent" icon={<Tag className="size-3" aria-hidden />}>
                    {tag}
                  </Badge>
                ))}
              </div>
            )}
          </div>
          <div className="flex items-center gap-2">
            <RangeSelector value={rangeKey} onChange={setRange} custom={custom} onCustomChange={setCustom} />
            {isAdmin && (
              <Button
                size="icon"
                variant="secondary"
                aria-label={t('userDetail.editProfile')}
                title={t('userDetail.editProfile')}
                onClick={() => setEditorOpen(true)}
              >
                <Pencil className="size-4" aria-hidden />
              </Button>
            )}
          </div>
        </div>
      </Card>

      <motion.div variants={staggerContainer(0.05)} initial="hidden" animate="show" className="space-y-4">
        {/* Verimlilik — active-hours heatmap + totals */}
        <motion.div variants={fadeUp}>
          <Card>
            <CardHeader
              eyebrow={t('userDetail.productivity')}
              title={t('userDetail.heatmapTitle')}
              description={t('userDetail.heatmapBody')}
              action={
                <div className="flex items-center gap-5">
                  <Stat label={t('userDetail.totalActive')} value={formatMinutes(activeMin)} accent />
                  <Stat label={t('userDetail.sessionTime')} value={formatMinutes(loggedMin)} />
                  <Stat label={t('userDetail.activeRatio')} value={formatPercent(ratio)} />
                </div>
              }
            />
            <div className="mt-5">
              {actLoading ? (
                <Skeleton className="h-44 w-full" />
              ) : activity && activity.heatmap.length > 0 ? (
                <ActivityHeatmap data={activity.heatmap} />
              ) : (
                <EmptyState icon={Activity} title={t('userDetail.noActivity')} />
              )}
            </div>
          </Card>
        </motion.div>

        <div className="grid gap-4 lg:grid-cols-[1.5fr_1fr]">
          <motion.div variants={fadeUp}>
            <Card>
              <CardHeader title={t('userDetail.dailyTitle')} description={t('userDetail.dailyBody')} />
              <div className="mt-4">
                {actLoading ? (
                  <Skeleton className="h-[220px] w-full" />
                ) : activity && activity.daily.length > 0 ? (
                  <DailyActivityBars daily={activity.daily} />
                ) : (
                  <EmptyState icon={Inbox} title={t('userDetail.noDaily')} />
                )}
              </div>
            </Card>
          </motion.div>
          <motion.div variants={fadeUp}>
            <Card>
              <CardHeader title={t('userDetail.categoriesTitle')} description={t('userDetail.categoriesBody')} />
              <div className="mt-5">
                {catLoading ? (
                  <Skeleton className="h-44 w-full" />
                ) : categories && categories.length > 0 ? (
                  <CategoryDonut data={categories} />
                ) : (
                  <EmptyState icon={Inbox} title={t('userDetail.noApps')} />
                )}
              </div>
            </Card>
          </motion.div>
        </div>

        <motion.div variants={fadeUp}>
          <Card>
            <CardHeader
              title={t('userDetail.appTimeTitle')}
              description={t('userDetail.appTimeBody')}
            />
            <div className="mt-5">
              {appsLoading ? (
                <SkeletonText lines={8} />
              ) : apps && apps.length > 0 ? (
                <AppUsageList apps={apps} />
              ) : (
                <EmptyState icon={AppWindow} title={t('userDetail.noAppTime')} />
              )}
            </div>
          </Card>
        </motion.div>

        <motion.div variants={fadeUp}>
          <Card>
            <CardHeader
              eyebrow={t('userDetail.security')}
              title={t('userDetail.logonsTitle')}
              description={t('userDetail.logonsBody')}
              action={
                logonDevices.length > 0 ? (
                  <Badge tone={logonDevices.length > 1 ? 'warn' : 'neutral'} icon={<Monitor className="size-3" aria-hidden />}>
                    {t('common.devices', { count: logonDevices.length })}
                  </Badge>
                ) : undefined
              }
            />
            <div className="mt-5">
              {logonsLoading ? (
                <SkeletonText lines={6} />
              ) : logons && logons.length > 0 ? (
                <LogonHistory logons={logons} devices={logonDevices} />
              ) : (
                <EmptyState icon={Inbox} title={t('userDetail.noLogons')} />
              )}
            </div>
          </Card>
        </motion.div>

        <motion.div variants={fadeUp}>
          <Card>
            <CardHeader title={t('userDetail.timelineTitle')} description={t('userDetail.timelineBody')} />
            <div className="mt-5">
              {tlLoading ? (
                <Skeleton className="h-28 w-full" />
              ) : timeline && timeline.length > 0 ? (
                <Timeline sessions={timeline} from={win.from} to={win.to} groupBy="hostname" />
              ) : (
                <EmptyState icon={Inbox} title={t('userDetail.noSessions')} />
              )}
            </div>
          </Card>
        </motion.div>

        <motion.div variants={fadeUp}>
          <Card>
            <CardHeader title={t('userDetail.networkTitle')} description={t('userDetail.networkBody')} />
            <div className="mt-4">
              {netLoading ? (
                <Skeleton className="h-[240px] w-full" />
              ) : network && network.length > 0 ? (
                <TimeSeriesChart data={toNetRows(network)} series={[
                    { key: 'recv', name: t('machine.received'), color: chart.netDown },
                    { key: 'sent', name: t('machine.sent'), color: chart.netUp },
                  ]} unit="bytes" spanMs={win ? new Date(win.to).getTime() - new Date(win.from).getTime() : 0} />
              ) : (
                <EmptyState icon={Inbox} title={t('userDetail.noNetwork')} />
              )}
            </div>
          </Card>
        </motion.div>
      </motion.div>

      {isAdmin && (
        <ProfileEditor
          key={editorOpen ? 'open' : 'closed'}
          open={editorOpen}
          onClose={() => setEditorOpen(false)}
          user={user}
          initialName={summary?.display_name ?? ''}
          initialTags={tags}
        />
      )}
    </div>
  )
}

function Stat({ label, value, accent }: { label: string; value: string; accent?: boolean }) {
  return (
    <div className="text-right">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-text-dim">{label}</div>
      <div className={cn('tabular mt-0.5 text-[19px] font-bold leading-none', accent ? 'text-gradient' : 'text-text')}>
        {value}
      </div>
    </div>
  )
}

// ── Daily active-hours bars ───────────────────────────────────────────────────
interface DailyRow {
  date: string
  hours: number
  active: number
  loggedon: number
}

interface DailyTipItem {
  payload?: DailyRow
}

function DailyTooltip({ active, payload }: { active?: boolean; payload?: DailyTipItem[] }) {
  const t = useT()
  const row = active && payload && payload.length > 0 ? payload[0].payload : undefined
  if (!row) return null
  return (
    <div className="glass rounded-[var(--radius-md)] border border-border-2 px-3 py-2 shadow-[var(--shadow-pop)]">
      <div className="mb-1 text-[11px] font-medium text-text-dim">{formatShortDate(row.date)}</div>
      <div className="flex items-center gap-2 text-[12px]">
        <span className="size-2 rounded-full" style={{ backgroundColor: chart.accent }} aria-hidden />
        <span className="text-text-muted">{t('userDetail.active')}</span>
        <span className="tabular ml-3 font-semibold text-text">{formatMinutes(row.active)}</span>
      </div>
      <div className="mt-0.5 text-[11px] text-text-dim">{t('userDetail.sessionShort', { value: formatMinutes(row.loggedon) })}</div>
    </div>
  )
}

function DailyActivityBars({ daily }: { daily: DailyActivity[] }) {
  const t = useT()
  const reduce = useReducedMotion()
  const resolve = useResolveColor()
  const grid = resolve(chart.grid)
  const axisText = resolve(chart.axisText)
  const rows: DailyRow[] = daily.map((d) => ({
    date: d.date,
    hours: Math.round((d.active_minutes / 60) * 10) / 10,
    active: d.active_minutes,
    loggedon: d.loggedon_minutes,
  }))
  return (
    <ResponsiveContainer width="100%" height={220}>
      <BarChart data={rows} margin={{ top: 6, right: 8, bottom: 0, left: 0 }}>
        <CartesianGrid stroke={grid} vertical={false} />
        <XAxis
          dataKey="date"
          tickFormatter={(v: string) => formatShortDate(v)}
          tick={{ fill: axisText, fontSize: 11 }}
          tickLine={false}
          axisLine={{ stroke: grid }}
          minTickGap={18}
          interval="preserveStartEnd"
        />
        <YAxis
          width={44}
          tick={{ fill: axisText, fontSize: 11 }}
          tickFormatter={(v: number) => t('userDetail.hoursTick', { value: v })}
          tickLine={false}
          axisLine={false}
          allowDecimals={false}
        />
        <Tooltip cursor={{ fill: resolve(chart.band) }} content={<DailyTooltip />} />
        <Bar
          dataKey="hours"
          name={t('userDetail.activeHours')}
          fill={resolve(chart.accent)}
          radius={[4, 4, 0, 0]}
          maxBarSize={26}
          isAnimationActive={!reduce}
          animationDuration={380}
        />
      </BarChart>
    </ResponsiveContainer>
  )
}

// ── Logon records (which device, which day, which hours) ──────────────────────
interface LogonDevice {
  machine_id: string
  hostname: string
  count: number
  first: string
  /** null = a record on this device is still running. */
  last: string | null
}

/** Collapse logon records (newest first) into one row per device. */
function summarizeDevices(logons: UserLogon[]): LogonDevice[] {
  const byId = new Map<string, LogonDevice>()
  for (const l of logons) {
    const d = byId.get(l.machine_id)
    if (!d) {
      byId.set(l.machine_id, {
        machine_id: l.machine_id, hostname: l.hostname, count: 1, first: l.started_at, last: l.ended_at,
      })
    } else {
      d.count += 1
      d.first = l.started_at // rows arrive newest first, so the last one seen is the earliest
    }
  }
  return [...byId.values()]
}

// Built per render: the headers follow the active language.
const logonColumns = (t: TFn): Column<UserLogon>[] => [
  {
    key: 'date',
    header: t('userDetail.date'),
    sortable: true,
    sortValue: (l) => new Date(l.started_at).getTime(),
    render: (l) => <span className="tabular">{formatDate(l.started_at)}</span>,
  },
  {
    key: 'device',
    header: t('userDetail.device'),
    sortable: true,
    sortValue: (l) => l.hostname,
    render: (l) => (
      <Link to={`/machines/${encodeURIComponent(l.machine_id)}`} className="font-medium text-text hover:text-accent ring-focus">
        {l.hostname}
      </Link>
    ),
  },
  {
    key: 'start',
    header: t('userDetail.start'),
    render: (l) => <span className="tabular">{formatTime(l.started_at)}</span>,
  },
  {
    key: 'end',
    header: t('userDetail.end'),
    render: (l) =>
      l.ended_at === null ? (
        <Badge tone="ok" dot pulse>{t('userDetail.ongoing')}</Badge>
      ) : (
        <span className="tabular">
          {/* Show the date too when the record ran past midnight. */}
          {formatDate(l.ended_at) === formatDate(l.started_at) ? formatTime(l.ended_at) : formatDayTime(l.ended_at)}
        </span>
      ),
  },
  {
    key: 'span',
    header: t('userDetail.duration'),
    align: 'right',
    sortable: true,
    sortValue: (l) => (l.ended_at ? new Date(l.ended_at).getTime() : Date.now()) - new Date(l.started_at).getTime(),
    render: (l) => <span className="tabular">{formatSpan(l.started_at, l.ended_at)}</span>,
  },
  {
    key: 'active',
    header: t('userDetail.activeUse'),
    align: 'right',
    sortable: true,
    sortValue: (l) => l.active_seconds,
    render: (l) => <span className="tabular text-text-muted">{formatDuration(l.active_seconds)}</span>,
  },
]

function LogonHistory({ logons, devices }: { logons: UserLogon[]; devices: LogonDevice[] }) {
  const t = useT()
  return (
    <div className="space-y-4">
      <ul className="grid gap-2.5 sm:grid-cols-2 lg:grid-cols-3">
        {devices.map((d) => (
          <li key={d.machine_id} className="rounded-[var(--radius-md)] border border-border bg-surface/60 px-3.5 py-3">
            <div className="flex items-center justify-between gap-2">
              <Link to={`/machines/${encodeURIComponent(d.machine_id)}`} className="truncate text-[14px] font-semibold text-text hover:text-accent ring-focus">
                {d.hostname}
              </Link>
              <span className="tabular shrink-0 text-[12px] text-text-dim">{t('common.recordCount', { count: d.count })}</span>
            </div>
            <dl className="mt-1.5 space-y-0.5 text-[12px] text-text-muted">
              <div className="flex justify-between gap-2">
                <dt>{t('common.firstSeen')}</dt>
                <dd className="tabular">{formatDateTime(d.first)}</dd>
              </div>
              <div className="flex justify-between gap-2">
                <dt>{t('common.lastSeen')}</dt>
                <dd className="tabular">{d.last === null ? t('userDetail.inSessionNow') : formatDateTime(d.last)}</dd>
              </div>
            </dl>
          </li>
        ))}
      </ul>
      <DataTable
        columns={logonColumns(t)}
        rows={logons}
        getRowKey={(l) => `${l.machine_id}|${l.started_at}`}
        pageSize={25}
      />
      <p className="text-[12px] leading-relaxed text-text-dim">
        {t('userDetail.logonsNote')}
      </p>
    </div>
  )
}

// ── Per-app active-use bars (working-hours productivity) ───────────────────────────────
function AppUsageList({ apps }: { apps: AppUsage[] }) {
  const rows = (apps ?? []).slice(0, 12)
  const max = Math.max(1, ...rows.map((a) => a.seconds))
  return (
    <ul className="space-y-2.5">
      {rows.map((a) => (
        <li key={a.name} className="flex items-center gap-3">
          <span className="w-28 shrink-0 truncate font-mono text-[13px] text-text" title={a.name}>
            {a.name}
          </span>
          <span className="h-2 flex-1 overflow-hidden rounded-full bg-wash-2">
            <span
              className="block h-full rounded-full bg-[color:var(--color-metric-cpu)]"
              style={{ width: `${Math.max(2, (a.seconds / max) * 100)}%` }}
            />
          </span>
          <span className="tabular w-20 shrink-0 text-right text-[13px] font-semibold text-text">
            {formatDuration(a.seconds)}
          </span>
        </li>
      ))}
    </ul>
  )
}

// ── Display-name + department editor (admin) ──────────────────────────────────
function ProfileEditor({
  open,
  onClose,
  user,
  initialName,
  initialTags,
}: {
  open: boolean
  onClose: () => void
  user: string
  initialName: string
  initialTags: string[]
}) {
  const t = useT()
  const update = useUpdateUserProfile()
  const { push } = useToast()
  const [name, setName] = useState(initialName)
  const [tags, setTags] = useState<string[]>(initialTags)
  const [tagInput, setTagInput] = useState('')

  const addTag = () => {
    const v = tagInput.trim()
    if (!v) return
    if (!tags.some((x) => sameText(x, v))) {
      setTags([...tags, v])
    }
    setTagInput('')
  }

  const removeTag = (v: string) => setTags((list) => list.filter((x) => x !== v))

  const save = () => {
    update.mutate(
      { user, profile: { display_name: name.trim(), tags } },
      {
        onSuccess: () => {
          push({ title: t('userDetail.profileSaved'), tone: 'ok' })
          onClose()
        },
        onError: () => push({ title: t('userDetail.profileSaveFailed'), tone: 'crit' }),
      },
    )
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={t('userDetail.profileTitle')}
      description={displayUser(user)}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>{t('common.cancel')}</Button>
          <Button variant="primary" leftIcon={<Save className="size-4" aria-hidden />} loading={update.isPending} onClick={save}>
            {t('common.save')}
          </Button>
        </>
      }
    >
      <div className="space-y-4 pb-2">
        <Input
          label={t('userDetail.displayName')}
          placeholder={displayUser(user)}
          value={name}
          onChange={(e) => setName(e.target.value)}
          hint={t('userDetail.displayNameHint')}
        />
        <div>
          <span className="mb-1.5 block text-[13px] font-medium text-text-muted">{t('userDetail.departments')}</span>
          <div className="flex flex-wrap items-center gap-1.5">
            {tags.length === 0 && <span className="text-[13px] text-text-dim">{t('userDetail.noDepartments')}</span>}
            {tags.map((tag) => (
              <span
                key={tag}
                className="inline-flex items-center gap-1 rounded-full border border-accent/25 bg-accent/12 py-0.5 pl-2.5 pr-1 text-[12px] font-medium text-accent"
              >
                {tag}
                <button
                  type="button"
                  onClick={() => removeTag(tag)}
                  aria-label={t('userDetail.removeDepartment', { tag })}
                  className="grid size-4 place-items-center rounded-full text-accent/70 transition-colors hover:bg-accent/20 hover:text-accent ring-focus"
                >
                  <X className="size-3" aria-hidden />
                </button>
              </span>
            ))}
          </div>
          <div className="mt-2.5 flex items-center gap-2">
            <Input
              placeholder={t('userDetail.departmentPlaceholder')}
              value={tagInput}
              onChange={(e) => setTagInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  addTag()
                }
              }}
            />
            <Button variant="secondary" leftIcon={<Plus className="size-4" aria-hidden />} onClick={addTag} disabled={!tagInput.trim()}>
              {t('common.add')}
            </Button>
          </div>
        </div>
      </div>
    </Modal>
  )
}
