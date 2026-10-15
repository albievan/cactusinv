import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from './App'

function mockFetch(routes: Record<string, () => Response>) {
  vi.stubGlobal('fetch', vi.fn(async (input: Request | string) => {
    const req = input instanceof Request ? input : new Request(input)
    const path = new URL(req.url, 'http://localhost').pathname
    const handler = routes[`${req.method} ${path}`]
    return handler ? handler() : new Response(JSON.stringify({ type: 'x', title: 'Not found', status: 404 }), { status: 404, headers: { 'Content-Type': 'application/problem+json' } })
  }))
}

const json = (body: unknown, status = 200) => () => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

afterEach(() => vi.unstubAllGlobals())

test('shows the sign-in form when not authenticated, then the dashboard', async () => {
  let signedIn = false
  mockFetch({
    'GET /api/v1/auth/me': () => (signedIn ? json({ id: '1', email: 'admin' })() : json({ type: 'u', title: 'Sign in required', status: 401 }, 401)()),
    'POST /api/v1/auth/login': () => { signedIn = true; return new Response(null, { status: 204 }) },
    'GET /api/v1/version': json({ version: '1.1.0', build: '7' }),
    'GET /api/v1/dashboard': json({ today: '2026-08-21', totals: [{ currency: 'ZAR', openCount: 1, outstanding: '3251.16', overdueCount: 0, overdue: '0.00' }], upcomingEmails: [], upcomingRuns: [], recentInvoices: [], failedEmailCount: 0, months: ['2026-07', '2026-08'], invoiced: [{ currency: 'ZAR', month: '2026-08', total: '3251.16', count: 1 }], paid: [], topDebtors: [{ customerId: 'c', name: 'Acme', currency: 'ZAR', outstanding: '3251.16', openCount: 1 }], activity: [{ seq: 1, at: '2026-08-21T07:00:00Z', actorId: 'u', actor: 'admin', action: 'invoice.issued', entityType: 'invoice', entityId: 'i', summary: 'CD/1195/26', hash: 'h' }] }),
  })
  render(<App />)
  await userEvent.type(await screen.findByLabelText('User name'), 'admin')
  await userEvent.type(screen.getByLabelText('Password'), 'admin')
  await userEvent.click(screen.getByRole('button', { name: 'Sign in' }))
  expect(await screen.findByText('Invoiced and paid, last 12 months')).toBeInTheDocument()
  expect(screen.getByText('Who owes the most')).toBeInTheDocument()
  expect(screen.getByText('Issued')).toBeInTheDocument()
  expect(screen.getAllByRole('navigation', { name: 'Main' }).length).toBeGreaterThan(0)
  expect(await screen.findByTestId('version')).toHaveTextContent('v1.1.0 · build 7')
})

test('sends the CSRF header on mutations', async () => {
  const seen: string[] = []
  vi.stubGlobal('fetch', vi.fn(async (req: Request) => {
    seen.push(`${req.method} ${req.headers.get('X-Requested-With')}`)
    return new Response(null, { status: 401 })
  }))
  render(<App />)
  await userEvent.type(await screen.findByLabelText('User name'), 'a')
  await userEvent.type(screen.getByLabelText('Password'), 'b')
  await userEvent.click(screen.getByRole('button', { name: 'Sign in' }))
  expect(seen).toContain('POST cactusinv')
})

test('the login screen shows the version and build', async () => {
  mockFetch({
    'GET /api/v1/auth/me': json({ type: 'u', title: 'Sign in required', status: 401 }, 401),
    'GET /api/v1/version': json({ version: '1.1.0', build: '7' }),
  })
  render(<App />)
  expect(await screen.findByText('v1.1.0 · build 7')).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'Sign in' })).toBeInTheDocument()
  expect(screen.getByLabelText('Show what I type')).toBeInTheDocument()
})
