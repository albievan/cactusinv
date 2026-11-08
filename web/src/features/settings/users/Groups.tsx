import { useState } from 'react'
import { api, unwrap } from '../../../lib/api/client'
import { Empty, Field, Sheet } from '../../../components/ui'
import { IconEdit, IconPlus, IconTrash } from '../../../components/icons'
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
    <section className="vstack gap-3" aria-labelledby="groups-heading">
      <div className="d-flex flex-wrap align-items-center justify-content-between gap-2">
        <h2 id="groups-heading" className="fs-5 fw-bold mb-0">
          Groups
        </h2>
        <button className="btn btn-primary" onClick={() => setAction({ kind: 'new' })}>
          <IconPlus width={16} />
          New group
        </button>
      </div>
      <p className="small text-body-secondary mb-0">
        A group gives its members access. A user may do what any of their groups allows; a user in no group can sign in but sees nothing. Administrators can do
        everything, including users, groups, backups and restore.
      </p>
      {groups.length === 0 ? (
        <Empty>No groups.</Empty>
      ) : (
        <>
          <ul className="list-unstyled vstack gap-2 mb-0 d-md-none" aria-label="Groups">
            {groups.map((g) => (
              <li key={g.id} className="card card-body vstack gap-2">
                <div className="d-flex flex-wrap align-items-center gap-2">
                  <span className="fw-semibold">{g.name}</span>
                  {g.builtin && <span className="badge rounded-pill bg-primary">Built in</span>}
                </div>
                {g.description && !g.builtin && <p className="small text-body-secondary mb-0">{g.description}</p>}
                <p className="small mb-0">{accessSummary(g, catalogue)}</p>
                <p className="small text-body-secondary text-break mb-0">Members: {members(g)}</p>
                <GroupButtons large group={g} onAction={setAction} />
              </li>
            ))}
          </ul>
          <div className="card overflow-hidden d-none d-md-block">
            <div className="table-responsive">
              <table className="table table-hover mb-0 small">
                <caption className="visually-hidden">Groups</caption>
                <thead>
                  <tr>
                    <th>Group</th>
                    <th>Access</th>
                    <th>Members</th>
                    <th>
                      <span className="visually-hidden">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {groups.map((g) => (
                    <tr key={g.id} className="align-top">
                      <td>
                        <div className="d-flex flex-wrap align-items-center gap-2 fw-medium">
                          {g.name}
                          {g.builtin && <span className="badge rounded-pill bg-primary">Built in</span>}
                        </div>
                        {g.description && !g.builtin && <div className="mt-1 fs-8 text-body-secondary">{g.description}</div>}
                      </td>
                      <td>{accessSummary(g, catalogue)}</td>
                      <td className="text-break">{members(g)}</td>
                      <td>
                        <GroupButtons group={g} onAction={setAction} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
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
function GroupButtons({ group, onAction, large }: { group: Group; onAction: (a: GroupAction) => void; large?: boolean }) {
  const size = large ? 'btn-lg' : 'btn-sm'
  return (
    <div className="d-flex gap-2 justify-content-md-end">
      <button className={`btn ${size} btn-outline-primary btn-icon`} title={group.builtin ? 'Change members' : 'Edit'} aria-label={`Edit ${group.name}`} onClick={() => onAction({ kind: 'edit', group })}>
        <IconEdit width={16} />
      </button>
      {!group.builtin && (
        <button className={`btn ${size} btn-outline-danger btn-icon`} title="Delete" aria-label={`Delete ${group.name}`} onClick={() => onAction({ kind: 'delete', group })}>
          <IconTrash width={16} />
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
    <fieldset>
      <legend className="form-label fs-6">Access</legend>
      <div className="list-group">
        {areas(catalogue).map(({ area, items }) => (
          <fieldset key={area} className="list-group-item px-3 py-2">
            <legend className="visually-hidden">{area}</legend>
            <div className="row g-1">
              <span aria-hidden="true" className="col-12 col-sm-3 pt-sm-2 small fw-semibold text-primary-emphasis">
                {area}
              </span>
              <div className="col-12 col-sm-9 d-flex flex-column">
                {items.map((p) => {
                  const granted = value.includes(p.key)
                  const included = !granted && implied.has(p.key)
                  return (
                    <label key={p.key} className="d-flex align-items-center gap-2 small" style={{ minHeight: 40 }}>
                      <input type="checkbox" className="form-check-input mt-0 flex-shrink-0" checked={granted || included} disabled={included} onChange={(e) => toggle(p.key, e.target.checked)} />
                      <span>
                        {p.label}
                        {included && <span className="ms-1 fs-8 text-body-secondary">(included)</span>}
                      </span>
                    </label>
                  )
                })}
              </div>
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
          <p className="small text-body-secondary mb-0">Administrators can do everything, including users, groups, backups and restore. Only its members can change, and you cannot remove yourself.</p>
        ) : (
          <>
            <Field label="Name">{(f) => <input id={f} className="form-control" maxLength={80} value={name} onChange={(e) => setName(e.target.value)} required />}</Field>
            <Field label="Description" hint="Optional: who the group is for.">
              {(f) => <input id={f} className="form-control" maxLength={500} value={description} onChange={(e) => setDescription(e.target.value)} />}
            </Field>
            <PermissionPicker catalogue={catalogue} value={permissions} onChange={setPermissions} />
          </>
        )}
        <fieldset>
          <legend className="form-label fs-6">Members</legend>
          <div className="overflow-y-auto border rounded-3 bg-white px-3 py-1" style={{ maxHeight: '14rem' }}>
            {users.map((u) => {
              const self = builtin && u.id === meId
              return (
                <label key={u.id} className="d-flex align-items-center gap-2 small" style={{ minHeight: 40 }}>
                  <input
                    type="checkbox"
                    className="form-check-input mt-0 flex-shrink-0"
                    checked={memberIds.includes(u.id)}
                    disabled={self}
                    onChange={(e) => setMemberIds(e.target.checked ? [...memberIds, u.id] : memberIds.filter((x) => x !== u.id))}
                  />
                  <span className="text-break min-w-0">{u.email}</span>
                  {!u.active && <span className="fs-8 text-body-secondary">(disabled)</span>}
                  {self && <span className="fs-8 text-body-secondary">(you cannot remove yourself)</span>}
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
        <p className="small text-body mb-0">
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
      <legend className="form-label fs-6">Groups</legend>
      <div className="border rounded-3 bg-white px-3 py-1">
        {groups.map((g) => (
          <label key={g.id} className="d-flex align-items-start gap-2 py-2 small" style={{ minHeight: 44 }}>
            <input
              type="checkbox"
              className="form-check-input mt-1 flex-shrink-0"
              checked={value.includes(g.id)}
              disabled={g.id === locked}
              onChange={(e) => onChange(e.target.checked ? [...value, g.id] : value.filter((x) => x !== g.id))}
            />
            <span>
              <span className="fw-medium">{g.name}</span>
              {g.id === locked && <span className="ms-1 fs-8 text-body-secondary">(you cannot remove yourself)</span>}
              {g.description && <span className="d-block fs-8 text-body-secondary">{g.description}</span>}
            </span>
          </label>
        ))}
      </div>
      {value.length === 0 && (
        <p role="note" className="alert alert-warning small py-2 mt-2 mb-0">
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
        <p className="small text-body-secondary mb-0">They may do what any of these groups allows. A change applies at once.</p>
        <GroupChoice groups={groups} value={groupIds} onChange={setGroupIds} locked={locked} />
        <YourPassword value={current} onChange={setCurrent} />
      </UserForm>
    </Sheet>
  )
}
