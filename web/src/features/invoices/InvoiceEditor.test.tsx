import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../../App'
import { json, mockFetch } from '../../test/fetch'

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

const party = { name: 'Acme (as invoiced)', attention: 'Jane', address: 'P O Box 1' }
const draft = {
  id: 'i1', status: 'draft', stage: 'draft', customerId: 'c1', currency: 'ZAR', issueDate: '2026-10-01',
  paymentTerms: { kind: 'days_after', value: 14, text: '14 days' }, header: {}, billTo: party, shipTo: party, notes: 'n', vatPercent: '15',
  lines: [{ productId: 'p1', description: 'Support', quantity: '1', unitPrice: '100.00', discountPercent: '0', amount: '100.00' }],
  totals: { subtotal: '100.00', discount: '0.00', vat: '15.00', total: '115.00' }, bank: {}, overdue: false,
  createdAt: '2026-10-01T08:00:00Z', updatedAt: '2026-10-01T08:00:00Z', version: 3,
}

test('saving an edited draft keeps its prices, parties and terms', async () => {
  let body: Record<string, unknown> = {}
  mockFetch({
    'GET /api/v1/auth/me': json({ administrator: true, permissions: [], id: '1', email: 'a@b.example', automationPaused: false }),
    'GET /api/v1/version': json({ version: '1.12.0', build: '1' }),
    'GET /api/v1/customers': json({ items: [{ id: 'c1', code: 'CD', name: 'Acme', currency: 'ZAR', paymentTerms: { kind: 'on_receipt', value: 0, text: 'On receipt' } }], total: 1 }),
    'GET /api/v1/settings/business': json({ name: 'B', vatPercent: '15', timeZone: 'UTC' }),
    'GET /api/v1/currencies': json({ items: [{ code: 'ZAR', name: 'Rand', symbol: 'R', minorUnits: 2, active: true }] }),
    'GET /api/v1/invoices/i1': json({ invoice: draft, events: [], emails: [] }),
    'PUT /api/v1/invoices/i1': async (req) => {
      body = (await req.json()) as Record<string, unknown>
      return json(draft)()
    },
  })
  window.history.pushState({}, '', '/invoices/i1/edit')
  render(<App />)
  await userEvent.click(await screen.findByRole('button', { name: /Save/ }))
  await vi.waitFor(() => expect(body.version).toBe(3))
  expect(body).toMatchObject({ billTo: party, shipTo: party, paymentTerms: { kind: 'days_after', value: 14 } })
  expect((body.lines as { unitPrice?: string }[])[0]!.unitPrice).toBe('100.00')
})
