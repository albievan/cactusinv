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
