import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../../App'
import { json, mockFetch } from '../../test/fetch'
import { todayISO } from '../../lib/format'

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

const party = { name: 'Acme', attention: '', address: '' }
const draft = {
  id: 'i1', status: 'draft', stage: 'draft', customerId: 'c1', currency: 'ZAR', issueDate: '2020-01-01',
  paymentTerms: { kind: 'on_receipt', value: 0, text: 'On receipt' }, header: {}, billTo: party, shipTo: party, notes: '', vatPercent: '0',
  lines: [], totals: { subtotal: '0.00', discount: '0.00', vat: '0.00', total: '0.00' }, bank: {}, overdue: false,
  createdAt: '2020-01-01T08:00:00Z', updatedAt: '2020-01-01T08:00:00Z', version: 1,
}

test('issuing a draft dated in the past asks which date to use', async () => {
  const bodies: unknown[] = []
  mockFetch({
    'GET /api/v1/auth/me': json({ administrator: true, permissions: [], id: '1', email: 'a@b.example', automationPaused: false }),
    'GET /api/v1/version': json({ version: '1.12.0', build: '1' }),
    'GET /api/v1/currencies': json({ items: [{ code: 'ZAR', name: 'Rand', symbol: 'R', minorUnits: 2, active: true }] }),
    'GET /api/v1/invoices/i1': json({ invoice: draft, events: [], emails: [] }),
    'POST /api/v1/invoices/i1/issue': async (req) => {
      bodies.push(await req.json())
      return json({ ...draft, status: 'issued', number: 'CD/1/26' })()
    },
  })
  window.history.pushState({}, '', '/invoices/i1')
  render(<App />)
  await userEvent.click(await screen.findByRole('button', { name: 'Issue' }))
  expect(bodies).toHaveLength(0)
  await userEvent.click(await screen.findByRole('button', { name: 'Issue dated today' }))
  await vi.waitFor(() => expect(bodies).toEqual([{ issueDate: todayISO() }]))
})

test('a part-payment is recorded with its date and reference, and a payment can be removed', async () => {
  const user = userEvent.setup()
  const issued = {
    ...draft, id: 'i2', number: 'CD/2/26', status: 'sent', stage: 'emailed', issueDate: '2026-11-01',
    totals: { subtotal: '1000.00', discount: '0.00', vat: '0.00', total: '1000.00' }, amountPaid: '400.00', balance: '600.00',
  }
  const payment = { id: 'p1', amount: '400.00', receivedOn: '2026-11-10', reference: 'EFT 1', createdAt: '2026-11-10T08:00:00Z' }
  const bodies: unknown[] = []
  const removed: string[] = []
  mockFetch({
    'GET /api/v1/auth/me': json({ administrator: true, permissions: [], id: '1', email: 'a@b.example', automationPaused: false }),
    'GET /api/v1/version': json({ version: '1.35.0', build: '1' }),
    'GET /api/v1/currencies': json({ items: [{ code: 'ZAR', name: 'Rand', symbol: 'R', minorUnits: 2, active: true }] }),
    'GET /api/v1/invoices/i2': json({ invoice: issued, events: [], emails: [], payments: [payment] }),
    'POST /api/v1/invoices/i2/payments': async (req) => {
      bodies.push(await req.json())
      return json({ ...issued, amountPaid: '650.00', balance: '350.00' })()
    },
    'POST /api/v1/invoices/i2/payments/p1/delete': () => {
      removed.push('p1')
      return json({ ...issued, amountPaid: '0.00', balance: '1000.00' })()
    },
  })
  window.history.pushState({}, '', '/invoices/i2')
  render(<App />)
  const section = await screen.findByRole('region', { name: 'Payments' })
  expect(section).toHaveTextContent('EFT 1')
  expect(section).toHaveTextContent(/Still owedZAR 600\.00/)
  expect(screen.getByText(/600\.00 still owed/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Void' })).toBeDisabled()

  await user.click(screen.getByRole('button', { name: /Record a payment/ }))
  const amount = screen.getByLabelText('Amount received (ZAR)')
  expect(amount).toHaveValue('600.00')
  await user.clear(amount)
  await user.type(amount, '700')
  expect(screen.getByRole('button', { name: 'Record payment' })).toBeDisabled()
  await user.clear(amount)
  await user.type(amount, '250')
  await user.type(screen.getByLabelText('Reference (optional)'), ' EFT 2 ')
  await user.click(screen.getByRole('button', { name: 'Record payment' }))
  await vi.waitFor(() => expect(bodies).toEqual([{ amount: '250', receivedOn: todayISO(), reference: 'EFT 2' }]))

  await user.click(screen.getByRole('button', { name: /^Remove the payment of ZAR 400\.00/ }))
  await user.click(await screen.findByRole('button', { name: 'Remove payment' }))
  await vi.waitFor(() => expect(removed).toEqual(['p1']))
})
