import { useState } from 'react'
import { api, unwrap } from '../../../lib/api/client'
import { Empty, Field, Sheet } from '../../../components/ui'
import { IconEdit, IconTrash } from '../../../components/icons'
import { ADMINISTRATORS, accessSummary, areas, useSubmit, UserForm, withImplied, YourPassword, type Group, type PermissionInfo, type User } from './shared'

type GroupAction = { kind: 'new' } | { kind: 'edit' | 'delete'; group: Group }

/**
 * GroupsAdmin is the Groups tab of Settings → Users & groups (ADR-012): every group, what it
 * allows and who is in it; create, change and delete groups.
 */
export function GroupsAdmin({ groups, catalogue, users, meId, onChanged }: { groups: Group[]; catalogue: PermissionInfo[]; users: User[]; meId?: string; onChanged: (msg: string) => void }) {
  const [action, setAction] = useState<GroupAction | null>(null)
  const name = (id: string) => users.find((u) => u.id === id)?.email ?? id
  const members = (g: Group) => (g.memberIds.length ? g.memberIds.map(name).join(', ') : 'Nobody')
  const done = (msg: string) => {
    setAction(null)
    onChanged(msg)
  }
  return (
    <section className="space-y-4" aria-labelledby="groups-heading">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id="groups-heading" className="text-xl font-bold text-brand-900">
          Groups
        </h2>
        <button className="btn-primary" onClick={() => setAction({ kind: 'new' })}>
          New group
        </button>
      </div>
      <p className="text-sm text-slate-600">
        A group gives its members access. A user may do what any of their groups allows; a user in no group can sign in but sees nothing. Administrators can do
        everything, including users, groups, backups and restore.
      </p>
      {groups.length === 0 ? (
        <Empty>No groups.</Empty>
      ) : (
        <>
          <ul className="space-y-2 md:hidden" aria-label="Groups">
            {groups.map((g) => (
              <li key={g.id} className="card space-y-2">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-semibold">{g.name}</span>
                  {g.builtin && <span className="chip bg-brand-600 text-white">Built in</span>}
                </div>
                {g.description && !g.builtin && <p className="text-sm text-slate-600">{g.description}</p>}
                <p className="text-sm">{accessSummary(g, catalogue)}</p>
                <p className="text-sm text-slate-500">Members: {members(g)}</p>
                <GroupButtons group={g} onAction={setAction} />
              </li>
            ))}
          </ul>
          <div className="card hidden overflow-x-auto p-0 md:block">
            <table className="w-full text-sm">
              <caption className="sr-only">Groups</caption>
              <thead className="bg-cream-100 text-left text-xs uppercase tracking-wide text-slate-600">
                <tr>
                  <th className="px-3 py-2">Group</th>
                  <th className="px-3 py-2">Access</th>
                  <th className="px-3 py-2">Members</th>
                  <th className="px-3 py-2">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {groups.map((g) => (
                  <tr key={g.id} className="border-t border-cream-200 align-top">
                    <td className="px-3 py-2">
                      <div className="flex flex-wrap items-center gap-2 font-medium">
                        {g.name}
                        {g.builtin && <span className="chip bg-brand-600 text-white">Built in</span>}
                      </div>
                      {g.description && !g.builtin && <div className="mt-0.5 text-xs text-slate-500">{g.description}</div>}
                    </td>
                    <td className="px-3 py-2">{accessSummary(g, catalogue)}</td>
                    <td className="break-words px-3 py-2">{members(g)}</td>
                    <td className="px-3 py-2">
                      <GroupButtons group={g} onAction={setAction} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      {(action?.kind === 'new' || action?.kind === 'edit') && (
        <GroupSheet
          group={action.kind === 'edit' ? action.group : undefined}
          catalogue={catalogue}
          users={users}
          meId={meId}
          onClose={() => setAction(null)}
          onDone={(g) => done(action.kind === 'new' ? `The group ${g.name} was created.` : `The group ${g.name} was saved.`)}
        />
      )}
      {action?.kind === 'delete' && <DeleteGroupSheet group={action.group} onClose={() => setAction(null)} onDone={() => done(`The group ${action.group.name} was deleted.`)} />}
    </section>
  )
}

/** GroupButtons are the compressed edit and delete buttons of a group. */
function GroupButtons({ group, onAction }: { group: Group; onAction: (a: GroupAction) => void }) {
  return (
    <div className="flex gap-2 md:justify-end">
      <button className="btn-secondary h-11 w-11 px-0 md:h-9 md:w-9" title={group.builtin ? 'Change members' : 'Edit'} aria-label={`Edit ${group.name}`} onClick={() => onAction({ kind: 'edit', group })}>
        <IconEdit width={16} height={16} />
      </button>
      {!group.builtin && (
        <button className="btn-secondary h-11 w-11 px-0 text-red-700 md:h-9 md:w-9" title="Delete" aria-label={`Delete ${group.name}`} onClick={() => onAction({ kind: 'delete', group })}>
          <IconTrash width={16} height={16} />
        </button>
      )}
    </div>
  )
}

/** PermissionPicker is the access a group gives, by area; implied permissions show as included. */
function PermissionPicker({ catalogue, value, onChange }: { catalogue: PermissionInfo[]; value: string[]; onChange: (v: string[]) => void }) {
  const implied = withImplied(value, catalogue)
  const toggle = (k: string, on: boolean) => onChange(on ? [...value, k] : value.filter((x) => x !== k))
  return (
    <fieldset className="space-y-2">
      <legend className="label">Access</legend>
      <div className="divide-y divide-cream-200 rounded-xl ring-1 ring-cream-300">
        {areas(catalogue).map(({ area, items }) => (
          <fieldset key={area} className="grid gap-1 px-3 py-2 sm:grid-cols-[9rem_1fr]">
            <legend className="sr-only">{area}</legend>
            <span aria-hidden="true" className="pt-2 text-sm font-semibold text-brand-900">
              {area}
            </span>
            <div className="flex flex-col">
              {items.map((p) => {
                const granted = value.includes(p.key)
                const included = !granted && implied.has(p.key)
                return (
                  <label key={p.key} className="flex min-h-[40px] items-center gap-2 text-sm">
                    <input type="checkbox" checked={granted || included} disabled={included} onChange={(e) => toggle(p.key, e.target.checked)} />
                    <span>
                      {p.label}
                      {included && <span className="ml-1 text-xs text-slate-500">(included)</span>}
                    </span>
                  </label>
                )
              })}
            </div>
          </fieldset>
        ))}
      </div>
    </fieldset>
  )
}

/** GroupSheet creates or edits a group; for Administrators only its members. */
function GroupSheet({ group, catalogue, users, meId, onClose, onDone }: { group?: Group; catalogue: PermissionInfo[]; users: User[]; meId?: string; onClose: () => void; onDone: (g: Group) => void }) {
  const [name, setName] = useState(group?.name ?? '')
  const [description, setDescription] = useState(group?.description ?? '')
  const [permissions, setPermissions] = useState<string[]>(group?.permissions ?? [])
  const [memberIds, setMemberIds] = useState<string[]>(group?.memberIds ?? [])
  const [current, setCurrent] = useState('')
  const builtin = !!group?.builtin
  const s = useSubmit(async () => {
    const body = { name: name.trim(), description: description.trim(), permissions, memberIds, currentPassword: current }
    const saved = group
      ? await unwrap(api.PUT('/groups/{id}', { params: { path: { id: group.id } }, body }))
      : await unwrap(api.POST('/groups', { body }))
    onDone(saved)
  }, 'group')
  return (
    <Sheet title={group ? (builtin ? 'Administrators: members' : `Edit ${group.name}`) : 'New group'} open onClose={onClose}>
      <UserForm submit={s.submit} err={s.err} busy={s.busy} label={group ? 'Save' : 'Create group'} onClose={onClose}>
        {builtin ? (
          <p className="text-sm text-slate-600">Administrators can do everything, including users, groups, backups and restore. Only its members can change, and you cannot remove yourself.</p>
        ) : (
          <>
            <Field label="Name">{(f) => <input id={f} className="input" maxLength={80} value={name} onChange={(e) => setName(e.target.value)} required />}</Field>
            <Field label="Description" hint="Optional: who the group is for.">
              {(f) => <input id={f} className="input" maxLength={500} value={description} onChange={(e) => setDescription(e.target.value)} />}
            </Field>
            <PermissionPicker catalogue={catalogue} value={permissions} onChange={setPermissions} />
          </>
        )}
        <fieldset>
          <legend className="label">Members</legend>
          <div className="max-h-56 overflow-y-auto rounded-xl px-3 py-1 ring-1 ring-cream-300">
            {users.map((u) => {
              const self = builtin && u.id === meId
              return (
                <label key={u.id} className="flex min-h-[40px] items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={memberIds.includes(u.id)}
                    disabled={self}
                    onChange={(e) => setMemberIds(e.target.checked ? [...memberIds, u.id] : memberIds.filter((x) => x !== u.id))}
                  />
                  <span className="break-all">{u.email}</span>
                  {!u.active && <span className="text-xs text-slate-500">(disabled)</span>}
                  {self && <span className="text-xs text-slate-500">(you cannot remove yourself)</span>}
                </label>
              )
            })}
          </div>
        </fieldset>
        <YourPassword value={current} onChange={setCurrent} />
      </UserForm>
    </Sheet>
  )
}

function DeleteGroupSheet({ group, onClose, onDone }: { group: Group; onClose: () => void; onDone: () => void }) {
  const [current, setCurrent] = useState('')
  const s = useSubmit(async () => {
    await unwrap(api.POST('/groups/{id}/delete', { params: { path: { id: group.id } }, body: { currentPassword: current } }))
    onDone()
  }, 'group')
  return (
    <Sheet title={`Delete ${group.name}?`} open onClose={onClose}>
      <UserForm submit={s.submit} err={s.err} busy={s.busy} label="Delete group" danger onClose={onClose}>
        <p className="text-sm text-slate-700">
          {group.memberIds.length
            ? `Its ${group.memberIds.length === 1 ? 'member loses' : `${group.memberIds.length} members lose`} the access it gave, at once. Their accounts and what they did stay.`
            : 'Nobody is in it.'}
        </p>
        <YourPassword value={current} onChange={setCurrent} />
      </UserForm>
    </Sheet>
  )
}

/** GroupChoice is a list of groups to tick, warning when none is ticked. */
export function GroupChoice({ groups, value, onChange, locked }: { groups: Group[]; value: string[]; onChange: (v: string[]) => void; locked?: string }) {
  return (
    <fieldset>
      <legend className="label">Groups</legend>
      <div className="rounded-xl px-3 py-1 ring-1 ring-cream-300">
        {groups.map((g) => (
          <label key={g.id} className="flex min-h-[44px] items-start gap-2 py-2 text-sm">
            <input
              type="checkbox"
              className="mt-0.5"
              checked={value.includes(g.id)}
              disabled={g.id === locked}
              onChange={(e) => onChange(e.target.checked ? [...value, g.id] : value.filter((x) => x !== g.id))}
            />
            <span>
              <span className="font-medium">{g.name}</span>
              {g.id === locked && <span className="ml-1 text-xs text-slate-500">(you cannot remove yourself)</span>}
              {g.description && <span className="block text-xs text-slate-500">{g.description}</span>}
            </span>
          </label>
        ))}
      </div>
      {value.length === 0 && (
        <p role="note" className="mt-2 rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-900 ring-1 ring-amber-200">
          In no group they can sign in but see nothing, until you put them in one.
        </p>
      )}
    </fieldset>
  )
}

/** UserGroupsSheet changes the groups a user is in. */
export function UserGroupsSheet({ user, groups, meId, onClose, onDone }: { user: User; groups: Group[]; meId?: string; onClose: () => void; onDone: () => void }) {
  const [groupIds, setGroupIds] = useState<string[]>(user.groupIds)
  const [current, setCurrent] = useState('')
  const s = useSubmit(async () => {
    await unwrap(api.PUT('/users/{id}/groups', { params: { path: { id: user.id } }, body: { groupIds, currentPassword: current } }))
    onDone()
  })
  const locked = user.id === meId && user.groupIds.includes(ADMINISTRATORS) ? ADMINISTRATORS : undefined
  return (
    <Sheet title={`Access of ${user.email}`} open onClose={onClose}>
      <UserForm submit={s.submit} err={s.err} busy={s.busy} label="Save access" onClose={onClose}>
        <p className="text-sm text-slate-600">They may do what any of these groups allows. A change applies at once.</p>
        <GroupChoice groups={groups} value={groupIds} onChange={setGroupIds} locked={locked} />
        <YourPassword value={current} onChange={setCurrent} />
      </UserForm>
    </Sheet>
  )
}
