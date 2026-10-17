import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from './App'
import { json, mockFetch } from './test/fetch'

afterEach(() => vi.unstubAllGlobals())

test('shows the sign-in form when not authenticated, then the dashboard', async () => {
  let signedIn = false
  mockFetch({
    'GET /api/v1/auth/me': () => (signedIn ? json({ administrator: true, permissions: [], id: '1', email: 'admin' })() : json({ type: 'u', title: 'Sign in required', status: 401 }, 401)()),
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

test('the statements page lists balances and e-mails selected statements', async () => {
  const aging = (cur: string) => ({ current: cur, days1to30: '0.00', days31to60: '0.00', days61to90: '0.00', over90: '0.00' })
  let sent: unknown
  mockFetch({
    'GET /api/v1/auth/me': json({ administrator: true, permissions: [], id: '1', email: 'admin' }),
    'GET /api/v1/version': json({ version: '1.2.0', build: '8' }),
    'GET /api/v1/statements': json({
      asOf: '2026-10-15',
      items: [
        { customerId: 'a', code: 'AC', name: 'Acme Commercial', email: 'ap@acme.example', currency: 'ZAR', total: '3251.16', openCount: 2, overdueCount: 1, maxDaysOverdue: 12, aging: aging('3251.16') },
        { customerId: 'z', code: 'ZZ', name: 'Zero Owing Ltd', email: 'ap@zero.example', currency: 'ZAR', total: '0.00', openCount: 0, overdueCount: 0, maxDaysOverdue: 0, aging: aging('0.00') },
      ],
    }),
    'POST /api/v1/statements/send': async (req) => {
      sent = await req.json()
      return json({ queued: ['a'], skipped: [] }, 202)()
    },
  })
  window.history.pushState({}, '', '/statements')
  render(<App />)
  expect(await screen.findByRole('link', { name: 'Acme Commercial' })).toBeInTheDocument()
  expect(screen.queryByText('Zero Owing Ltd')).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'All customers' }))
  expect(screen.getByRole('link', { name: 'Zero Owing Ltd' })).toBeInTheDocument()
  await userEvent.click(screen.getByLabelText('Select Acme Commercial'))
  await userEvent.click(screen.getByRole('button', { name: /E-mail 1 statement/ }))
  await userEvent.click(screen.getByRole('button', { name: 'E-mail 1' }))
  expect(await screen.findByText('1 statement queued for e-mail.')).toBeInTheDocument()
  expect(sent).toEqual({ customerIds: ['a'], includeZero: false })
  window.history.pushState({}, '', '/')
})

const business = {
  name: 'Example Software Consulting', addressLines: '', phone: '', email: '', taxNumber: '', numberFormat: '{code}/{dd}{mm}-{seq}/{yy}',
  timeZone: 'Africa/Johannesburg', defaultNotes: 'Business disclaimer: no liability for third-party software.', emailSubject: 's', emailBody: 'b',
  vatPercent: '0', nextSequence: 1195, version: 1,
}

function signedIn(extra: Record<string, (req: Request) => Response | Promise<Response>> = {}) {
  mockFetch({
    'GET /api/v1/auth/me': json({ administrator: true, permissions: [], id: '1', email: 'admin' }),
    'GET /api/v1/version': json({ version: '1.4.0', build: '9' }),
    'GET /api/v1/settings/business': json(business),
    'GET /api/v1/settings/email': json({ enabled: false, host: 'smtp.gmail.com', port: 587, username: '', passwordSet: false, from: '', tls: 'starttls', warnings: [], version: 0 }),
    'GET /api/v1/currencies': json({ items: [{ code: 'ZAR', name: 'South African Rand', symbol: 'R', minorUnits: 2, active: true }] }),
    'GET /api/v1/bank-accounts': json({ items: [] }),
    ...extra,
  })
}

function sidebar() {
  // The desktop sidebar is the Main navigation that holds the Settings group.
  const navs = screen.getAllByRole('navigation', { name: 'Main' })
  return navs.find((n) => within(n).queryByRole('link', { name: 'Settings' }))!
}

test('/settings opens Business, and the sidebar nests the settings pages under Settings', async () => {
  signedIn()
  window.history.pushState({}, '', '/settings')
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'Business' })).toBeInTheDocument()
  expect(window.location.pathname).toBe('/settings/business')
  expect(screen.getByLabelText('Invoice number format')).toHaveValue('{code}/{dd}{mm}-{seq}/{yy}')
  expect(screen.getByText(/\{dd\} day/)).toBeInTheDocument()

  const group = within(sidebar()).getByRole('list', { name: 'Settings' })
  const links = within(group).getAllByRole('link')
  expect(links.map((l) => l.textContent)).toEqual(['Business', 'E-mail server', 'Currencies & bank accounts', 'Import/Export Settings', 'Users & groups', 'Backup & restore', 'Your account'])
  expect(within(group).getByRole('link', { name: 'Business' })).toHaveAttribute('aria-current', 'page')
  expect(within(sidebar()).getByRole('link', { name: 'Settings' })).toHaveAttribute('aria-current', 'true')

  await userEvent.click(within(group).getByRole('link', { name: 'E-mail server' }))
  expect(await screen.findByRole('heading', { name: 'E-mail server' })).toBeInTheDocument()
  expect(window.location.pathname).toBe('/settings/email')
  expect(within(group).getByRole('link', { name: 'E-mail server' })).toHaveAttribute('aria-current', 'page')
  expect(within(group).getByRole('link', { name: 'Business' })).not.toHaveAttribute('aria-current')

  // The phone section links reach the same pages.
  const sections = screen.getByRole('navigation', { name: 'Settings sections' })
  await userEvent.click(within(sections).getByRole('link', { name: 'Currencies & bank accounts' }))
  expect(await screen.findByRole('heading', { name: 'Currencies & bank accounts' })).toBeInTheDocument()
  expect(window.location.pathname).toBe('/settings/currencies')
  const after = screen.getByRole('navigation', { name: 'Settings sections' })
  expect(within(after).getByRole('link', { name: 'Currencies & bank accounts' })).toHaveAttribute('aria-current', 'page')
  expect(within(after).getByRole('link', { name: 'Business' })).not.toHaveAttribute('aria-current')
  window.history.pushState({}, '', '/')
})

test('the settings pages stay folded away off a settings route; More lists them for phones', async () => {
  signedIn()
  window.history.pushState({}, '', '/more')
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'More' })).toBeInTheDocument()
  expect(within(sidebar()).queryByRole('list', { name: 'Settings' })).not.toBeInTheDocument()
  expect(within(sidebar()).getByRole('link', { name: 'Settings' })).not.toHaveAttribute('aria-current')

  const more = screen.getByRole('list', { name: 'Settings' })
  expect(within(more).getByRole('link', { name: /E-mail server/ })).toHaveAttribute('href', '/settings/email')
  expect(within(more).getByRole('link', { name: /Business/ })).toHaveAttribute('href', '/settings/business')
  expect(within(more).getByRole('link', { name: /Currencies & bank accounts/ })).toHaveAttribute('href', '/settings/currencies')
  expect(within(more).getByRole('link', { name: /Backup & restore/ })).toHaveAttribute('href', '/settings/backup')
  window.history.pushState({}, '', '/')
})

test('the customer form shows the business disclaimer it falls back to and which one its PDFs use', async () => {
  const customer = (defaultNotes: string, disclaimerSource: 'customer' | 'business') => ({
    id: 'c1', code: 'CD', name: 'Acme Commercial', email: 'ap@acme.example', cc: [], attention: '', billingAddress: '', shippingName: '',
    shippingAddress: '', phone: '', taxNumber: '', currency: 'ZAR', paymentTerms: { kind: 'days_after', value: 30, text: 'Payment due within 30 days' },
    bankAccountId: '', defaultNotes, disclaimerSource, active: true, version: 1,
  })
  let current = customer('', 'business')
  signedIn({ 'GET /api/v1/customers/c1': () => json(current)() })
  window.history.pushState({}, '', '/customers/c1')
  const { unmount } = render(<App />)
  const box = await screen.findByLabelText('Disclaimer for this customer')
  expect(box).toHaveValue('')
  expect(box).toHaveAttribute('placeholder', business.defaultNotes)
  expect(screen.getByText(/^Empty: the business disclaimer below is used/)).toBeInTheDocument()
  expect(screen.getByTestId('disclaimer-source')).toHaveTextContent('Its PDFs use the business disclaimer.')
  expect(screen.getByText(business.defaultNotes)).toBeInTheDocument()
  unmount()

  current = customer('Acme disclaimer', 'customer')
  render(<App />)
  expect(await screen.findByDisplayValue('Acme disclaimer')).toBeInTheDocument()
  expect(screen.getByTestId('disclaimer-source')).toHaveTextContent("Its PDFs use this customer's own disclaimer.")
  window.history.pushState({}, '', '/')
})

test('a new customer has no saved disclaimer yet, only the fallback', async () => {
  signedIn()
  window.history.pushState({}, '', '/customers/new')
  render(<App />)
  expect(await screen.findByLabelText('Disclaimer for this customer')).toHaveAttribute('placeholder', business.defaultNotes)
  expect(screen.queryByTestId('disclaimer-source')).not.toBeInTheDocument()
  window.history.pushState({}, '', '/')
})

test('the customer form reports a failed business settings load and an empty business disclaimer plainly', async () => {
  signedIn({ 'GET /api/v1/settings/business': json({ type: 'x', title: 'Settings unavailable', status: 500 }, 500) })
  window.history.pushState({}, '', '/customers/new')
  const { unmount } = render(<App />)
  expect(await screen.findByText('Settings unavailable')).toBeInTheDocument()
  expect(screen.queryByText('None set yet.')).not.toBeInTheDocument()
  unmount()

  signedIn({ 'GET /api/v1/settings/business': json({ ...business, defaultNotes: '' }) })
  render(<App />)
  expect(await screen.findByText('None set yet.')).toBeInTheDocument()
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  window.history.pushState({}, '', '/')
})

test('E-mail server shows the server\'s Send-as warnings and adds them to the test result', async () => {
  const warning = 'Gmail sends as albievan@gmail.com unless accounts@primesoft.example is a verified "Send mail as" address of that account (Gmail → Settings → Accounts → Send mail as).'
  signedIn({
    'GET /api/v1/settings/email': json({ enabled: true, host: 'smtp.gmail.com', port: 587, username: 'albievan@gmail.com', passwordSet: true, from: 'accounts@primesoft.example', tls: 'starttls', warnings: [warning], version: 2 }),
    'POST /api/v1/settings/email/test': json({ warnings: [warning] }),
  })
  window.history.pushState({}, '', '/settings/email')
  render(<App />)
  const list = await screen.findByRole('list', { name: 'Send-as warnings' })
  expect(within(list).getByRole('listitem')).toHaveTextContent(warning)
  expect(screen.getByText('For the saved settings:')).toBeInTheDocument()
  expect(screen.getByText(/The provider decides the sender it shows; see the setup guide\./)).toBeInTheDocument()

  await userEvent.click(screen.getByRole('button', { name: 'Send test e-mail' }))
  expect(await screen.findByText(`Test e-mail sent — but: ${warning}`)).toBeInTheDocument()
})

test('E-mail server without warnings shows no note and a plain test result', async () => {
  signedIn({ 'POST /api/v1/settings/email/test': json({ warnings: [] }) })
  window.history.pushState({}, '', '/settings/email')
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'E-mail server' })).toBeInTheDocument()
  expect(screen.queryByRole('list', { name: 'Send-as warnings' })).not.toBeInTheDocument()
  await userEvent.type(screen.getByLabelText('Send a test e-mail to'), 'me@example.com')
  await userEvent.click(screen.getByRole('button', { name: 'Send test e-mail' }))
  expect(await screen.findByText('Test e-mail sent — check the inbox.')).toBeInTheDocument()
})

test('a user sees only what their groups allow', async () => {
  mockFetch({
    'GET /api/v1/auth/me': json({ id: '2', email: 'clerk@example.com', administrator: false, permissions: ['customers.view', 'statements.view'], automationPaused: true, mustChangePassword: false }),
    'GET /api/v1/version': json({ version: '1.7.0', build: '1' }),
    'GET /api/v1/statements': json({ items: [] }),
  })
  window.history.pushState({}, '', '/')
  render(<App />)
  // No dashboard: the start page is the first one allowed.
  expect(await screen.findByRole('heading', { name: 'Customers' })).toBeInTheDocument()
  expect(window.location.pathname).toBe('/customers')
  const links = within(sidebar()).getAllByRole('link').map((l) => l.textContent)
  expect(links).toEqual(['Customers', 'Statements', 'Settings'])
  expect(screen.queryByRole('link', { name: /New customer/ })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Resume automatic sending' })).not.toBeInTheDocument()
  expect(screen.getByText(/An administrator resumes it/)).toBeInTheDocument()

  // A page outside their access says so instead.
  window.history.pushState({}, '', '/invoices')
  window.dispatchEvent(new PopStateEvent('popstate'))
  expect(await screen.findByRole('heading', { name: 'You do not have access to this' })).toBeInTheDocument()

  // Settings has only their own account.
  window.history.pushState({}, '', '/settings')
  window.dispatchEvent(new PopStateEvent('popstate'))
  expect(await screen.findByRole('heading', { name: 'Change password' })).toBeInTheDocument()
  expect(window.location.pathname).toBe('/settings/account')
  window.history.pushState({}, '', '/')
})
