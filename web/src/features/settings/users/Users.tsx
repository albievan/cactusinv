import { useState, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, unwrap } from '../../../lib/api/client'
import { formatDateTime } from '../../../lib/format'
import { useAsync } from '../../../lib/useAsync'
import { Empty, ErrorBanner, Field, Sheet, Spinner } from '../../../components/ui'
import { useAuth } from '../../../components/auth'
import { IconBan, IconEdit, IconHistory, IconPlay, IconPlus, IconTrash, IconUsers } from '../../../components/icons'
import { PasswordInput } from '../../../components/password'
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
  const onUsers = tab === 'users'
  const users = useAsync(() => unwrap(api.GET('/users')), [])
  const groups = useAsync(() => unwrap(api.GET('/groups')), [])
  const [msg, setMsg] = useState('')
  const changed = (text: string) => {
    setMsg(text)
    users.reload()
    groups.reload()
  }
  const tabClass = (on: boolean) => `nav-link rounded-pill d-inline-flex align-items-center px-3 small fw-semibold ${on ? 'active' : 'bg-white border'}`
  const show = (t: 'users' | 'groups') => {
    setMsg('')
    setParams(t === 'groups' ? { tab: 'groups' } : {}, { replace: true })
  }
  return (
    <SettingsPage>
      <div role="tablist" aria-label="Users and groups" className="nav nav-pills mb-3 gap-2">
        <button role="tab" aria-selected={tab === 'users'} className={tabClass(onUsers)} style={{ minHeight: 44 }} onClick={() => show('users')}>
          Users
        </button>
        <button role="tab" aria-selected={tab === 'groups'} className={tabClass(!onUsers)} style={{ minHeight: 44 }} onClick={() => show('groups')}>
          Groups
        </button>
      </div>
      <ErrorBanner error={users.error ?? groups.error} />
      {msg && (
        <p role="status" className="mb-3 small fw-medium text-success">
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

type Action = { kind: 'add' } | { kind: 'reset' | 'disable' | 'enable' | 'access' | 'edit' | 'delete'; user: User }

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
    <section className="vstack gap-3" aria-labelledby="users-heading">
      <div className="d-flex flex-wrap align-items-center justify-content-between gap-2">
        <h2 id="users-heading" className="fs-5 fw-bold mb-0">
          Users
        </h2>
        <button className="btn btn-primary" onClick={() => open({ kind: 'add' })}>
          <IconPlus width={16} />
          Add user
        </button>
      </div>
      <p className="small text-body-secondary mb-0">
        What a user may do comes from their groups (see the Groups tab). Disable a user to stop them signing in for now, or delete them for good; the
        audit log keeps their user name on everything they did either way. Nobody can disable or delete themselves or the last active user.
      </p>
      {items.length === 0 ? (
        <Empty>No users.</Empty>
      ) : (
        <>
          <ul className="list-unstyled vstack gap-2 mb-0 d-md-none" aria-label="Users">
            {items.map((u) => (
              <li key={u.id} className="card card-body vstack gap-2">
                <div className="d-flex flex-wrap align-items-center gap-2">
                  <span className="min-w-0 text-break fw-semibold">{u.email}</span>
                  <FullName user={u} />
                  <Badges user={u} you={u.id === me?.id} />
                </div>
                <div className="small">
                  <GroupChips user={u} groups={groups} />
                </div>
                <div className="small text-body-secondary">Last sign-in: {u.lastLoginAt ? formatDateTime(u.lastLoginAt) : 'never'}</div>
                <Actions user={u} you={u.id === me?.id} lastActive={u.active && active <= 1} onAction={open} />
              </li>
            ))}
          </ul>
          <div className="card overflow-hidden d-none d-md-block">
            <div className="table-responsive">
              <table className="table table-hover align-middle mb-0 small">
                <caption className="visually-hidden">Users</caption>
                <thead>
                  <tr>
                    <th>User name</th>
                    <th>Status</th>
                    <th>Groups</th>
                    <th>Last sign-in</th>
                    <th>Added</th>
                    <th>
                      <span className="visually-hidden">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((u) => (
                    <tr key={u.id}>
                      <td className="text-break" style={{ minWidth: '12rem' }}>
                        <div className="fw-medium">{u.email}</div>
                        <FullName user={u} />
                      </td>
                      <td>
                        <div className="d-flex flex-wrap gap-1">
                          <Badges user={u} you={u.id === me?.id} />
                        </div>
                      </td>
                      <td>
                        <GroupChips user={u} groups={groups} />
                      </td>
                      <td className="text-nowrap">{u.lastLoginAt ? formatDateTime(u.lastLoginAt) : 'never'}</td>
                      <td className="text-nowrap">{formatDateTime(u.createdAt)}</td>
                      <td>
                        <Actions compact user={u} you={u.id === me?.id} lastActive={u.active && active <= 1} onAction={open} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
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
      {action?.kind === 'edit' && (
        <EditUserSheet user={action.user} onClose={() => setAction(null)} onDone={(u) => done(`${u.email} was saved.`)} />
      )}
      {action?.kind === 'delete' && (
        <DeleteSheet user={action.user} onClose={() => setAction(null)} onDone={() => done(`${action.user.email} was deleted. The audit log keeps their user name on what they did.`)} />
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

const tone = (c: string) => `badge rounded-pill bg-${c}-subtle text-${c}-emphasis border border-${c}-subtle`
const chip = { active: tone('success'), disabled: tone('secondary'), warning: tone('warning') }

function Badges({ user, you }: { user: User; you: boolean }) {
  return (
    <>
      {you && <span className="badge rounded-pill bg-primary">You</span>}
      {user.active ? <span className={chip.active}>Active</span> : <span className={chip.disabled}>Disabled</span>}
      {user.mustChangePassword && <span className={chip.warning}>Must change password</span>}
    </>
  )
}

/** GroupChips names the groups a user is in, or says they have no access. */
function GroupChips({ user, groups }: { user: User; groups: Group[] }) {
  const names = user.groupIds.map((id) => groups.find((g) => g.id === id)?.name ?? id)
  if (names.length === 0) return <span className={chip.warning}>No access</span>
  return (
    <span className="d-flex flex-wrap gap-1">
      {names.map((n) => (
        <span key={n} className="badge rounded-pill bg-body-tertiary text-primary-emphasis border">
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
        className={`btn btn-sm btn-icon flex-shrink-0 ${opts.danger ? 'btn-outline-danger' : 'btn-outline-primary'}`}
        title={opts.title ?? label.replace('…', '')}
        aria-label={aria}
        onClick={onClick}
        disabled={opts.disabled}
      >
        {icon}
      </button>
    ) : (
      <button className={opts.danger ? 'btn btn-outline-danger' : 'btn btn-outline-primary'} title={opts.title} aria-label={aria} onClick={onClick} disabled={opts.disabled}>
        {icon}
        {label}
      </button>
    )
  const access = btn('Access…', `Change the groups of ${user.email}`, <IconUsers width={16} height={16} />, () => onAction({ kind: 'access', user }))
  const editBtn = btn('Edit…', `Edit ${user.email}`, <IconEdit width={16} height={16} />, () => onAction({ kind: 'edit', user }))
  return (
    <div className={`d-flex align-items-center gap-2 ${compact ? 'flex-nowrap justify-content-end' : 'flex-wrap'}`}>
      {editBtn}
      {access}
      {you ? (
        !compact && <span className="fs-8 text-body-secondary">Change your own password below.</span>
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
          {btn('Delete…', `Delete ${user.email}`, <IconTrash width={16} height={16} />, () => onAction({ kind: 'delete', user }), {
            danger: true,
            disabled: lastActive,
            title: lastActive ? 'The last active user cannot be deleted' : undefined,
          })}
        </>
      )}
    </div>
  )
}

function AddUserSheet({ groups, onClose, onDone }: { groups: Group[]; onClose: () => void; onDone: (u: User) => void }) {
  const [email, setEmail] = useState('')
  const [groupIds, setGroupIds] = useState<string[]>([])
  const [password, setPassword] = useState('')
  const [current, setCurrent] = useState('')
  const s = useSubmit(async () => {
    if ([...password].length < MIN_PASSWORD) throw new Error(`The first password needs at least ${MIN_PASSWORD} characters.`)
    onDone(await unwrap(api.POST('/users', { body: { email: email.trim(), password, currentPassword: current, groupIds } })))
  })
  return (
    <Sheet title="Add a user" open onClose={onClose}>
      <UserForm submit={s.submit} err={s.err} busy={s.busy} label="Add user" onClose={onClose}>
        <p className="small text-body-secondary mb-0">They sign in with their e-mail address and must choose their own password the first time. What they may do comes from their groups.</p>
        <Field label="E-mail address" hint="This is their login.">
          {(f) => <input id={f} type="email" inputMode="email" autoComplete="off" autoCapitalize="none" className="form-control" maxLength={254} value={email} onChange={(e) => setEmail(e.target.value)} required />}
        </Field>
        <Field label="First password" hint={`${MIN_PASSWORD} to 200 characters, any characters. Give it to them safely; they must change it.`}>
          {(f) => <PasswordInput id={f} autoComplete="new-password" value={password} onChange={setPassword} />}
        </Field>
        <GroupChoice groups={groups} value={groupIds} onChange={setGroupIds} />
        <YourPassword value={current} onChange={setCurrent} />
      </UserForm>
    </Sheet>
  )
}

function ResetSheet({ user, onClose, onDone }: { user: User; onClose: () => void; onDone: () => void }) {
  const [password, setPassword] = useState('')
  const [current, setCurrent] = useState('')
  const s = useSubmit(async () => {
    if ([...password].length < MIN_PASSWORD) throw new Error(`The temporary password needs at least ${MIN_PASSWORD} characters.`)
    await unwrap(api.POST('/users/{id}/reset-password', { params: { path: { id: user.id } }, body: { password, currentPassword: current } }))
    onDone()
  })
  return (
    <Sheet title="Reset a password" open onClose={onClose}>
      <UserForm submit={s.submit} err={s.err} busy={s.busy} label="Reset password" onClose={onClose}>
        <p className="small text-body-secondary mb-0">
          <strong className="text-break">{user.email}</strong> is signed out everywhere and must choose their own password the next time they sign in.
        </p>
        <Field label="Temporary password" hint={`${MIN_PASSWORD} to 200 characters, any characters. Give it to them safely.`}>
          {(f) => <PasswordInput id={f} autoComplete="new-password" value={password} onChange={setPassword} />}
        </Field>
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
        <p className="small text-body mb-0">
          {disabling
            ? 'They are signed out at once and can no longer sign in. Nothing they did is removed, and you can enable them again later.'
            : 'They can sign in again with their existing password.'}
        </p>
        <YourPassword value={current} onChange={setCurrent} />
      </UserForm>
    </Sheet>
  )
}

/** FullName is a user's first and last name under their user name, when they have one. */
function FullName({ user }: { user: User }) {
  const name = `${user.firstName ?? ''} ${user.lastName ?? ''}`.trim()
  return name ? <div className="small text-body-secondary">{name}</div> : null
}

/** EditUserSheet changes a user's user name, name and e-mail address (M44). */
function EditUserSheet({ user, onClose, onDone }: { user: User; onClose: () => void; onDone: (u: User) => void }) {
  const [login, setLogin] = useState(user.email)
  const [firstName, setFirstName] = useState(user.firstName ?? '')
  const [lastName, setLastName] = useState(user.lastName ?? '')
  const [emailAddress, setEmailAddress] = useState(user.emailAddress ?? '')
  const [current, setCurrent] = useState('')
  const s = useSubmit(async () => {
    onDone(await unwrap(api.PUT('/users/{id}', { params: { path: { id: user.id } }, body: { userName: login.trim(), firstName, lastName, emailAddress, currentPassword: current } })))
  })
  return (
    <Sheet title={`Edit ${user.email}`} open onClose={onClose}>
      <UserForm submit={s.submit} err={s.err} busy={s.busy} label="Save user" onClose={onClose}>
        <Field label="User name" hint="What they sign in with. A new one must be an e-mail address; the audit log keeps the old name on what was done before.">
          {(f) => <input id={f} autoComplete="off" autoCapitalize="none" className="form-control" maxLength={254} value={login} onChange={(e) => setLogin(e.target.value)} required />}
        </Field>
        <div className="row row-cols-1 row-cols-sm-2 g-3">
          <Field label="First name">{(f) => <input id={f} autoComplete="off" className="form-control" maxLength={100} value={firstName} onChange={(e) => setFirstName(e.target.value)} />}</Field>
          <Field label="Last name">{(f) => <input id={f} autoComplete="off" className="form-control" maxLength={100} value={lastName} onChange={(e) => setLastName(e.target.value)} />}</Field>
        </div>
        <Field label="E-mail address" hint="Password reset links go here (else to the user name when it is an e-mail address).">
          {(f) => <input id={f} type="email" inputMode="email" autoComplete="off" className="form-control" maxLength={254} value={emailAddress} onChange={(e) => setEmailAddress(e.target.value)} />}
        </Field>
        <YourPassword value={current} onChange={setCurrent} />
      </UserForm>
    </Sheet>
  )
}

/** DeleteSheet deletes a user for good (M44) after the actor's password. */
function DeleteSheet({ user, onClose, onDone }: { user: User; onClose: () => void; onDone: () => void }) {
  const [current, setCurrent] = useState('')
  const s = useSubmit(async () => {
    await unwrap(api.POST('/users/{id}/delete', { params: { path: { id: user.id } }, body: { currentPassword: current } }))
    onDone()
  })
  return (
    <Sheet title={`Delete ${user.email}?`} open onClose={onClose}>
      <UserForm submit={s.submit} err={s.err} busy={s.busy} label="Delete user" danger onClose={onClose}>
        <p className="small text-body mb-0">
          <strong className="text-break">{user.email}</strong> is signed out at once and can never sign in again; this cannot be undone. Everything they did stays, and the
          audit log keeps their user name on it. To stop them only for now, disable them instead.
        </p>
        <YourPassword value={current} onChange={setCurrent} />
      </UserForm>
    </Sheet>
  )
}
