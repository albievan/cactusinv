import { useState, type FormEvent, type ReactNode } from 'react'
import { api, ApiError, unwrap, type Schemas } from '../../../lib/api/client'
import { formatDateTime } from '../../../lib/format'
import { useAsync } from '../../../lib/useAsync'
import { Empty, ErrorBanner, Field, Sheet, Spinner } from '../../../components/ui'
import { useAuth } from '../../../components/auth'
import { ChangePassword, SettingsPage } from '../Settings'

type User = Schemas['User']

const MIN_PASSWORD = 12

/** UsersSettingsPage is /settings/users: who can sign in (ADR-011), and your own password. */
export function UsersSettingsPage() {
  return (
    <SettingsPage>
      <UsersAdmin />
      <ChangePassword />
    </SettingsPage>
  )
}

/** friendly turns a refused request into a sentence for this page. */
export function friendly(e: unknown): Error {
  if (e instanceof ApiError) {
    const p = e.problem
    if (p.field === 'currentPassword') return new Error('Your password is not correct.')
    if (p.field === 'email') return new Error('Enter one valid e-mail address; it is the new user’s login.')
    if (p.field === 'password') return new Error(`The password needs ${MIN_PASSWORD} to 200 characters.`)
    if (p.type.endsWith('/conflict')) return new Error('A user with this login already exists.')
    if (p.detail) return new Error(`${p.title}. ${p.detail}`)
    return new Error(p.title)
  }
  return e as Error
}

type Action = { kind: 'add' } | { kind: 'reset' | 'disable' | 'enable'; user: User }

function UsersAdmin() {
  const { user: me } = useAuth()
  const users = useAsync(() => unwrap(api.GET('/users')), [])
  const [action, setAction] = useState<Action | null>(null)
  const [msg, setMsg] = useState('')
  const done = (text: string) => {
    setAction(null)
    setMsg(text)
    users.reload()
  }
  const open = (a: Action) => {
    setMsg('')
    setAction(a)
  }
  const items = users.data?.items ?? []
  const active = items.filter((u) => u.active).length
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
        Every user can do everything. Users are disabled, never deleted, so the audit log keeps naming them. Nobody can disable themselves or the last
        active user.
      </p>
      <ErrorBanner error={users.error} />
      {msg && (
        <p role="status" className="text-sm font-medium text-green-700">
          {msg}
        </p>
      )}
      {users.loading && !users.data ? (
        <Spinner />
      ) : items.length === 0 ? (
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
                    <td className="break-all px-3 py-2 font-medium">{u.email}</td>
                    <td className="px-3 py-2">
                      <div className="flex flex-wrap gap-1">
                        <Badges user={u} you={u.id === me?.id} />
                      </div>
                    </td>
                    <td className="whitespace-nowrap px-3 py-2">{u.lastLoginAt ? formatDateTime(u.lastLoginAt) : 'never'}</td>
                    <td className="whitespace-nowrap px-3 py-2">{formatDateTime(u.createdAt)}</td>
                    <td className="px-3 py-2">
                      <Actions user={u} you={u.id === me?.id} lastActive={u.active && active <= 1} onAction={open} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      {action?.kind === 'add' && <AddUserSheet onClose={() => setAction(null)} onDone={(u) => done(`${u.email} was added. Give them the password; they must change it when they first sign in.`)} />}
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

function Actions({ user, you, lastActive, onAction }: { user: User; you: boolean; lastActive: boolean; onAction: (a: Action) => void }) {
  if (you) return <p className="text-xs text-slate-500 md:text-right">Change your own password below.</p>
  return (
    <div className="flex flex-wrap gap-2 md:justify-end">
      <button className="btn-secondary" onClick={() => onAction({ kind: 'reset', user })} aria-label={`Reset the password of ${user.email}`}>
        Reset password…
      </button>
      {user.active ? (
        <button
          className="btn-danger"
          onClick={() => onAction({ kind: 'disable', user })}
          disabled={lastActive}
          title={lastActive ? 'The last active user cannot be disabled' : undefined}
          aria-label={`Disable ${user.email}`}
        >
          Disable…
        </button>
      ) : (
        <button className="btn-secondary" onClick={() => onAction({ kind: 'enable', user })} aria-label={`Enable ${user.email}`}>
          Enable…
        </button>
      )}
    </div>
  )
}

/** YourPassword asks for the signed-in user's own password, which every change here needs. */
function YourPassword({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <Field label="Your password" hint="To confirm it is you.">
      {(f) => <input id={f} type="password" autoComplete="current-password" className="input" maxLength={200} value={value} onChange={(e) => onChange(e.target.value)} required />}
    </Field>
  )
}

/** UserForm is the shared frame of the sheets: an error, the fields and the buttons. */
function UserForm({ submit, err, busy, label, danger, onClose, children }: { submit: () => Promise<void>; err: Error | null; busy: boolean; label: string; danger?: boolean; onClose: () => void; children: ReactNode }) {
  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    void submit()
  }
  return (
    <form onSubmit={onSubmit} className="space-y-3">
      <ErrorBanner error={err} />
      {children}
      <div className="flex justify-end gap-2">
        <button type="button" className="btn-secondary" onClick={onClose}>
          Cancel
        </button>
        <button className={danger ? 'btn-danger' : 'btn-primary'} disabled={busy}>
          {busy ? 'Saving…' : label}
        </button>
      </div>
    </form>
  )
}

function useSubmit(run: () => Promise<void>) {
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    setErr(null)
    setBusy(true)
    try {
      await run()
    } catch (e) {
      setErr(friendly(e))
      setBusy(false)
    }
  }
  return { err, setErr, busy, submit }
}

function AddUserSheet({ onClose, onDone }: { onClose: () => void; onDone: (u: User) => void }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [show, setShow] = useState(false)
  const [current, setCurrent] = useState('')
  const s = useSubmit(async () => {
    if ([...password].length < MIN_PASSWORD) throw new Error(`The first password needs at least ${MIN_PASSWORD} characters.`)
    onDone(await unwrap(api.POST('/users', { body: { email: email.trim(), password, currentPassword: current } })))
  })
  return (
    <Sheet title="Add a user" open onClose={onClose}>
      <UserForm submit={s.submit} err={s.err} busy={s.busy} label="Add user" onClose={onClose}>
        <p className="text-sm text-slate-600">The new user can do everything you can. They sign in with their e-mail address and must choose their own password the first time.</p>
        <Field label="E-mail address" hint="This is their login.">
          {(f) => <input id={f} type="email" inputMode="email" autoComplete="off" autoCapitalize="none" className="input" maxLength={254} value={email} onChange={(e) => setEmail(e.target.value)} required />}
        </Field>
        <Field label="First password" hint={`At least ${MIN_PASSWORD} characters. Give it to them safely; they must change it.`}>
          {(f) => <input id={f} type={show ? 'text' : 'password'} autoComplete="new-password" className="input" minLength={MIN_PASSWORD} maxLength={200} value={password} onChange={(e) => setPassword(e.target.value)} required />}
        </Field>
        <label className="flex min-h-[44px] items-center gap-2 text-sm text-slate-600">
          <input type="checkbox" checked={show} onChange={(e) => setShow(e.target.checked)} /> Show the first password
        </label>
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
