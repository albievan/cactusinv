import { useState, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, unwrap } from '../../../lib/api/client'
import { formatDateTime } from '../../../lib/format'
import { useAsync } from '../../../lib/useAsync'
import { Empty, ErrorBanner, Field, Sheet, Spinner } from '../../../components/ui'
import { useAuth } from '../../../components/auth'
import { IconBan, IconHistory, IconPlay, IconUsers } from '../../../components/icons'
import { ChangePassword, SettingsPage } from '../Settings'
import { GroupChoice, GroupsAdmin, UserGroupsSheet } from './Groups'
import { MIN_PASSWORD, useSubmit, UserForm, YourPassword, type Group, type User } from './shared'

export { friendly } from './shared'

/**
 * UsersSettingsPage is /settings/users: who can sign in (ADR-011) and, on the Groups tab,
 * what each group may do (ADR-012).
 */
export function UsersSettingsPage() {
  const { user: me } = useAuth()
  const [params, setParams] = useSearchParams()
  const tab = params.get('tab') === 'groups' ? 'groups' : 'users'
  const users = useAsync(() => unwrap(api.GET('/users')), [])
  const groups = useAsync(() => unwrap(api.GET('/groups')), [])
  const [msg, setMsg] = useState('')
  const changed = (text: string) => {
    setMsg(text)
    users.reload()
    groups.reload()
  }
  const tabClass = (on: boolean) => `inline-flex min-h-[44px] items-center rounded-full px-4 text-sm font-semibold ${on ? 'bg-brand-600 text-white' : 'bg-white text-brand-900 ring-1 ring-cream-300'}`
  const show = (t: 'users' | 'groups') => {
    setMsg('')
    setParams(t === 'groups' ? { tab: 'groups' } : {}, { replace: true })
  }
  return (
    <SettingsPage>
      <div role="tablist" aria-label="Users and groups" className="mb-4 flex gap-2">
        <button role="tab" aria-selected={tab === 'users'} className={tabClass(tab === 'users')} onClick={() => show('users')}>
          Users
        </button>
        <button role="tab" aria-selected={tab === 'groups'} className={tabClass(tab === 'groups')} onClick={() => show('groups')}>
          Groups
        </button>
      </div>
      <ErrorBanner error={users.error ?? groups.error} />
      {msg && (
        <p role="status" className="mb-3 text-sm font-medium text-green-700">
          {msg}
        </p>
      )}
      {(users.loading && !users.data) || (groups.loading && !groups.data) ? (
        <Spinner />
      ) : tab === 'users' ? (
        <>
          <UsersAdmin users={users.data?.items ?? []} groups={groups.data?.items ?? []} meId={me?.id} onChanged={changed} />
          <ChangePassword />
        </>
      ) : (
        <GroupsAdmin groups={groups.data?.items ?? []} catalogue={groups.data?.permissions ?? []} users={users.data?.items ?? []} meId={me?.id} onChanged={changed} />
      )}
    </SettingsPage>
  )
}

type Action = { kind: 'add' } | { kind: 'reset' | 'disable' | 'enable' | 'access'; user: User }

function UsersAdmin({ users: items, groups, meId, onChanged }: { users: User[]; groups: Group[]; meId?: string; onChanged: (msg: string) => void }) {
  const [action, setAction] = useState<Action | null>(null)
  const done = (text: string) => {
    setAction(null)
    onChanged(text)
  }
  const open = (a: Action) => setAction(a)
  const active = items.filter((u) => u.active).length
  const me = { id: meId }
  return (
    <section className="space-y-4" aria-labelledby="users-heading">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id="users-heading" className="text-xl font-bold text-brand-900">
          Users
        </h2>
        <button className="btn-primary" onClick={() => open({ kind: 'add' })}>
          Add user
        </button>
      </div>
      <p className="text-sm text-slate-600">
        What a user may do comes from their groups (see the Groups tab). Users are disabled, never deleted, so the audit log keeps naming them. Nobody can
        disable themselves or the last active user.
      </p>
      {items.length === 0 ? (
        <Empty>No users.</Empty>
      ) : (
        <>
          <ul className="space-y-2 md:hidden" aria-label="Users">
            {items.map((u) => (
              <li key={u.id} className="card space-y-2">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="min-w-0 break-all font-semibold">{u.email}</span>
                  <Badges user={u} you={u.id === me?.id} />
                </div>
                <div className="text-sm">
                  <GroupChips user={u} groups={groups} />
                </div>
                <div className="text-sm text-slate-500">Last sign-in: {u.lastLoginAt ? formatDateTime(u.lastLoginAt) : 'never'}</div>
                <Actions user={u} you={u.id === me?.id} lastActive={u.active && active <= 1} onAction={open} />
              </li>
            ))}
          </ul>
          <div className="card hidden overflow-x-auto p-0 md:block">
            <table className="w-full text-sm">
              <caption className="sr-only">Users</caption>
              <thead className="bg-cream-100 text-left text-xs uppercase tracking-wide text-slate-600">
                <tr>
                  <th className="px-3 py-2">Login</th>
                  <th className="px-3 py-2">Status</th>
                  <th className="px-3 py-2">Groups</th>
                  <th className="px-3 py-2">Last sign-in</th>
                  <th className="px-3 py-2">Added</th>
                  <th className="px-3 py-2">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {items.map((u) => (
                  <tr key={u.id} className="border-t border-cream-200 align-middle">
                    <td className="min-w-[12rem] break-all px-3 py-2 font-medium">{u.email}</td>
                    <td className="px-3 py-2">
                      <div className="flex flex-wrap gap-1">
                        <Badges user={u} you={u.id === me?.id} />
                      </div>
                    </td>
                    <td className="px-3 py-2">
                      <GroupChips user={u} groups={groups} />
                    </td>
                    <td className="whitespace-nowrap px-3 py-2">{u.lastLoginAt ? formatDateTime(u.lastLoginAt) : 'never'}</td>
                    <td className="whitespace-nowrap px-3 py-2">{formatDateTime(u.createdAt)}</td>
                    <td className="px-3 py-2">
                      <Actions compact user={u} you={u.id === me?.id} lastActive={u.active && active <= 1} onAction={open} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      {action?.kind === 'add' && (
        <AddUserSheet groups={groups} onClose={() => setAction(null)} onDone={(u) => done(`${u.email} was added. Give them the password; they must change it when they first sign in.`)} />
      )}
      {action?.kind === 'access' && (
        <UserGroupsSheet user={action.user} groups={groups} meId={meId} onClose={() => setAction(null)} onDone={() => done(`The access of ${action.user.email} was saved.`)} />
      )}
      {action?.kind === 'reset' && (
        <ResetSheet user={action.user} onClose={() => setAction(null)} onDone={() => done(`The password of ${action.user.email} was reset; they were signed out and must change it when they next sign in.`)} />
      )}
      {(action?.kind === 'disable' || action?.kind === 'enable') && (
        <ToggleSheet
          kind={action.kind}
          user={action.user}
          onClose={() => setAction(null)}
          onDone={() => done(action.kind === 'disable' ? `${action.user.email} was disabled and signed out.` : `${action.user.email} can sign in again.`)}
        />
      )}
    </section>
  )
}

function Badges({ user, you }: { user: User; you: boolean }) {
  return (
    <>
      {you && <span className="chip bg-brand-600 text-white">You</span>}
      {user.active ? (
        <span className="chip bg-green-100 text-green-800 ring-1 ring-green-200">Active</span>
      ) : (
        <span className="chip bg-slate-100 text-slate-700 ring-1 ring-slate-200">Disabled</span>
      )}
      {user.mustChangePassword && <span className="chip bg-amber-50 text-amber-800 ring-1 ring-amber-200">Must change password</span>}
    </>
  )
}

/** GroupChips names the groups a user is in, or says they have no access. */
function GroupChips({ user, groups }: { user: User; groups: Group[] }) {
  const names = user.groupIds.map((id) => groups.find((g) => g.id === id)?.name ?? id)
  if (names.length === 0) return <span className="chip bg-amber-50 text-amber-800 ring-1 ring-amber-200">No access</span>
  return (
    <span className="flex flex-wrap gap-1">
      {names.map((n) => (
        <span key={n} className="chip bg-cream-100 text-brand-900 ring-1 ring-cream-300">
          {n}
        </span>
      ))}
    </span>
  )
}

function Actions({ user, you, lastActive, onAction, compact }: { user: User; you: boolean; lastActive: boolean; onAction: (a: Action) => void; compact?: boolean }) {
  // compact: icon buttons with tooltips for the desktop table; phones get labelled buttons.
  const btn = (label: string, aria: string, icon: ReactNode, onClick: () => void, opts: { danger?: boolean; disabled?: boolean; title?: string } = {}) =>
    compact ? (
      <button
        className={`btn-secondary h-9 w-9 shrink-0 px-0 ${opts.danger ? 'text-red-700' : ''}`}
        title={opts.title ?? label.replace('…', '')}
        aria-label={aria}
        onClick={onClick}
        disabled={opts.disabled}
      >
        {icon}
      </button>
    ) : (
      <button className={opts.danger ? 'btn-danger' : 'btn-secondary'} title={opts.title} aria-label={aria} onClick={onClick} disabled={opts.disabled}>
        {label}
      </button>
    )
  const access = btn('Access…', `Change the groups of ${user.email}`, <IconUsers width={16} height={16} />, () => onAction({ kind: 'access', user }))
  return (
    <div className={`flex items-center gap-2 ${compact ? 'flex-nowrap justify-end' : 'flex-wrap'}`}>
      {access}
      {you ? (
        !compact && <span className="text-xs text-slate-500">Change your own password below.</span>
      ) : (
        <>
          {btn('Reset password…', `Reset the password of ${user.email}`, <IconHistory width={16} height={16} />, () => onAction({ kind: 'reset', user }))}
          {user.active
            ? btn('Disable…', `Disable ${user.email}`, <IconBan width={16} height={16} />, () => onAction({ kind: 'disable', user }), {
                danger: true,
                disabled: lastActive,
                title: lastActive ? 'The last active user cannot be disabled' : undefined,
              })
            : btn('Enable…', `Enable ${user.email}`, <IconPlay width={16} height={16} />, () => onAction({ kind: 'enable', user }))}
        </>
      )}
    </div>
  )
}

function AddUserSheet({ groups, onClose, onDone }: { groups: Group[]; onClose: () => void; onDone: (u: User) => void }) {
  const [email, setEmail] = useState('')
  const [groupIds, setGroupIds] = useState<string[]>([])
  const [password, setPassword] = useState('')
  const [show, setShow] = useState(false)
  const [current, setCurrent] = useState('')
  const s = useSubmit(async () => {
    if ([...password].length < MIN_PASSWORD) throw new Error(`The first password needs at least ${MIN_PASSWORD} characters.`)
    onDone(await unwrap(api.POST('/users', { body: { email: email.trim(), password, currentPassword: current, groupIds } })))
  })
  return (
    <Sheet title="Add a user" open onClose={onClose}>
      <UserForm submit={s.submit} err={s.err} busy={s.busy} label="Add user" onClose={onClose}>
        <p className="text-sm text-slate-600">They sign in with their e-mail address and must choose their own password the first time. What they may do comes from their groups.</p>
        <Field label="E-mail address" hint="This is their login.">
          {(f) => <input id={f} type="email" inputMode="email" autoComplete="off" autoCapitalize="none" className="input" maxLength={254} value={email} onChange={(e) => setEmail(e.target.value)} required />}
        </Field>
        <Field label="First password" hint={`At least ${MIN_PASSWORD} characters. Give it to them safely; they must change it.`}>
          {(f) => <input id={f} type={show ? 'text' : 'password'} autoComplete="new-password" className="input" minLength={MIN_PASSWORD} maxLength={200} value={password} onChange={(e) => setPassword(e.target.value)} required />}
        </Field>
        <label className="flex min-h-[44px] items-center gap-2 text-sm text-slate-600">
          <input type="checkbox" checked={show} onChange={(e) => setShow(e.target.checked)} /> Show the first password
        </label>
        <GroupChoice groups={groups} value={groupIds} onChange={setGroupIds} />
        <YourPassword value={current} onChange={setCurrent} />
      </UserForm>
    </Sheet>
  )
}

function ResetSheet({ user, onClose, onDone }: { user: User; onClose: () => void; onDone: () => void }) {
  const [password, setPassword] = useState('')
  const [show, setShow] = useState(false)
  const [current, setCurrent] = useState('')
  const s = useSubmit(async () => {
    if ([...password].length < MIN_PASSWORD) throw new Error(`The temporary password needs at least ${MIN_PASSWORD} characters.`)
    await unwrap(api.POST('/users/{id}/reset-password', { params: { path: { id: user.id } }, body: { password, currentPassword: current } }))
    onDone()
  })
  return (
    <Sheet title="Reset a password" open onClose={onClose}>
      <UserForm submit={s.submit} err={s.err} busy={s.busy} label="Reset password" onClose={onClose}>
        <p className="text-sm text-slate-600">
          <strong className="break-all">{user.email}</strong> is signed out everywhere and must choose their own password the next time they sign in.
        </p>
        <Field label="Temporary password" hint={`At least ${MIN_PASSWORD} characters. Give it to them safely.`}>
          {(f) => <input id={f} type={show ? 'text' : 'password'} autoComplete="new-password" className="input" minLength={MIN_PASSWORD} maxLength={200} value={password} onChange={(e) => setPassword(e.target.value)} required />}
        </Field>
        <label className="flex min-h-[44px] items-center gap-2 text-sm text-slate-600">
          <input type="checkbox" checked={show} onChange={(e) => setShow(e.target.checked)} /> Show the temporary password
        </label>
        <YourPassword value={current} onChange={setCurrent} />
      </UserForm>
    </Sheet>
  )
}

function ToggleSheet({ kind, user, onClose, onDone }: { kind: 'disable' | 'enable'; user: User; onClose: () => void; onDone: () => void }) {
  const [current, setCurrent] = useState('')
  const s = useSubmit(async () => {
    const path = kind === 'disable' ? '/users/{id}/disable' : '/users/{id}/enable'
    await unwrap(api.POST(path, { params: { path: { id: user.id } }, body: { currentPassword: current } }))
    onDone()
  })
  const disabling = kind === 'disable'
  return (
    <Sheet title={disabling ? `Disable ${user.email}?` : `Enable ${user.email}?`} open onClose={onClose}>
      <UserForm submit={s.submit} err={s.err} busy={s.busy} label={disabling ? 'Disable' : 'Enable'} danger={disabling} onClose={onClose}>
        <p className="text-sm text-slate-700">
          {disabling
            ? 'They are signed out at once and can no longer sign in. Nothing they did is removed, and you can enable them again later.'
            : 'They can sign in again with their existing password.'}
        </p>
        <YourPassword value={current} onChange={setCurrent} />
      </UserForm>
    </Sheet>
  )
}
