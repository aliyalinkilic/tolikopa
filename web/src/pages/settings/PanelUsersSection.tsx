import { useEffect, useRef, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { KeyRound, Pencil, ShieldOff, Trash2, UserCheck, UserPlus, UserX } from 'lucide-react'
import { Card, CardHeader } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { Modal } from '@/components/ui/Modal'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { DataTable } from '@/components/DataTable'
import type { Column } from '@/components/DataTable'
import { EmptyState } from '@/components/EmptyState'
import { SkeletonText } from '@/components/Skeleton'
import { PasswordRules, passwordAcceptable } from '@/components/PasswordRules'
import {
  useCreatePanelUser, useDeletePanelUser, useGeneralSettings, usePanelUsers, useResetPanelUser2fa,
  useResetPanelUserPassword, useUpdatePanelUser,
} from '@/hooks/queries'
import { useAuth } from '@/hooks/useAuth'
import { useToast } from '@/hooks/useToast'
import { errorCode, errorText } from '@/lib/errors'
import { formatDateTime, initialOf, relativeTime } from '@/lib/format'
import { useT } from '@/i18n'
import type { TKey } from '@/i18n'
import type { PanelUser } from '@/lib/types'

// docs/API.md §7.1 — letters, digits and . _ - @, 1–64 characters.
const USERNAME_RE = /^[\p{L}\p{N}._@-]{1,64}$/u

type Pending =
  | { kind: 'edit'; user: PanelUser }
  | { kind: 'disable'; user: PanelUser }
  | { kind: 'password'; user: PanelUser }
  | { kind: 'twofa'; user: PanelUser }
  | { kind: 'delete'; user: PanelUser }
  | null

function roleKey(role: string): TKey {
  return role === 'admin' ? 'common.role.admin' : 'common.role.viewer'
}

/**
 * Settings → Panel Users: who can sign in, with what role, in what state. The
 * signed-in admin's own row offers only "Edit" (display name) — the server refuses
 * self-delete/disable/demote and self password/2FA resets (`cannot_modify_self`),
 * so those are not offered at all instead of failing on click.
 */
export function PanelUsersSection() {
  const t = useT()
  const { user: me } = useAuth()
  const { push } = useToast()
  const { data, isLoading, isError } = usePanelUsers()
  const general = useGeneralSettings()
  const update = useUpdatePanelUser()
  const resetPassword = useResetPanelUserPassword()
  const reset2fa = useResetPanelUser2fa()
  const remove = useDeletePanelUser()

  const [adding, setAdding] = useState(false)
  const [pending, setPending] = useState<Pending>(null)
  const [newPassword, setNewPassword] = useState('')

  const policyOn = general.data?.require_2fa === true
  const isSelf = (u: PanelUser) => !!me && u.username.toLowerCase() === me.username.toLowerCase()
  const close = () => setPending(null)

  useEffect(() => {
    if (pending?.kind === 'password') setNewPassword('')
  }, [pending])

  const fail = (title: TKey) => (err: unknown) => push({ title: t(title), description: errorText(err), tone: 'crit' })

  const setDisabled = (u: PanelUser, disabled: boolean) =>
    update.mutate(
      { username: u.username, patch: { disabled } },
      {
        onSuccess: () => {
          close()
          push({
            title: t(disabled ? 'settings.users.disabledToast' : 'settings.users.enabledToast', { user: u.username }),
            tone: disabled ? 'warn' : 'ok',
          })
        },
        onError: fail('settings.users.updateFailed'),
      },
    )

  const columns: Column<PanelUser>[] = [
    {
      key: 'user',
      header: t('settings.users.colUser'),
      sortable: true,
      sortValue: (u) => u.username,
      render: (u) => (
        <div className="flex min-w-0 items-center gap-3">
          <span
            className={
              u.disabled
                ? 'grid size-9 shrink-0 place-items-center rounded-full bg-wash-2 text-[14px] font-bold text-text-dim'
                : 'grid size-9 shrink-0 place-items-center rounded-full bg-accent-grad-soft text-[14px] font-bold text-accent'
            }
            aria-hidden
          >
            {initialOf(u.display_name || u.username)}
          </span>
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="truncate font-medium text-text">{u.display_name || u.username}</span>
              {isSelf(u) && <Badge tone="accent">{t('settings.users.you')}</Badge>}
            </div>
            {u.display_name && <div className="truncate font-mono text-[12px] text-text-dim">{u.username}</div>}
          </div>
        </div>
      ),
    },
    {
      key: 'role',
      header: t('settings.users.colRole'),
      sortable: true,
      sortValue: (u) => u.role,
      render: (u) => <Badge tone={u.role === 'admin' ? 'accent' : 'neutral'}>{t(roleKey(u.role))}</Badge>,
    },
    {
      key: 'status',
      header: t('settings.users.colStatus'),
      sortable: true,
      sortValue: (u) => (u.disabled ? 1 : 0),
      render: (u) =>
        u.disabled ? (
          <Badge tone="offline" dot>
            {t('settings.users.disabled')}
          </Badge>
        ) : (
          <Badge tone="ok" dot>
            {t('settings.users.active')}
          </Badge>
        ),
    },
    {
      key: 'twofa',
      header: t('settings.users.col2fa'),
      sortable: true,
      sortValue: (u) => (u.totp_enabled ? 0 : 1),
      render: (u) =>
        u.totp_enabled ? (
          <Badge tone="ok">{t('settings.users.twofaOn')}</Badge>
        ) : policyOn && !u.disabled ? (
          <Badge tone="warn" title={t('settings.users.twofaPendingTitle')}>
            {t('settings.users.twofaPending')}
          </Badge>
        ) : (
          <Badge tone="neutral">{t('settings.users.twofaOff')}</Badge>
        ),
    },
    {
      key: 'last_login',
      header: t('settings.users.colLastLogin'),
      sortable: true,
      sortValue: (u) => u.last_login_at ?? '',
      render: (u) =>
        u.last_login_at ? (
          <span className="tabular text-text-muted" title={formatDateTime(u.last_login_at)}>
            {relativeTime(u.last_login_at)}
          </span>
        ) : (
          <span className="text-text-dim">{t('settings.users.never')}</span>
        ),
    },
    {
      key: 'actions',
      header: <span className="sr-only">{t('settings.users.colActions')}</span>,
      align: 'right',
      render: (u) => {
        const self = isSelf(u)
        return (
          <div
            className="flex items-center justify-end gap-0.5"
            role="group"
            aria-label={t('settings.users.actionsFor', { user: u.username })}
          >
            <RowAction label={t('settings.users.edit')} onClick={() => setPending({ kind: 'edit', user: u })}>
              <Pencil className="size-4" aria-hidden />
            </RowAction>
            {!self && (
              <>
                {u.disabled ? (
                  <RowAction label={t('settings.users.enable')} onClick={() => setDisabled(u, false)}>
                    <UserCheck className="size-4" aria-hidden />
                  </RowAction>
                ) : (
                  <RowAction label={t('settings.users.disable')} onClick={() => setPending({ kind: 'disable', user: u })}>
                    <UserX className="size-4" aria-hidden />
                  </RowAction>
                )}
                <RowAction label={t('settings.users.resetPassword')} onClick={() => setPending({ kind: 'password', user: u })}>
                  <KeyRound className="size-4" aria-hidden />
                </RowAction>
                {u.totp_enabled && (
                  <RowAction label={t('settings.users.reset2fa')} onClick={() => setPending({ kind: 'twofa', user: u })}>
                    <ShieldOff className="size-4" aria-hidden />
                  </RowAction>
                )}
                <RowAction danger label={t('settings.users.delete')} onClick={() => setPending({ kind: 'delete', user: u })}>
                  <Trash2 className="size-4" aria-hidden />
                </RowAction>
              </>
            )}
          </div>
        )
      },
    },
  ]

  // Dialog titles keep naming the user while the dialog animates out.
  const lastTarget = useRef<PanelUser | null>(null)
  if (pending) lastTarget.current = pending.user
  const target = pending?.user ?? lastTarget.current ?? undefined

  return (
    <Card padded={false}>
      <CardHeader
        title={t('settings.users.title')}
        description={t('settings.users.body')}
        className="p-5 pb-4"
        action={
          <Button size="sm" variant="primary" leftIcon={<UserPlus className="size-4" aria-hidden />} onClick={() => setAdding(true)}>
            {t('settings.users.add')}
          </Button>
        }
      />
      <div className="px-5 pb-5">
        {isLoading ? (
          <SkeletonText lines={4} />
        ) : isError ? (
          <EmptyState icon={UserX} title={t('settings.users.loadFailed')} />
        ) : !data || data.length === 0 ? (
          <EmptyState icon={UserPlus} title={t('settings.users.empty')} />
        ) : (
          <>
            <DataTable columns={columns} rows={data} getRowKey={(u) => u.username} initialSort={{ key: 'user', dir: 'asc' }} />
            <p className="mt-3 text-[12px] text-text-dim">{t('settings.users.selfNote')}</p>
          </>
        )}
      </div>

      <AddUserDialog open={adding} onClose={() => setAdding(false)} />

      <EditUserDialog user={pending?.kind === 'edit' ? pending.user : null} self={!!target && isSelf(target)} onClose={close} />

      <ConfirmDialog
        open={pending?.kind === 'disable'}
        onClose={close}
        title={t('settings.users.disableTitle', { user: target?.username })}
        description={t('settings.users.disableBody')}
        confirmLabel={t('settings.users.disable')}
        loading={update.isPending}
        onConfirm={() => target && setDisabled(target, true)}
      />

      <ConfirmDialog
        open={pending?.kind === 'password'}
        onClose={close}
        title={t('settings.users.resetPasswordTitle', { user: target?.username })}
        description={t('settings.users.resetPasswordBody')}
        confirmLabel={t('settings.users.resetPasswordConfirm')}
        loading={resetPassword.isPending}
        confirmDisabled={!passwordAcceptable(newPassword)}
        onConfirm={() =>
          target &&
          resetPassword.mutate(
            { username: target.username, new_password: newPassword },
            {
              onSuccess: () => {
                close()
                push({ title: t('settings.users.passwordReset', { user: target.username }), tone: 'ok' })
              },
              onError: fail('settings.users.passwordResetFailed'),
            },
          )
        }
      >
        <div className="space-y-2">
          <Input
            label={t('settings.users.newPassword')}
            type="password"
            autoComplete="new-password"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
          />
          <PasswordRules password={newPassword} />
        </div>
      </ConfirmDialog>

      <ConfirmDialog
        open={pending?.kind === 'twofa'}
        onClose={close}
        title={t('settings.users.reset2faTitle', { user: target?.username })}
        description={policyOn ? t('settings.users.reset2faBodyPolicy') : t('settings.users.reset2faBody')}
        confirmLabel={t('settings.users.reset2faConfirm')}
        loading={reset2fa.isPending}
        onConfirm={() =>
          target &&
          reset2fa.mutate(target.username, {
            onSuccess: () => {
              close()
              push({ title: t('settings.users.reset2faDone', { user: target.username }), tone: 'warn' })
            },
            onError: fail('settings.users.reset2faFailed'),
          })
        }
      />

      <ConfirmDialog
        open={pending?.kind === 'delete'}
        onClose={close}
        title={t('settings.users.deleteTitle', { user: target?.username })}
        description={t('settings.users.deleteBody')}
        confirmLabel={t('settings.users.delete')}
        acknowledgeLabel={t('settings.users.deleteAck')}
        loading={remove.isPending}
        onConfirm={() =>
          target &&
          remove.mutate(target.username, {
            onSuccess: () => {
              close()
              push({ title: t('settings.users.deleted', { user: target.username }), tone: 'ok' })
            },
            onError: fail('settings.users.deleteFailed'),
          })
        }
      />
    </Card>
  )
}

function RowAction({
  label,
  onClick,
  danger,
  children,
}: {
  label: string
  onClick: () => void
  danger?: boolean
  children: ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={label}
      className={
        danger
          ? 'grid size-8 place-items-center rounded-full text-text-muted transition-colors hover:bg-crit/12 hover:text-crit ring-focus'
          : 'grid size-8 place-items-center rounded-full text-text-muted transition-colors hover:bg-wash-2 hover:text-text ring-focus'
      }
    >
      {children}
    </button>
  )
}

function useRoleOptions() {
  const t = useT()
  return [
    { value: 'admin', label: t('common.role.admin') },
    { value: 'viewer', label: t('common.role.viewer') },
  ]
}

function AddUserDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const t = useT()
  const { push } = useToast()
  const create = useCreatePanelUser()
  const roles = useRoleOptions()
  const [form, setForm] = useState({ username: '', display_name: '', password: '', role: 'viewer' })
  const [usernameError, setUsernameError] = useState<string | null>(null)
  const [passwordError, setPasswordError] = useState<string | null>(null)

  useEffect(() => {
    if (open) {
      setForm({ username: '', display_name: '', password: '', role: 'viewer' })
      setUsernameError(null)
      setPasswordError(null)
    }
  }, [open])

  const username = form.username.trim()
  const usernameOk = USERNAME_RE.test(username)
  const ready = usernameOk && passwordAcceptable(form.password)

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!ready) return
    setUsernameError(null)
    setPasswordError(null)
    create.mutate(
      {
        username,
        password: form.password,
        role: form.role,
        ...(form.display_name.trim() ? { display_name: form.display_name.trim() } : {}),
      },
      {
        onSuccess: () => {
          onClose()
          push({ title: t('settings.users.created'), tone: 'ok' })
        },
        onError: (err) => {
          const code = errorCode(err)
          if (code === 'username_taken') setUsernameError(errorText(err))
          else if (code === 'password_too_weak' || code === 'password_too_long') setPasswordError(errorText(err))
          else push({ title: t('settings.users.createFailed'), description: errorText(err), tone: 'crit' })
        },
      },
    )
  }

  return (
    <Modal open={open} onClose={onClose} title={t('settings.users.addTitle')}>
      <form onSubmit={submit} className="space-y-4 pb-5">
        <Input
          label={t('settings.users.username')}
          hint={t('settings.users.usernameHint')}
          error={usernameError ?? (username !== '' && !usernameOk ? t('settings.users.usernameInvalid') : undefined)}
          value={form.username}
          maxLength={64}
          onChange={(e) => {
            setForm({ ...form, username: e.target.value })
            setUsernameError(null)
          }}
          autoComplete="off"
          autoCapitalize="none"
          spellCheck={false}
          autoFocus
        />
        <Input
          label={t('settings.users.displayName')}
          hint={t('settings.users.displayNameHint')}
          value={form.display_name}
          maxLength={80}
          onChange={(e) => setForm({ ...form, display_name: e.target.value })}
          autoComplete="off"
        />
        <div className="space-y-2">
          <Input
            label={t('settings.users.password')}
            type="password"
            error={passwordError ?? undefined}
            value={form.password}
            onChange={(e) => {
              setForm({ ...form, password: e.target.value })
              setPasswordError(null)
            }}
            autoComplete="new-password"
          />
          <PasswordRules password={form.password} />
        </div>
        <Select label={t('settings.users.role')} options={roles} value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value })} />
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose} disabled={create.isPending}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" variant="primary" loading={create.isPending} disabled={!ready}>
            {t('common.create')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

function EditUserDialog({ user, self, onClose }: { user: PanelUser | null; self: boolean; onClose: () => void }) {
  const t = useT()
  const { push } = useToast()
  const { refresh } = useAuth()
  const update = useUpdatePanelUser()
  const roles = useRoleOptions()
  const [displayName, setDisplayName] = useState('')
  const [role, setRole] = useState('viewer')
  // Keep the last user while the dialog animates out, so its title does not blank.
  const [shown, setShown] = useState<PanelUser | null>(null)

  useEffect(() => {
    if (user) {
      setShown(user)
      setDisplayName(user.display_name ?? '')
      setRole(user.role)
    }
  }, [user])

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!user) return
    const patch: { display_name?: string; role?: string } = {}
    if (displayName.trim() !== (user.display_name ?? '')) patch.display_name = displayName.trim()
    if (!self && role !== user.role) patch.role = role
    if (Object.keys(patch).length === 0) {
      onClose()
      return
    }
    update.mutate(
      { username: user.username, patch },
      {
        onSuccess: () => {
          onClose()
          push({ title: t('settings.users.updated'), tone: 'ok' })
          if (self) void refresh() // own display name is shown in the top bar
        },
        onError: (err) => push({ title: t('settings.users.updateFailed'), description: errorText(err), tone: 'crit' }),
      },
    )
  }

  return (
    <Modal open={!!user} onClose={onClose} title={t('settings.users.editTitle', { user: shown?.username })}>
      <form onSubmit={submit} className="space-y-4 pb-5">
        <Input
          label={t('settings.users.displayName')}
          hint={t('settings.users.displayNameHint')}
          value={displayName}
          maxLength={80}
          onChange={(e) => setDisplayName(e.target.value)}
          autoComplete="off"
          autoFocus
        />
        <Select
          label={t('settings.users.role')}
          options={roles}
          value={role}
          disabled={self}
          onChange={(e) => setRole(e.target.value)}
        />
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose} disabled={update.isPending}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" variant="primary" loading={update.isPending}>
            {t('common.save')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
