import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../../App'
import { json, mockFetch } from '../../test/fetch'

const token = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNO_-'
const me = json({ administrator: true, permissions: [], id: '1', email: 'admin', mustChangePassword: false })

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

test('Your account: my details, and a password change with eye buttons, a confirmation and the policy', async () => {
  const saved: unknown[] = []
  const changed: unknown[] = []
  mockFetch({
    'GET /api/v1/auth/me': me,
    'GET /api/v1/version': json({ version: '1.25.0', build: '47' }),
    'GET /api/v1/auth/profile': json({ userName: 'admin', firstName: '', lastName: '', emailAddress: '', resetAddress: '' }),
    'PUT /api/v1/auth/profile': async (req) => {
      const b = await req.json()
      saved.push(b)
      return json({ userName: 'admin', ...b, resetAddress: b.emailAddress })()
    },
    'POST /api/v1/auth/password': async (req) => { changed.push(await req.json()); return new Response(null, { status: 204 }) },
  })
  window.history.pushState({}, '', '/settings/account')
  render(<App />)
  const details = await screen.findByRole('form', { name: 'My details' })
  expect(within(details).getByLabelText('User name')).toHaveValue('admin')
  expect(within(details).getByLabelText('User name')).toHaveAttribute('readonly')
  expect(within(details).getByText('Add one so a forgotten password can be reset by e-mail.')).toBeInTheDocument()
  await userEvent.type(within(details).getByLabelText('First name'), 'Albie')
  await userEvent.type(within(details).getByLabelText('Last name'), 'van der Merwe')
  await userEvent.type(within(details).getByLabelText('E-mail address'), 'albie@example.com')
  await userEvent.click(within(details).getByRole('button', { name: 'Save my details' }))
  expect(await within(details).findByText('Saved.')).toBeInTheDocument()
  expect(saved).toEqual([{ firstName: 'Albie', lastName: 'van der Merwe', emailAddress: 'albie@example.com' }])
  expect(within(details).getByText('Password reset links go to albie@example.com.')).toBeInTheDocument()

  const pw = screen.getByRole('form', { name: 'Change password' })
  expect(within(pw).getAllByRole('button', { name: 'Show password' })).toHaveLength(3)
  const submit = within(pw).getByRole('button', { name: 'Change password' })
  expect(within(pw).getByText('Password policy')).toBeInTheDocument()
  await userEvent.type(within(pw).getByLabelText('Current password'), 'old password 1')
  await userEvent.type(within(pw).getByLabelText('New password'), 'a long new password')
  await userEvent.type(within(pw).getByLabelText('Confirm new password'), 'a long new passwort')
  expect(submit).toBeDisabled() // not the same
  const rules = within(pw).getByRole('list', { name: 'Password rules' })
  expect(within(rules).getByText(/12 to 200 characters \(now 19\)/).closest('li')).toHaveTextContent('(met)')
  expect(within(rules).getByText('New password and confirmation are the same').closest('li')).toHaveTextContent('(not met yet)')
  await userEvent.clear(within(pw).getByLabelText('Confirm new password'))
  await userEvent.type(within(pw).getByLabelText('Confirm new password'), 'a long new password')
  // The eye shows what was typed.
  const newBox = within(pw).getByLabelText('New password')
  expect(newBox).toHaveAttribute('type', 'password')
  await userEvent.click(within(pw).getAllByRole('button', { name: 'Show password' })[1]!)
  expect(newBox).toHaveAttribute('type', 'text')
  await userEvent.click(submit)
  expect(await within(pw).findByText(/Password changed/)).toBeInTheDocument()
  expect(changed).toEqual([{ currentPassword: 'old password 1', newPassword: 'a long new password' }])
})

test('Forgot password on the sign-in page e-mails a reset link and says the same for any user name', async () => {
  const asked: unknown[] = []
  mockFetch({
    'GET /api/v1/auth/me': json({ type: 'u', title: 'Sign in required', status: 401 }, 401),
    'GET /api/v1/version': json({ version: '1.25.0', build: '47' }),
    'POST /api/v1/auth/password-reset': async (req) => { asked.push(await req.json()); return new Response(null, { status: 202 }) },
  })
  render(<App />)
  await userEvent.type(await screen.findByLabelText('User name'), 'admin')
  await userEvent.click(screen.getByRole('button', { name: 'Forgot password?' }))
  expect(screen.getByRole('heading', { name: 'Reset your password' })).toBeInTheDocument()
  expect(screen.getByLabelText('User name')).toHaveValue('admin')
  await userEvent.click(screen.getByRole('button', { name: 'E-mail me a reset link' }))
  expect(await screen.findByText(/If “admin” has a password and an e-mail address, a link to choose a new password is on its way/)).toBeInTheDocument()
  expect(asked).toEqual([{ userName: 'admin', linkBase: window.location.origin }])
  await userEvent.click(screen.getByRole('button', { name: 'Back to sign in' }))
  expect(screen.getByRole('heading', { name: 'Sign in' })).toBeInTheDocument()
})

test('the reset link sets a new password once; a used link says so', async () => {
  const sent: unknown[] = []
  mockFetch({
    [`GET /api/v1/auth/password-reset/${token}`]: () => new Response(null, { status: 204 }),
    [`POST /api/v1/auth/password-reset/${token}`]: async (req) => { sent.push(await req.json()); return new Response(null, { status: 204 }) },
  })
  window.history.pushState({}, '', `/reset-password/${token}`)
  const { unmount } = render(<App />)
  const save = await screen.findByRole('button', { name: 'Save new password' })
  expect(screen.queryByLabelText('Current password')).not.toBeInTheDocument()
  await userEvent.type(screen.getByLabelText('New password'), 'short')
  expect(save).toBeDisabled()
  await userEvent.clear(screen.getByLabelText('New password'))
  await userEvent.type(screen.getByLabelText('New password'), 'a brand new long password')
  await userEvent.type(screen.getByLabelText('Confirm new password'), 'a brand new long password')
  await userEvent.click(save)
  expect(await screen.findByText(/Your password was changed/)).toBeInTheDocument()
  expect(sent).toEqual([{ newPassword: 'a brand new long password' }])
  unmount()

  mockFetch({ [`GET /api/v1/auth/password-reset/${token}`]: json({ type: 'x', title: 'Invalid', status: 422, field: 'token' }, 422) })
  render(<App />)
  expect(await screen.findByText(/This link does not work any more/)).toBeInTheDocument()
})
