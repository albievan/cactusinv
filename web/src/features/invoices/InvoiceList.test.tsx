import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../../App'
import { json, mockFetch } from '../../test/fetch'

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

const party = { name: 'Paradigm', attention: '', address: '' }
const invoice = (over: Record<string, unknown>) => ({
  id: 'i1', number: 'PD/1/26', status: 'issued', stage: 'issued', customerId: 'c1', currency: 'ZAR', issueDate: '2026-11-20', dueDate: '2026-12-07',
  paymentTerms: { kind: 'on_receipt', value: 0, text: 'On receipt' }, header: {}, billTo: party, shipTo: party, notes: 'Hours for October: 18.', vatPercent: '0',
  lines: [], totals: { subtotal: '17325.00', discount: '0.00', vat: '0.00', total: '17325.00' }, bank: {}, overdue: false,
  createdAt: '2026-11-20T06:00:00Z', updatedAt: '2026-11-20T06:00:00Z', version: 3, ...over,
})

test('the Scheduled filter lists scheduled invoices with only Preview, Edit schedule and Notes', async () => {
  const queries: string[] = []
  const saved: unknown[] = []
  const scheduled = invoice({ scheduleId: 's1' })
  mockFetch({
    'GET /api/v1/auth/me': json({ administrator: true, permissions: [], id: '1', email: 'admin', automationPaused: false }),
    'GET /api/v1/version': json({ version: '1.16.0', build: '1' }),
    'GET /api/v1/currencies': json({ items: [{ code: 'ZAR', name: 'Rand', symbol: 'R', minorUnits: 2, active: true }] }),
    'GET /api/v1/invoices': (req) => {
      const q = new URL(req.url).searchParams
      queries.push(q.get('scheduled') ?? '')
      const items = q.get('scheduled') ? [scheduled] : [scheduled, invoice({ id: 'i2', number: 'PD/2/26' })]
      return json({ items, total: items.length })()
    },
    'PUT /api/v1/invoices/i1/notes': async (req) => {
      saved.push(await req.json())
      return json({ ...scheduled, notes: 'Hours for November: 20.', version: 4 })()
    },
  })
  window.history.pushState({}, '', '/invoices')
  render(<App />)
  await userEvent.click(await screen.findByRole('tab', { name: 'Scheduled' }))
  await vi.waitFor(() => expect(queries).toContain('true'))
  const table = screen.getByRole('table')
  await vi.waitFor(() => expect(within(table).queryByText('PD/2/26')).not.toBeInTheDocument())

  const bar = within(table).getByRole('group', { name: 'Actions for PD/1/26' })
  const names = [...bar.querySelectorAll('a, button')].map((b) => b.getAttribute('aria-label'))
  expect(names).toEqual(['Preview invoice', 'Edit schedule', 'Edit notes'])
  expect(within(bar).getByRole('link', { name: 'Preview invoice' })).toHaveAttribute('href', '/api/v1/invoices/i1/pdf')

  await userEvent.click(within(bar).getByRole('button', { name: 'Edit notes' }))
  const notes = await screen.findByLabelText('Invoice notes')
  expect(notes).toHaveValue('Hours for October: 18.')
  await userEvent.clear(notes)
  await userEvent.type(notes, 'Hours for November: 20.')
  await userEvent.click(screen.getByRole('button', { name: 'Save notes' }))
  await vi.waitFor(() => expect(saved).toEqual([{ notes: 'Hours for November: 20.', version: 3 }]))
  expect(await screen.findByText('Notes saved for PD/1/26')).toBeInTheDocument()
})

test('an invoice not raised by a schedule keeps the full action bar', async () => {
  mockFetch({
    'GET /api/v1/auth/me': json({ administrator: true, permissions: [], id: '1', email: 'admin', automationPaused: false }),
    'GET /api/v1/version': json({ version: '1.16.0', build: '1' }),
    'GET /api/v1/currencies': json({ items: [{ code: 'ZAR', name: 'Rand', symbol: 'R', minorUnits: 2, active: true }] }),
    'GET /api/v1/invoices': json({ items: [invoice({ id: 'i2', number: 'PD/2/26' })], total: 1 }),
  })
  window.history.pushState({}, '', '/invoices')
  render(<App />)
  const table = await screen.findByRole('table')
  const bar = await within(table).findByRole('group', { name: 'Actions for PD/2/26' })
  expect(within(bar).getByRole('button', { name: 'Mark paid' })).toBeInTheDocument()
  expect(within(bar).queryByRole('button', { name: 'Edit notes' })).not.toBeInTheDocument()
})
