import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../../../App'
import { json, mockFetch } from '../../../test/fetch'

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

const me = { id: 'u1', email: 'albie@example.com', automationPaused: false, mustChangePassword: false }
const albie = { id: 'u1', email: 'albie@example.com', createdAt: '2026-10-01T08:00:00Z', active: true, mustChangePassword: false, lastLoginAt: '2026-10-15T08:00:00Z' }
const bob = { id: 'u2', email: 'bob@example.com', createdAt: '2026-10-02T08:00:00Z', active: true, mustChangePassword: true }
const carol = { id: 'u3', email: 'carol@example.com', createdAt: '2026-10-03T08:00:00Z', active: false, disabledAt: '2026-10-10T08:00:00Z', mustChangePassword: false }

function routes(users: unknown[], extra: Record<string, (req: Request) => Response | Promise<Response>> = {}) {
  mockFetch({
    'GET /api/v1/auth/me': json(me),
    'GET /api/v1/version': json({ version: '1.6.0', build: '20' }),
    'GET /api/v1/users': json({ items: users }),
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
  expect(screen.getAllByRole('link', { name: 'Users' }).length).toBeGreaterThan(0)
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
  await userEvent.type(within(sheet).getByLabelText('Your password'), 'wrong')
  await userEvent.click(within(sheet).getByRole('button', { name: 'Add user' }))
  expect(await within(sheet).findByRole('alert')).toHaveTextContent('Your password is not correct.')
  await userEvent.clear(within(sheet).getByLabelText('Your password'))
  await userEvent.type(within(sheet).getByLabelText('Your password'), 'correct horse battery')
  await userEvent.click(within(sheet).getByRole('button', { name: 'Add user' }))
  expect(await screen.findByText(/dave@example.com was added/)).toBeInTheDocument()
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(bodies[1]).toEqual({ email: 'dave@example.com', password: 'a first password', currentPassword: 'correct horse battery' })
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
    'GET /api/v1/auth/me': () => json({ id: 'adm', email: 'admin', automationPaused: false, mustChangePassword: must })(),
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
    'GET /api/v1/auth/me': () => (out ? json({ type: 'u', title: 'Sign in required', status: 401 }, 401)() : json({ id: 'adm', email: 'admin', automationPaused: false, mustChangePassword: true })()),
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
