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

test('a raised scheduled invoice keeps only Preview, Edit schedule and Notes', async () => {
  const saved: unknown[] = []
  const scheduled = invoice({ scheduleId: 's1' })
  mockFetch({
    'GET /api/v1/auth/me': json({ administrator: true, permissions: [], id: '1', email: 'admin', automationPaused: false }),
    'GET /api/v1/version': json({ version: '1.16.0', build: '1' }),
    'GET /api/v1/currencies': json({ items: [{ code: 'ZAR', name: 'Rand', symbol: 'R', minorUnits: 2, active: true }] }),
    'GET /api/v1/invoices': json({ items: [scheduled, invoice({ id: 'i2', number: 'PD/2/26' })], total: 2 }),
    'PUT /api/v1/invoices/i1/notes': async (req) => {
      saved.push(await req.json())
      return json({ ...scheduled, notes: 'Hours for November: 20.', version: 4 })()
    },
  })
  window.history.pushState({}, '', '/invoices')
  render(<App />)
  const table = await screen.findByRole('table')
  const bar = await within(table).findByRole('group', { name: 'Actions for PD/1/26' })
  const names = [...bar.querySelectorAll('a, button')].map((b) => b.getAttribute('aria-label'))
  expect(names).toEqual(['Preview invoice', 'Edit schedule', 'Edit notes'])
  expect(within(bar).getByRole('link', { name: 'Preview invoice' })).toHaveAttribute('href', '/api/v1/invoices/i1/pdf')

  await userEvent.click(within(bar).getByRole('button', { name: 'Edit notes' }))
  // The notes open in the formatted editor, plain notes as text.
  const notes = await screen.findByRole('textbox', { name: 'Invoice notes' })
  expect(notes).toHaveTextContent('Hours for October: 18.')
  expect(screen.getByRole('toolbar', { name: 'Invoice notes formatting' })).toBeInTheDocument()
  await userEvent.clear(notes)
  await userEvent.type(notes, 'Hours for November: 20.')
  await userEvent.click(screen.getByRole('button', { name: 'Save notes' }))
  await vi.waitFor(() => expect(saved).toEqual([{ notes: '<div>Hours for November: 20.</div>', version: 3 }]))
  expect(await screen.findByText('Notes saved for PD/1/26')).toBeInTheDocument()
})

const upcoming = (over: Record<string, unknown>) => ({
  scheduleId: 's1', scheduleName: 'Paradigm monthly', scheduleStatus: 'active', customerId: 'c1', customerName: 'Paradigm', currency: 'ZAR',
  period: '2026-11-21', runAt: '2026-11-21T07:00:00Z', issueDate: '2026-11-21', dueDate: '2026-12-07', autoSend: true,
  totals: { subtotal: '17325.00', discount: '0.00', vat: '0.00', total: '17325.00' }, ...over,
})

test('the Scheduled filter lists the invoices still to be raised, paused schedules too, with a sample preview', async () => {
  const invoiceQueries: string[] = []
  mockFetch({
    'GET /api/v1/auth/me': json({ administrator: true, permissions: [], id: '1', email: 'admin', automationPaused: false }),
    'GET /api/v1/version': json({ version: '1.16.0', build: '1' }),
    'GET /api/v1/currencies': json({ items: [{ code: 'ZAR', name: 'Rand', symbol: 'R', minorUnits: 2, active: true }] }),
    'GET /api/v1/invoices': (req) => {
      invoiceQueries.push(new URL(req.url).search)
      return json({ items: [invoice({ scheduleId: 's1' })], total: 1 })()
    },
    'GET /api/v1/schedules/upcoming': json({ items: [
      upcoming({}),
      upcoming({ scheduleId: 's2', scheduleName: 'Acme hosting', scheduleStatus: 'paused', customerName: 'Acme', runAt: undefined }),
      upcoming({ scheduleId: 's3', scheduleName: 'Old product', totals: undefined, issueDate: undefined, dueDate: undefined, problem: 'product "X" is no longer available' }),
    ] }),
  })
  window.history.pushState({}, '', '/invoices')
  render(<App />)
  await userEvent.click(await screen.findByRole('tab', { name: 'Scheduled' }))
  const table = await screen.findByRole('table')
  await within(table).findByText('Paradigm monthly')
  expect(within(table).queryByText('PD/1/26')).not.toBeInTheDocument()
  const n = invoiceQueries.length

  const bar = within(table).getByRole('group', { name: 'Actions for Paradigm monthly' })
  const names = [...bar.querySelectorAll('a, button')].map((b) => b.getAttribute('aria-label'))
  expect(names).toEqual(['Preview sample invoice', 'Edit schedule'])
  expect(within(bar).getByRole('link', { name: 'Preview sample invoice' })).toHaveAttribute('href', '/api/v1/schedules/s1/sample-pdf')

  const paused = within(table).getByText('Acme hosting').closest('tr') as HTMLElement
  expect(within(paused).getByText('Paused')).toBeInTheDocument()
  expect(within(paused).getByText('When resumed')).toBeInTheDocument()

  const broken = within(table).getByText('Old product').closest('tr') as HTMLElement
  expect(within(broken).getByText('product "X" is no longer available')).toBeInTheDocument()
  expect(within(broken).getByRole('button', { name: 'Preview sample invoice' })).toBeDisabled()

  await userEvent.type(screen.getByPlaceholderText('Search number or customer'), 'acme')
  await vi.waitFor(() => expect(within(table).queryByText('Paradigm monthly')).not.toBeInTheDocument())
  expect(within(table).getByText('Acme hosting')).toBeInTheDocument()
  expect(invoiceQueries.length).toBe(n)
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
