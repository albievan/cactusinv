import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { App } from './App'
import { AuthContext } from './components/auth'
import { InvoiceActions } from './components/InvoiceActions'
import { TimeZoneSelect, deviceZone } from './components/TimeZoneSelect'
import { json, mockFetch } from './test/fetch'

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

const customer = {
  id: 'c1', code: 'CD', name: 'Acme', email: 'ap@acme.example', cc: [], attention: '', billingAddress: '', shippingName: '',
  shippingAddress: '', phone: '', taxNumber: '', currency: 'ZAR', paymentTerms: { kind: 'days_after', value: 30, text: '30 days' },
  bankAccountId: '', defaultNotes: '', disclaimerSource: 'business', active: true, version: 1,
}

function as(permissions: string[], extra: Record<string, (req: Request) => Response | Promise<Response>> = {}) {
  mockFetch({
    'GET /api/v1/auth/me': json({ id: '2', email: 'clerk@example.com', administrator: false, permissions, mustChangePassword: false }),
    'GET /api/v1/version': json({ version: '1.8.0', build: '1' }),
    'GET /api/v1/settings/business': json({ name: 'B', timeZone: 'UTC', defaultNotes: '' }),
    'GET /api/v1/currencies': json({ items: [{ code: 'ZAR', name: 'Rand', symbol: 'R', minorUnits: 2, active: true }] }),
    'GET /api/v1/bank-accounts': json({ items: [] }),
    'GET /api/v1/customers': json({ items: [customer], total: 1 }),
    'GET /api/v1/customers/c1': json(customer),
    ...extra,
  })
}

test('adding customers is not editing or deleting them', async () => {
  as(['customers.view', 'customers.create'])
  window.history.pushState({}, '', '/customers')
  const { unmount } = render(<App />)
  expect(await screen.findByRole('link', { name: /New customer/ })).toBeInTheDocument()
  unmount()

  window.history.pushState({}, '', '/customers/new')
  const second = render(<App />)
  expect(await screen.findByRole('button', { name: 'Save' })).toBeInTheDocument()
  second.unmount()

  // An existing customer opens read-only: no Save, no Delete.
  window.history.pushState({}, '', '/customers/c1')
  render(<App />)
  expect(await screen.findByDisplayValue('Acme')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
})

test('editing customers is not deleting them, and deleting is not editing', async () => {
  as(['customers.view', 'customers.edit'])
  window.history.pushState({}, '', '/customers/c1')
  const { unmount } = render(<App />)
  expect(await screen.findByRole('button', { name: 'Save' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
  expect(screen.queryByRole('link', { name: /New customer/ })).not.toBeInTheDocument()
  unmount()

  as(['customers.view', 'customers.delete'])
  render(<App />)
  expect(await screen.findByRole('button', { name: 'Delete' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
})

const issued = { id: 'i1', number: 'CD/1', status: 'sent', total: '10.00', balance: '10.00' } as never

function actionsFor(permissions: string[], inv = issued) {
  const auth = { user: { id: '2', email: 'c', administrator: false, permissions }, outside: false, logout: async () => {}, refresh: async () => {}, endSession: () => {} }
  return render(
    <AuthContext.Provider value={auth as never}>
      <MemoryRouter><InvoiceActions inv={inv} showLabels onChanged={() => {}} onError={() => {}} /></MemoryRouter>
    </AuthContext.Provider>,
  )
}

test('mark paid, mark overdue and void are separate rights', () => {
  const labels = () => screen.queryAllByRole('button').map((b) => b.getAttribute('aria-label'))
  const { unmount } = actionsFor(['invoices.view', 'invoices.pay'])
  expect(labels()).toEqual(['Mark paid'])
  unmount()
  const two = actionsFor(['invoices.view', 'invoices.void'])
  expect(labels()).toEqual(['Void'])
  two.unmount()
  actionsFor(['invoices.view', 'invoices.send', 'invoices.overdue'], { ...(issued as object), status: 'draft' } as never)
  // A draft is e-mailed by issuing it: without invoices.issue there is no "Issue & send".
  expect(labels()).toEqual(['Mark overdue'])
})

test('time zones are picked from a list, the device zone first', () => {
  render(<TimeZoneSelect id="z" value="UTC" onChange={() => {}} />)
  const options = screen.getAllByRole('option').map((o) => o.getAttribute('value'))
  expect(options[0]).toBe('UTC')
  if (deviceZone() !== 'UTC') expect(options[1]).toBe(deviceZone())
  expect(options).toContain('Africa/Johannesburg')
  expect(new Set(options).size).toBe(options.length)
})

test('settings permissions are per page and per action (M24)', async () => {
  const bank = {
    id: 'b1', currency: 'ZAR', label: 'Main', bankName: 'FNB', branch: '', branchCode: '', accountName: '', accountNumber: '62000000001',
    accountType: '', swiftBic: '', iban: '', country: '', isDefault: true, active: true, version: 1,
  }
  as(['settings.currencies.view', 'settings.banks.view', 'settings.banks.edit'], { 'GET /api/v1/bank-accounts': json({ items: [bank] }) })
  window.history.pushState({}, '', '/settings/currencies')
  const { unmount } = render(<App />)
  const row = await screen.findByRole('button', { name: /Main · FNB · 62000000001/ })
  // Only the pages it allows are in the menu.
  expect(screen.queryByRole('link', { name: /^Business/ })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '+ Currency' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '+ Bank' })).not.toBeInTheDocument()
  row.click()
  expect(await screen.findByRole('button', { name: 'Save' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
  unmount()

  // Seeing currencies is not seeing their bank accounts.
  as(['settings.currencies.view'], { 'GET /api/v1/bank-accounts': json({ items: [bank] }) })
  render(<App />)
  expect(await screen.findByText('ZAR')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /Main · FNB/ })).not.toBeInTheDocument()
})
