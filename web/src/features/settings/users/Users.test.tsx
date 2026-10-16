import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../../../App'
import { json, mockFetch } from '../../../test/fetch'

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

const me = { id: 'u1', email: 'albie@example.com', automationPaused: false, mustChangePassword: false, administrator: true, permissions: [] }
const albie = { id: 'u1', email: 'albie@example.com', createdAt: '2026-10-01T08:00:00Z', active: true, mustChangePassword: false, lastLoginAt: '2026-10-15T08:00:00Z', groupIds: ['administrators'] }
const bob = { id: 'u2', email: 'bob@example.com', createdAt: '2026-10-02T08:00:00Z', active: true, mustChangePassword: true, groupIds: [] as string[] }
const carol = { id: 'u3', email: 'carol@example.com', createdAt: '2026-10-03T08:00:00Z', active: false, disabledAt: '2026-10-10T08:00:00Z', mustChangePassword: false, groupIds: [] as string[] }

const catalogue = [
  { key: 'dashboard.view', area: 'Dashboard', label: 'See the dashboard', implies: [] },
  { key: 'invoices.view', area: 'Invoices', label: 'See invoices and their PDFs', implies: [] },
  { key: 'invoices.send', area: 'Invoices', label: 'E-mail invoices', implies: ['invoices.view'] },
  { key: 'audit.view', area: 'Audit log', label: 'See and verify the audit log', implies: [] },
]
const admins = { id: 'administrators', name: 'Administrators', description: 'Everything', builtin: true, permissions: [], memberIds: ['u1'], createdAt: '2026-10-01T08:00:00Z', updatedAt: '2026-10-01T08:00:00Z' }
const accounts = { id: 'g1', name: 'Accounts', description: 'Bills customers', builtin: false, permissions: ['invoices.send'], memberIds: [] as string[], createdAt: '2026-10-01T08:00:00Z', updatedAt: '2026-10-01T08:00:00Z' }
const groupList = { items: [admins, accounts], permissions: catalogue }

function routes(users: unknown[], extra: Record<string, (req: Request) => Response | Promise<Response>> = {}) {
  mockFetch({
    'GET /api/v1/auth/me': json(me),
    'GET /api/v1/version': json({ version: '1.6.0', build: '20' }),
    'GET /api/v1/users': json({ items: users }),
    'GET /api/v1/groups': json(groupList),
    ...extra,
  })
}

async function openUsers() {
  window.history.pushState({}, '', '/settings/users')
  render(<App />)
  await screen.findByRole('heading', { name: 'Users' })
  return screen.findByRole('table', { name: 'Users' })
}

test('Users is a Settings sub-page with a table and phone cards, and marks you', async () => {
  routes([albie, bob, carol])
  await openUsers()
  const table = screen.getByRole('table', { name: 'Users' })
  const rows = within(table).getAllByRole('row')
  expect(rows).toHaveLength(4)
  expect(within(rows[1]!).getByText('You')).toBeInTheDocument()
  expect(within(rows[1]!).queryByRole('button', { name: /Disable/ })).not.toBeInTheDocument() // never yourself
  expect(within(rows[2]!).getByText('Must change password')).toBeInTheDocument()
  expect(within(rows[3]!).getByText('Disabled')).toBeInTheDocument()
  expect(within(rows[3]!).getByRole('button', { name: 'Enable carol@example.com' })).toBeInTheDocument()
  const cards = screen.getByRole('list', { name: 'Users' })
  expect(within(cards).getAllByRole('listitem')).toHaveLength(3)
  expect(within(cards).getByText('bob@example.com')).toBeInTheDocument()
  // The side nav lists Users under Settings.
  expect(screen.getAllByRole('link', { name: 'Users & groups' }).length).toBeGreaterThan(0)
  expect(within(rows[1]!).getByText('Administrators')).toBeInTheDocument()
  expect(within(rows[2]!).getByText('No access')).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'Change password' })).toBeInTheDocument()
})

test('the last active user cannot be disabled', async () => {
  routes([albie, { ...bob, active: true }, carol].map((u) => (u.id === 'u1' ? { ...u, active: false } : u)))
  await openUsers()
  const table = screen.getByRole('table', { name: 'Users' })
  expect(within(table).getByRole('button', { name: 'Disable bob@example.com' })).toBeDisabled()
})

test('adds a user with your password, and reports a wrong one', async () => {
  const bodies: unknown[] = []
  let calls = 0
  routes([albie], {
    'POST /api/v1/users': async (req) => {
      const body = await req.json()
      bodies.push(body)
      calls++
      if (calls === 1) return json({ type: 'https://cactusinv/problems/validation', title: 'Invalid input', status: 422, detail: 'currentPassword: is not correct', field: 'currentPassword' }, 422)()
      expect(req.headers.get('Idempotency-Key')).toBeNull() // passwords are never fingerprinted
      return json({ ...bob, email: 'dave@example.com', id: 'u4' }, 201)()
    },
  })
  await openUsers()
  await userEvent.click(screen.getByRole('button', { name: 'Add user' }))
  const sheet = screen.getByRole('dialog', { name: 'Add a user' })
  await userEvent.type(within(sheet).getByLabelText('E-mail address'), ' dave@example.com ')
  await userEvent.type(within(sheet).getByLabelText('First password'), 'a first password')
  // Without a group the form warns that they will see nothing.
  expect(within(sheet).getByRole('note')).toHaveTextContent('see nothing')
  await userEvent.click(within(sheet).getByRole('checkbox', { name: /Accounts/ }))
  expect(within(sheet).queryByRole('note')).not.toBeInTheDocument()
  await userEvent.type(within(sheet).getByLabelText('Your password'), 'wrong')
  await userEvent.click(within(sheet).getByRole('button', { name: 'Add user' }))
  expect(await within(sheet).findByRole('alert')).toHaveTextContent('Your password is not correct.')
  await userEvent.clear(within(sheet).getByLabelText('Your password'))
  await userEvent.type(within(sheet).getByLabelText('Your password'), 'correct horse battery')
  await userEvent.click(within(sheet).getByRole('button', { name: 'Add user' }))
  expect(await screen.findByText(/dave@example.com was added/)).toBeInTheDocument()
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(bodies[1]).toEqual({ email: 'dave@example.com', password: 'a first password', currentPassword: 'correct horse battery', groupIds: ['g1'] })
})

test('resets a password and disables a user after confirmation', async () => {
  const posted: string[] = []
  routes([albie, bob], {
    'POST /api/v1/users/u2/reset-password': async (req) => {
      posted.push(`reset ${JSON.stringify(await req.json())}`)
      return json(bob)()
    },
    'POST /api/v1/users/u2/disable': async (req) => {
      posted.push(`disable ${JSON.stringify(await req.json())}`)
      return json({ ...bob, active: false })()
    },
  })
  await openUsers()
  const table = screen.getByRole('table', { name: 'Users' })
  await userEvent.click(within(table).getByRole('button', { name: 'Reset the password of bob@example.com' }))
  let sheet = screen.getByRole('dialog', { name: 'Reset a password' })
  await userEvent.type(within(sheet).getByLabelText('Temporary password'), 'short')
  await userEvent.type(within(sheet).getByLabelText('Your password'), 'correct horse battery')
  await userEvent.click(within(sheet).getByRole('button', { name: 'Reset password' }))
  expect(within(sheet).getByRole('alert')).toHaveTextContent('at least 12 characters')
  expect(posted).toHaveLength(0)
  await userEvent.type(within(sheet).getByLabelText('Temporary password'), ' and longer')
  await userEvent.click(within(sheet).getByRole('button', { name: 'Reset password' }))
  expect(await screen.findByText(/password of bob@example.com was reset/)).toBeInTheDocument()

  await userEvent.click(within(table).getByRole('button', { name: 'Disable bob@example.com' }))
  sheet = screen.getByRole('dialog', { name: 'Disable bob@example.com?' })
  expect(within(sheet).getByText(/signed out at once/)).toBeInTheDocument()
  await userEvent.type(within(sheet).getByLabelText('Your password'), 'correct horse battery')
  await userEvent.click(within(sheet).getByRole('button', { name: 'Disable' }))
  expect(await screen.findByText('bob@example.com was disabled and signed out.')).toBeInTheDocument()
  expect(posted).toEqual([
    'reset {"password":"short and longer","currentPassword":"correct horse battery"}',
    'disable {"currentPassword":"correct horse battery"}',
  ])
})

test('a refused disable shows the server reason', async () => {
  routes([albie, bob], {
    'POST /api/v1/users/u2/disable': json({ type: 'https://cactusinv/problems/last-active-user', title: 'The last active user cannot be disabled', status: 409, detail: 'Add or enable another user first, so someone can still sign in.' }, 409),
  })
  await openUsers()
  await userEvent.click(within(screen.getByRole('table', { name: 'Users' })).getByRole('button', { name: 'Disable bob@example.com' }))
  const sheet = screen.getByRole('dialog', { name: 'Disable bob@example.com?' })
  await userEvent.type(within(sheet).getByLabelText('Your password'), 'correct horse battery')
  await userEvent.click(within(sheet).getByRole('button', { name: 'Disable' }))
  expect(await within(sheet).findByRole('alert')).toHaveTextContent('The last active user cannot be disabled. Add or enable another user first')
})

test('a user who must change the password sees only the change screen, then the app', async () => {
  let must = true
  let changed: unknown
  mockFetch({
    'GET /api/v1/auth/me': () => json({ administrator: true, permissions: [], id: 'adm', email: 'admin', automationPaused: false, mustChangePassword: must })(),
    'GET /api/v1/version': json({ version: '1.6.0', build: '20' }),
    'POST /api/v1/auth/password': async (req) => {
      changed = await req.json()
      must = false
      return new Response(null, { status: 204 })
    },
    'GET /api/v1/users': json({ items: [] }),
  })
  window.history.pushState({}, '', '/settings/users')
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'Choose a new password' })).toBeInTheDocument()
  expect(screen.queryByRole('navigation', { name: 'Main' })).not.toBeInTheDocument()
  expect(screen.getByText('admin')).toBeInTheDocument()
  await userEvent.type(screen.getByLabelText('Current password'), 'admin')
  await userEvent.type(screen.getByLabelText('New password'), 'a long new password')
  await userEvent.type(screen.getByLabelText('New password again'), 'a long new passwort')
  await userEvent.click(screen.getByRole('button', { name: 'Save and continue' }))
  expect(screen.getByRole('alert')).toHaveTextContent('not the same')
  await userEvent.clear(screen.getByLabelText('New password again'))
  await userEvent.type(screen.getByLabelText('New password again'), 'a long new password')
  await userEvent.click(screen.getByRole('button', { name: 'Save and continue' }))
  expect(await screen.findByRole('heading', { name: 'Users' })).toBeInTheDocument()
  expect(changed).toEqual({ currentPassword: 'admin', newPassword: 'a long new password' })
})

test('the change screen reports a wrong current password and can sign out', async () => {
  let out = false
  mockFetch({
    'GET /api/v1/auth/me': () => (out ? json({ type: 'u', title: 'Sign in required', status: 401 }, 401)() : json({ administrator: true, permissions: [], id: 'adm', email: 'admin', automationPaused: false, mustChangePassword: true })()),
    'GET /api/v1/version': json({ version: '1.6.0', build: '20' }),
    'POST /api/v1/auth/password': json({ type: 'https://cactusinv/problems/validation', title: 'Invalid input', status: 422, detail: 'currentPassword: is not correct', field: 'currentPassword' }, 422),
    'POST /api/v1/auth/logout': () => {
      out = true
      return new Response(null, { status: 204 })
    },
  })
  render(<App />)
  await screen.findByRole('heading', { name: 'Choose a new password' })
  await userEvent.type(screen.getByLabelText('Current password'), 'nimda')
  await userEvent.type(screen.getByLabelText('New password'), 'a long new password')
  await userEvent.type(screen.getByLabelText('New password again'), 'a long new password')
  await userEvent.click(screen.getByRole('button', { name: 'Save and continue' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('The current password is not correct.')
  await userEvent.click(screen.getByRole('button', { name: 'Sign out' }))
  expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument()
})

test('the Groups tab lists groups with their access and members', async () => {
  routes([albie, bob])
  window.history.pushState({}, '', '/settings/users?tab=groups')
  render(<App />)
  const table = await screen.findByRole('table', { name: 'Groups' })
  const rows = within(table).getAllByRole('row')
  expect(rows).toHaveLength(3)
  expect(within(rows[1]!).getByText('Built in')).toBeInTheDocument()
  expect(within(rows[1]!).getByText(/Everything, including users/)).toBeInTheDocument()
  expect(within(rows[1]!).getByText('albie@example.com')).toBeInTheDocument()
  expect(within(rows[1]!).queryByRole('button', { name: 'Delete Administrators' })).not.toBeInTheDocument()
  expect(within(rows[2]!).getByText('Invoices: view, e-mail')).toBeInTheDocument() // e-mailing includes viewing
  expect(within(rows[2]!).getByText('Nobody')).toBeInTheDocument()
})

test('creates a group with access and members; implied access shows as included', async () => {
  let body: unknown
  routes([albie, bob], {
    'POST /api/v1/groups': async (req) => {
      body = await req.json()
      return json({ ...accounts, id: 'g2', name: 'Clerks' }, 201)()
    },
  })
  window.history.pushState({}, '', '/settings/users?tab=groups')
  render(<App />)
  await userEvent.click(await screen.findByRole('button', { name: 'New group' }))
  const sheet = screen.getByRole('dialog', { name: 'New group' })
  await userEvent.type(within(sheet).getByLabelText('Name'), 'Clerks')
  await userEvent.click(within(sheet).getByRole('checkbox', { name: 'E-mail invoices' }))
  const view = within(sheet).getByRole('checkbox', { name: /See invoices and their PDFs/ })
  expect(view).toBeChecked()
  expect(view).toBeDisabled() // included with e-mailing
  await userEvent.click(within(sheet).getByRole('checkbox', { name: /See and verify the audit log/ }))
  await userEvent.click(within(sheet).getByRole('checkbox', { name: /See and verify the audit log/ })) // and off again
  await userEvent.click(within(sheet).getByRole('checkbox', { name: /bob@example.com/ }))
  await userEvent.type(within(sheet).getByLabelText('Your password'), 'correct horse battery')
  await userEvent.click(within(sheet).getByRole('button', { name: 'Create group' }))
  expect(await screen.findByText('The group Clerks was created.')).toBeInTheDocument()
  expect(body).toEqual({ name: 'Clerks', description: '', permissions: ['invoices.send'], memberIds: ['u2'], currentPassword: 'correct horse battery' })
})

test('Administrators: only members change, and you cannot remove yourself', async () => {
  let body: unknown
  routes([albie, bob], {
    'PUT /api/v1/groups/administrators': async (req) => {
      body = await req.json()
      return json({ ...admins, memberIds: ['u1', 'u2'] })()
    },
  })
  window.history.pushState({}, '', '/settings/users?tab=groups')
  render(<App />)
  await userEvent.click(await within(await screen.findByRole('table', { name: 'Groups' })).findByRole('button', { name: 'Edit Administrators' }))
  const sheet = screen.getByRole('dialog', { name: 'Administrators: members' })
  expect(within(sheet).queryByLabelText('Name')).not.toBeInTheDocument()
  expect(within(sheet).getByRole('checkbox', { name: /albie@example.com/ })).toBeDisabled()
  await userEvent.click(within(sheet).getByRole('checkbox', { name: /bob@example.com/ }))
  await userEvent.type(within(sheet).getByLabelText('Your password'), 'correct horse battery')
  await userEvent.click(within(sheet).getByRole('button', { name: 'Save' }))
  expect(await screen.findByText('The group Administrators was saved.')).toBeInTheDocument()
  expect(body).toMatchObject({ memberIds: ['u1', 'u2'], currentPassword: 'correct horse battery' })
})

test('deletes a group after your password', async () => {
  let deleted = false
  routes([albie], {
    'POST /api/v1/groups/g1/delete': () => {
      deleted = true
      return new Response(null, { status: 204 })
    },
  })
  window.history.pushState({}, '', '/settings/users?tab=groups')
  render(<App />)
  await userEvent.click(await within(await screen.findByRole('table', { name: 'Groups' })).findByRole('button', { name: 'Delete Accounts' }))
  const sheet = screen.getByRole('dialog', { name: 'Delete Accounts?' })
  await userEvent.type(within(sheet).getByLabelText('Your password'), 'correct horse battery')
  await userEvent.click(within(sheet).getByRole('button', { name: 'Delete group' }))
  expect(await screen.findByText('The group Accounts was deleted.')).toBeInTheDocument()
  expect(deleted).toBe(true)
})

test("changes a user's groups", async () => {
  let body: unknown
  routes([albie, bob], {
    'PUT /api/v1/users/u2/groups': async (req) => {
      body = await req.json()
      return json({ ...bob, groupIds: ['g1'] })()
    },
  })
  await openUsers()
  await userEvent.click(within(screen.getByRole('table', { name: 'Users' })).getByRole('button', { name: 'Change the groups of bob@example.com' }))
  const sheet = screen.getByRole('dialog', { name: 'Access of bob@example.com' })
  await userEvent.click(within(sheet).getByRole('checkbox', { name: /Accounts/ }))
  await userEvent.type(within(sheet).getByLabelText('Your password'), 'correct horse battery')
  await userEvent.click(within(sheet).getByRole('button', { name: 'Save access' }))
  expect(await screen.findByText('The access of bob@example.com was saved.')).toBeInTheDocument()
  expect(body).toEqual({ groupIds: ['g1'], currentPassword: 'correct horse battery' })
})
