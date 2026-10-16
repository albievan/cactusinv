import { useState, type FormEvent, type ReactNode } from 'react'
import { ApiError, type Schemas } from '../../../lib/api/client'
import { ErrorBanner, Field } from '../../../components/ui'

export type User = Schemas['User']
export type Group = Schemas['Group']
export type PermissionInfo = Schemas['PermissionInfo']

export const MIN_PASSWORD = 12
export const ADMINISTRATORS = 'administrators'

/** friendly turns a refused request into a sentence for these pages. */
export function friendly(e: unknown, what: 'user' | 'group' = 'user'): Error {
  if (e instanceof ApiError) {
    const p = e.problem
    if (p.field === 'currentPassword') return new Error('Your password is not correct.')
    if (p.field === 'email') return new Error('Enter one valid e-mail address; it is the new user’s login.')
    if (p.field === 'password') return new Error(`The password needs ${MIN_PASSWORD} to 200 characters.`)
    if (p.field === 'name') return new Error('Give the group a name of at most 80 characters.')
    if (p.type.endsWith('/conflict')) return new Error(what === 'group' ? 'A group with this name already exists.' : 'A user with this login already exists.')
    if (p.detail) return new Error(`${p.title}. ${p.detail}`)
    return new Error(p.title)
  }
  return e as Error
}

/** YourPassword asks for the signed-in user's own password, which every change here needs. */
export function YourPassword({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <Field label="Your password" hint="To confirm it is you.">
      {(f) => <input id={f} type="password" autoComplete="current-password" className="input" maxLength={200} value={value} onChange={(e) => onChange(e.target.value)} required />}
    </Field>
  )
}

/** UserForm is the shared frame of the sheets: an error, the fields and the buttons. */
export function UserForm({ submit, err, busy, label, danger, onClose, children }: { submit: () => Promise<void>; err: Error | null; busy: boolean; label: string; danger?: boolean; onClose: () => void; children: ReactNode }) {
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

export function useSubmit(run: () => Promise<void>, what: 'user' | 'group' = 'user') {
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    setErr(null)
    setBusy(true)
    try {
      await run()
    } catch (e) {
      setErr(friendly(e, what))
      setBusy(false)
    }
  }
  return { err, setErr, busy, submit }
}

/** withImplied adds every permission the given ones imply, transitively (as the server does). */
export function withImplied(granted: string[], catalogue: PermissionInfo[]): Set<string> {
  const out = new Set<string>()
  const add = (k: string) => {
    if (out.has(k)) return
    out.add(k)
    for (const q of catalogue.find((p) => p.key === k)?.implies ?? []) add(q)
  }
  granted.forEach(add)
  return out
}

/** areas groups the catalogue by area, in its order. */
export function areas(catalogue: PermissionInfo[]): { area: string; items: PermissionInfo[] }[] {
  const out: { area: string; items: PermissionInfo[] }[] = []
  for (const p of catalogue) {
    const a = out.find((x) => x.area === p.area)
    if (a) a.items.push(p)
    else out.push({ area: p.area, items: [p] })
  }
  return out
}

const verbs: Record<string, string> = { view: 'view', edit: 'edit', send: 'e-mail', status: 'paid/overdue/void', manage: 'cancel & retry' }

/** accessSummary says briefly what a group allows: "Invoices: view, e-mail · Audit log: view". */
export function accessSummary(g: Group, catalogue: PermissionInfo[]): string {
  if (g.builtin) return 'Everything, including users, groups, backups and restore'
  const has = withImplied(g.permissions, catalogue)
  const parts = areas(catalogue)
    .map(({ area, items }) => {
      const vs = items.filter((p) => has.has(p.key)).map((p) => verbs[p.key.split('.')[1] ?? ''] ?? p.key)
      return vs.length ? `${area}: ${vs.join(', ')}` : ''
    })
    .filter(Boolean)
  return parts.length ? parts.join(' · ') : 'No access'
}
