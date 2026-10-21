import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { Archive, ArchiveRestore, CheckCircle2, Info, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/Button'
import { Card, CardHeader } from '@/components/ui/Card'
import { Checkbox } from '@/components/ui/Checkbox'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { Select } from '@/components/ui/Select'
import { DataTable } from '@/components/DataTable'
import type { Column } from '@/components/DataTable'
import { EmptyState } from '@/components/EmptyState'
import { SkeletonText } from '@/components/Skeleton'
import { useArchiveMachines, useArchivedMachines, useFleet, usePurgeMachines, useRestoreMachines } from '@/hooks/queries'
import { useToast } from '@/hooks/useToast'
import { errorText } from '@/lib/errors'
import { displayUser, formatDate, relativeTime } from '@/lib/format'
import { kindLabel } from '@/lib/labels'
import type { ArchivedMachine, FleetMachine } from '@/lib/types'
import { useT } from '@/i18n'
import { Trans } from '@/i18n/rich'

const DAY_MS = 24 * 60 * 60 * 1000

const STALE_DAYS = [7, 14, 30, 60, 90] as const

/**
 * "Makineler" — retiring PCs that left the fleet. Archiving hides a machine from
 * every current-state view but keeps its history, and an archived machine whose
 * agent reports again comes back on its own. The one destructive action is
 * "Kalıcı Sil" on an archived machine, gated by typing its name.
 */
export function MachinesSection() {
  const t = useT()
  return (
    <div className="space-y-4">
      <p className="flex items-start gap-2 text-[13px] text-text-muted">
        <Info className="mt-0.5 size-4 shrink-0 text-text-dim" aria-hidden />
        <span>
          {t('settings.machines.intro')}
        </span>
      </p>
      <StaleMachines />
      <ArchivedList />
    </div>
  )
}

function StaleMachines() {
  const t = useT()
  const fleet = useFleet()
  const archive = useArchiveMachines()
  const { push } = useToast()
  const [days, setDays] = useState(30)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [confirmOpen, setConfirmOpen] = useState(false)

  const stale = useMemo(() => {
    const cutoff = Date.now() - days * DAY_MS
    return (fleet.data ?? [])
      .filter((m) => new Date(m.last_seen).getTime() < cutoff)
      .sort((a, b) => a.last_seen.localeCompare(b.last_seen))
  }, [fleet.data, days])

  // A selection only ever covers rows currently on screen: changing the threshold
  // must not leave an invisible machine ticked and then archive it.
  const visibleSelected = stale.filter((m) => selected.has(m.machine_id))
  const allChecked = stale.length > 0 && visibleSelected.length === stale.length

  const toggle = (id: string, on: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (on) next.add(id)
      else next.delete(id)
      return next
    })

  const commit = () => {
    const ids = visibleSelected.map((m) => m.machine_id)
    archive.mutate(ids, {
      onSuccess: (res) => {
        setConfirmOpen(false)
        setSelected(new Set())
        push({
          title: t('settings.machines.retired'),
          description: t('settings.machines.retiredBody', { count: res?.changed ?? ids.length }),
          tone: 'ok',
        })
      },
      onError: () => push({ title: t('settings.machines.retireFailed'), tone: 'crit' }),
    })
  }

  const columns: Column<FleetMachine>[] = [
    {
      key: 'select',
      header: (
        <Checkbox
          size="sm"
          checked={allChecked}
          indeterminate={visibleSelected.length > 0 && !allChecked}
          onChange={(on) => setSelected(on ? new Set(stale.map((m) => m.machine_id)) : new Set())}
          ariaLabel={t('settings.machines.selectAll')}
        />
      ),
      className: 'w-10',
      render: (m) => (
        <Checkbox
          size="sm"
          checked={selected.has(m.machine_id)}
          onChange={(on) => toggle(m.machine_id, on)}
          ariaLabel={t('settings.machines.selectOne', { host: m.hostname })}
        />
      ),
    },
    {
      key: 'machine',
      header: t('common.machine'),
      sortable: true,
      sortValue: (m) => m.hostname,
      render: (m) => (
        <div className="min-w-0">
          <Link to={`/machines/${m.machine_id}`} className="truncate font-medium text-accent hover:underline ring-focus">
            {m.hostname}
          </Link>
          <p className="text-[12px] text-text-dim">
            {kindLabel(m.kind)}
            {m.agent_version ? t('settings.machines.agentVersion', { version: m.agent_version }) : ''}
          </p>
        </div>
      ),
    },
    {
      key: 'user',
      header: t('settings.machines.lastUser'),
      render: (m) => <span className="text-text-muted">{displayUser(m.logon_user)}</span>,
    },
    {
      key: 'last',
      header: t('common.lastSeen'),
      align: 'right',
      sortable: true,
      sortValue: (m) => m.last_seen,
      render: (m) => (
        <div className="text-right">
          <span className="text-text">{relativeTime(m.last_seen)}</span>
          <p className="tabular text-[12px] text-text-dim">{formatDate(m.last_seen)}</p>
        </div>
      ),
    },
  ]

  return (
    <Card padded={false}>
      <CardHeader
        title={t('settings.machines.staleTitle')}
        description={t('settings.machines.staleBody')}
        action={
          <div className="w-64">
            <Select
              options={STALE_DAYS.map((d) => ({ value: String(d), label: t('settings.machines.staleOption', { count: d }) }))}
              value={String(days)}
              onChange={(e) => setDays(Number(e.target.value))}
              aria-label={t('settings.machines.staleSelect')}
            />
          </div>
        }
        className="p-5 pb-4"
      />
      <div className="px-5 pb-5">
        {fleet.isLoading ? (
          <SkeletonText lines={5} />
        ) : stale.length === 0 ? (
          <EmptyState
            icon={CheckCircle2}
            title={t('settings.machines.noneTitle')}
            description={t('settings.machines.noneBody', { count: Number(days) })}
          />
        ) : (
          <>
            <DataTable
              columns={columns}
              rows={stale}
              getRowKey={(m) => m.machine_id}
              initialSort={{ key: 'last', dir: 'asc' }}
              pageSize={20}
              className="border-0"
            />
            <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
              <p className="text-[13px] text-text-muted">
                <Trans k="settings.machines.selectedOf" values={{ total: stale.length, selected: visibleSelected.length }} tags={{ b: (c) => <span className="tabular font-medium text-text">{c}</span> }} />
              </p>
              <Button
                leftIcon={<Archive className="size-4" aria-hidden />}
                disabled={visibleSelected.length === 0}
                onClick={() => setConfirmOpen(true)}
              >
                {t('settings.machines.retireSelected')}
              </Button>
            </div>
          </>
        )}
      </div>

      <ConfirmDialog
        open={confirmOpen}
        onClose={() => setConfirmOpen(false)}
        onConfirm={commit}
        loading={archive.isPending}
        tone="primary"
        title={t('settings.machines.retireTitle')}
        confirmLabel={t('settings.machines.retireConfirm')}
        description={
          <Trans k="settings.machines.retireConfirmBody" values={{ count: visibleSelected.length }} tags={{ b: (c) => <strong className="tabular font-semibold text-text">{c}</strong> }} />
        }
      >
        <p className="max-h-40 overflow-y-auto rounded-[var(--radius-md)] border border-border bg-surface-2/40 p-3 text-[13px] text-text-muted">
          {visibleSelected.map((m) => m.hostname).join(', ')}
        </p>
      </ConfirmDialog>
    </Card>
  )
}

function ArchivedList() {
  const t = useT()
  const archived = useArchivedMachines()
  const restore = useRestoreMachines()
  const purge = usePurgeMachines()
  const { push } = useToast()
  const [purgeTarget, setPurgeTarget] = useState<ArchivedMachine | null>(null)

  const onPurge = () => {
    const m = purgeTarget
    if (!m) return
    purge.mutate([m.machine_id], {
      onSuccess: () => {
        setPurgeTarget(null)
        push({ title: t('settings.machines.purged', { host: m.hostname }), tone: 'ok' })
      },
      onError: (err) => push({ title: t('settings.machines.purgeFailed'), description: errorText(err), tone: 'crit' }),
    })
  }

  const onRestore = (m: ArchivedMachine) =>
    restore.mutate([m.machine_id], {
      onSuccess: () => push({ title: t('settings.machines.restored', { host: m.hostname }), tone: 'ok' }),
      onError: () => push({ title: t('settings.machines.restoreFailed'), tone: 'crit' }),
    })

  const columns: Column<ArchivedMachine>[] = [
    {
      key: 'machine',
      header: t('common.machine'),
      sortable: true,
      sortValue: (m) => m.hostname,
      render: (m) => (
        <div className="min-w-0">
          <Link to={`/machines/${m.machine_id}`} className="truncate font-medium text-accent hover:underline ring-focus">
            {m.hostname}
          </Link>
          <p className="text-[12px] text-text-dim">
            {m.os || kindLabel(m.kind)}
            {m.agent_version ? t('settings.machines.agentVersion', { version: m.agent_version }) : ''}
          </p>
        </div>
      ),
    },
    {
      key: 'last',
      header: t('common.lastSeen'),
      sortable: true,
      sortValue: (m) => m.last_seen ?? '',
      render: (m) => <span className="tabular text-text-muted">{m.last_seen ? formatDate(m.last_seen) : '—'}</span>,
    },
    {
      key: 'archived',
      header: t('settings.machines.archivedAt'),
      sortable: true,
      sortValue: (m) => m.archived_at,
      render: (m) => (
        <div>
          <span className="tabular text-text">{formatDate(m.archived_at)}</span>
          <p className="text-[12px] text-text-dim">{m.archived_by || '—'}</p>
        </div>
      ),
    },
    {
      key: 'actions',
      header: '',
      align: 'right',
      render: (m) => (
        <div className="flex justify-end gap-1">
          <Button
            size="sm"
            variant="ghost"
            leftIcon={<ArchiveRestore className="size-4" aria-hidden />}
            onClick={() => onRestore(m)}
            loading={restore.isPending && restore.variables?.[0] === m.machine_id}
          >
            {t('common.undo')}
          </Button>
          <Button
            size="sm"
            variant="danger"
            leftIcon={<Trash2 className="size-4" aria-hidden />}
            onClick={() => setPurgeTarget(m)}
          >
            {t('settings.machines.purge')}
          </Button>
        </div>
      ),
    },
  ]

  const rows = archived.data?.machines ?? []

  return (
    <Card padded={false}>
      <CardHeader
        title={t('settings.machines.archiveTitle')}
        description={t('settings.machines.archiveBody')}
        className="p-5 pb-4"
      />
      <div className="px-5 pb-5">
        {archived.isLoading ? (
          <SkeletonText lines={4} />
        ) : archived.isError ? (
          <EmptyState
            icon={Archive}
            title={t('settings.machines.archiveLoadFailed')}
            description={t('settings.machines.notSupported')}
          />
        ) : rows.length === 0 ? (
          <EmptyState icon={Archive} title={t('settings.machines.archiveEmptyTitle')} description={t('settings.machines.archiveEmptyBody')} />
        ) : (
          <DataTable
            columns={columns}
            rows={rows}
            getRowKey={(m) => m.machine_id}
            initialSort={{ key: 'archived', dir: 'desc' }}
            pageSize={20}
            className="border-0"
          />
        )}
      </div>

      <ConfirmDialog
        open={purgeTarget !== null}
        onClose={() => setPurgeTarget(null)}
        onConfirm={onPurge}
        loading={purge.isPending}
        tone="danger"
        title={t('settings.machines.purgeTitle', { host: purgeTarget?.hostname ?? '' })}
        confirmLabel={t('settings.machines.purgeConfirm')}
        description={
          <Trans
            k="settings.machines.purgeConfirmBody"
            values={{ host: purgeTarget?.hostname ?? '' }}
            tags={{ b: (c) => <strong className="font-semibold text-text">{c}</strong> }}
          />
        }
        requireText={purgeTarget?.hostname}
        requireTextLabel={t('settings.machines.purgeTypeLabel')}
      />
    </Card>
  )
}
