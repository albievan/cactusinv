import { render, screen, within } from '@testing-library/react'
import { App } from '../../App'
import { json, mockFetch } from '../../test/fetch'

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

const party = (name: string) => ({ name, address: '', attention: '' })
const totals = (total: string) => ({ subtotal: total, discount: '0.00', vat: '0.00', total })

const dashboard = {
  today: '2026-11-13', soonDays: 7,
  totals: [{ currency: 'ZAR', openCount: 2, outstanding: '5000.00', overdueCount: 0, overdue: '0.00' }],
  upcomingEmails: [], upcomingRuns: [], recentInvoices: [], failedEmailCount: 0, months: ['2026-11'], invoiced: [], paid: [], topDebtors: [], activity: [],
  quoteStats: [{
    currency: 'ZAR', open: 3, openValue: '12500.00', accepted: 1, acceptedValue: '4000.00', declined: 1, converted: 2, expired: 1,
    expiringSoon: 2, issuedThisMonth: 4, thisMonthValue: '9000.00', won: 3, decided: 4,
  }],
  expiringQuotes: [{ id: 'q1', number: 'QU26-7', status: 'sent', stage: 'sent', currency: 'ZAR', issueDate: '2026-10-20', validUntil: '2026-11-14', billTo: party('Prospect Ltd'), totals: totals('2500.00') }],
  upcomingInvoices: [
    { scheduleId: 's1', scheduleName: 'Monthly hosting', scheduleStatus: 'active', customerId: 'c1', customerName: 'Acme Ltd', currency: 'ZAR', period: '2026-12-01', runAt: '2026-12-01T07:00:00Z', issueDate: '2026-12-01', dueDate: '2026-12-31', totals: totals('1500.00'), autoSend: true, problem: '' },
    { scheduleId: 's2', scheduleName: 'Support', scheduleStatus: 'active', customerId: 'c2', customerName: 'Gone Ltd', currency: 'USD', period: '2026-12-01', runAt: '2026-12-01T07:00:00Z', autoSend: false, problem: 'the customer is inactive' },
  ],
  dueSoon: [{ id: 'i1', number: 'CD/1201/26', status: 'sent', stage: 'emailed', customerId: 'c1', currency: 'ZAR', issueDate: '2026-10-15', dueDate: '2026-11-14', billTo: party('Acme Ltd'), totals: totals('3250.00') }],
}

function signedIn(me: Record<string, unknown>) {
  mockFetch({
    'GET /api/v1/auth/me': json({ id: '1', email: 'admin', mustChangePassword: false, ...me }),
    'GET /api/v1/version': json({ version: '1.21.0', build: '43' }),
    'GET /api/v1/dashboard': json(dashboard),
    'GET /api/v1/currencies': json({ items: [{ code: 'ZAR', name: 'Rand', symbol: 'R', minorUnits: 2, active: true }] }),
  })
}

test('the dashboard shows quote figures, upcoming invoices, invoices falling due and quotes running out', async () => {
  signedIn({ administrator: true, permissions: [] })
  render(<App />)
  const quotes = await screen.findByRole('region', { name: 'Quotes' })
  expect(within(quotes).getByText('ZAR 12,500.00')).toBeInTheDocument()
  expect(within(quotes).getByText('3 quotes awaiting an answer')).toBeInTheDocument()
  expect(within(quotes).getByText('ZAR 4,000.00')).toBeInTheDocument()
  expect(within(quotes).getByText('75%')).toBeInTheDocument()
  expect(within(quotes).getByText('3 won of 4 answered')).toBeInTheDocument()
  expect(within(quotes).getByRole('progressbar', { name: 'Win rate' })).toHaveAttribute('aria-valuenow', '75')
  expect(within(quotes).getByText('Expiring in 7 days')).toBeInTheDocument()
  expect(within(quotes).getByText('1 quote already expired')).toBeInTheDocument()
  expect(within(quotes).getByText(/This month: 4 quotes worth ZAR 9,000.00 · 2 turned into invoices · 1 declined/)).toBeInTheDocument()

  const upcoming = screen.getByRole('heading', { name: 'Upcoming invoices' }).closest('section')!
  expect(within(upcoming).getByRole('link', { name: 'Acme Ltd' })).toHaveAttribute('href', '/schedules/s1')
  expect(within(upcoming).getByText('ZAR 1,500.00')).toBeInTheDocument()
  expect(within(upcoming).getByText(/Monthly hosting · .* · e-mailed/)).toBeInTheDocument()
  expect(within(upcoming).getByText('Cannot be raised: the customer is inactive')).toBeInTheDocument()

  const due = screen.getByRole('heading', { name: 'Due in the next 7 days' }).closest('section')!
  expect(within(due).getByRole('link', { name: 'CD/1201/26 · Acme Ltd' })).toHaveAttribute('href', '/invoices/i1')
  expect(within(due).getByText(/Due tomorrow/)).toBeInTheDocument()

  const expiring = screen.getByRole('heading', { name: 'Quotes expiring soon' }).closest('section')!
  expect(within(expiring).getByRole('link', { name: 'QU26-7 · Prospect Ltd' })).toHaveAttribute('href', '/quotes/q1')
  expect(within(expiring).getByText(/Sent · valid until .* \(tomorrow\)/)).toBeInTheDocument()
})

test('a user sees only the dashboard parts their groups allow', async () => {
  signedIn({ administrator: false, permissions: ['dashboard.view', 'quotes.view'] })
  render(<App />)
  expect(await screen.findByRole('region', { name: 'Quotes' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'Quotes expiring soon' })).toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: 'Upcoming invoices' })).not.toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: 'Due in the next 7 days' })).not.toBeInTheDocument()
})
