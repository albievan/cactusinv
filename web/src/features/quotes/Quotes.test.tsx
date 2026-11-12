import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../../App'
import { json, mockFetch } from '../../test/fetch'
import { addDays } from './QuoteEditor'

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

const me = json({ administrator: true, permissions: [], id: '1', email: 'admin', automationPaused: false })
const base = {
  'GET /api/v1/auth/me': me,
  'GET /api/v1/version': json({ version: '1.18.0', build: '1' }),
  'GET /api/v1/currencies': json({ items: [{ code: 'ZAR', name: 'Rand', symbol: 'R', minorUnits: 2, active: true }] }),
  'GET /api/v1/settings/business': json({ vatPercent: '15' }),
  'GET /api/v1/settings/quotes': json({ validDays: 14, numberFormat: 'Q-{seq}/{yy}', emailSubject: 's', emailBody: 'b', terms: 'Deposit 50%.', nextSequence: 3, version: 1 }),
}
const party = { name: 'Prospect Ltd', attention: 'Thandi', address: '1 Main Rd' }
const quote = (over: Record<string, unknown> = {}) => ({
  id: 'q1', number: 'Q-2/26', status: 'sent', stage: 'sent', adhocCustomerId: 'a1', currency: 'ZAR', issueDate: '2026-11-12', validUntil: '2026-11-26',
  salesPerson: '', reference: 'Site', billTo: party, notes: '', terms: 'Deposit 50%.', vatPercent: '15', fingerprint: 'ABCD-1234-EF56',
  lines: [{ productId: 'p1', description: 'Website build', quantity: '1', unitPrice: '15000.00', discountPercent: '0', amount: '15000.00' }],
  totals: { subtotal: '15000.00', discount: '0.00', vat: '2250.00', total: '17250.00' }, createdAt: '2026-11-12T08:00:00Z', updatedAt: '2026-11-12T08:00:00Z', version: 2, ...over,
})
const adhoc = { id: 'a1', name: 'Prospect Ltd', email: '', attention: 'Thandi', address: '1 Main Rd', phone: '', taxNumber: '', currency: 'ZAR', version: 1 }

test('addDays counts calendar days across month ends', () => {
  expect(addDays('2026-11-12', 14)).toBe('2026-11-26')
  expect(addDays('2026-12-25', 30)).toBe('2027-01-24')
})

test('the quote list filters by stage, shows expired and marks ad-hoc customers', async () => {
  const queries: string[] = []
  mockFetch({
    ...base,
    'GET /api/v1/quotes': (req) => {
      const q = new URL(req.url).searchParams
      queries.push(`${q.get('status') ?? ''}|${q.get('expired') ?? ''}`)
      return json({ items: [quote({ stage: q.get('expired') ? 'expired' : 'sent' })], total: 1 })()
    },
  })
  window.history.pushState({}, '', '/quotes')
  render(<App />)
  const table = await screen.findByRole('table')
  expect(within(table).getByText('Q-2/26')).toBeInTheDocument()
  expect(within(table).getByText('ad-hoc')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('tab', { name: 'Expired' }))
  await vi.waitFor(() => expect(queries).toContain('|true'))
  expect(await within(screen.getByRole('table')).findByText('Expired')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('tab', { name: 'Invoiced' }))
  await vi.waitFor(() => expect(queries).toContain('converted|'))
})

test('a quote for an ad-hoc customer: e-mail needs an address, and it becomes a customer before an invoice', async () => {
  const sent: unknown[] = []
  const converted: unknown[] = []
  let detail = { quote: quote(), events: [{ action: 'quote.created', actor: 'admin', detail: 'Q-2/26', at: '2026-11-12T08:00:00Z' }], emails: [], adhocCustomer: adhoc }
  mockFetch({
    ...base,
    'GET /api/v1/quotes/q1': () => json(detail)(),
    'POST /api/v1/quotes/q1/send': async (req) => { sent.push(await req.json()); return json({ id: 'm1' }, 202)() },
    'POST /api/v1/adhoc-customers/a1/convert': async (req) => {
      converted.push(await req.json())
      detail = { ...detail, quote: quote({ customerId: 'c9' }) }
      return json({ id: 'c9', code: 'PR', name: 'Prospect Ltd' }, 201)()
    },
  })
  window.history.pushState({}, '', '/quotes/q1')
  render(<App />)
  expect(await screen.findByText('ABCD-1234-EF56')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'PDF' })).toHaveAttribute('href', '/api/v1/quotes/q1/pdf')
  expect(screen.queryByRole('button', { name: 'Convert to invoice' })).not.toBeInTheDocument()
  expect(screen.getByText(/first make Prospect Ltd a customer/)).toBeInTheDocument()

  await userEvent.click(screen.getByRole('button', { name: 'E-mail' }))
  const dialog = screen.getByRole('dialog', { name: 'E-mail quote' })
  expect(within(dialog).getAllByRole('button', { name: 'Send now' }).at(-1)).toBeDisabled()
  await userEvent.type(within(dialog).getByLabelText('To'), 'thandi@prospect.example')
  await userEvent.click(within(dialog).getAllByRole('button', { name: 'Send now' }).at(-1)!)
  await vi.waitFor(() => expect(sent).toEqual([{ to: ['thandi@prospect.example'] }]))

  await userEvent.click(screen.getByRole('button', { name: 'Make customer' }))
  const conv = screen.getByRole('dialog', { name: 'Make Prospect Ltd a customer' })
  await userEvent.type(within(conv).getByLabelText('Invoice code'), 'pr')
  await userEvent.type(within(conv).getByLabelText('E-mail'), 'thandi@prospect.example')
  await userEvent.click(within(conv).getByRole('button', { name: 'Make customer' }))
  await vi.waitFor(() => expect(converted).toEqual([{ code: 'PR', email: 'thandi@prospect.example', paymentTerms: { kind: 'days_after', value: 30, text: '' } }]))
  expect(await screen.findByText(/is now customer PR/)).toBeInTheDocument()
  expect(await screen.findByRole('button', { name: 'Convert to invoice' })).toBeInTheDocument()
})

test('a new quote for an ad-hoc customer added on the spot, valid for the settings days', async () => {
  const created: unknown[] = []
  const posted: unknown[] = []
  mockFetch({
    ...base,
    'GET /api/v1/adhoc-customers': json({ items: [], total: 0 }),
    'GET /api/v1/customers': json({ items: [], total: 0 }),
    'GET /api/v1/products': json({ items: [{ id: 'p1', sku: 'WEB', name: 'Website', description: 'Website build', unit: '', prices: [{ currency: 'ZAR', unitPrice: '15000.00' }], active: true, version: 1 }], total: 1 }),
    'POST /api/v1/adhoc-customers': async (req) => { const b = await req.json(); created.push(b); return json({ ...b, id: 'a2', version: 1 }, 201)() },
    'POST /api/v1/quotes': async (req) => { posted.push(await req.json()); return json(quote({ id: 'q9' }), 201)() },
    'GET /api/v1/quotes/q9': json({ quote: quote({ id: 'q9' }), events: [], emails: [], adhocCustomer: adhoc }),
  })
  window.history.pushState({}, '', '/quotes/new')
  render(<App />)
  await userEvent.click(await screen.findByRole('radio', { name: 'Ad-hoc customer' }))
  await userEvent.click(screen.getByRole('button', { name: 'New' }))
  const sheet = screen.getByRole('dialog', { name: 'New ad-hoc customer' })
  await userEvent.type(within(sheet).getByLabelText('Name'), 'Walk-in Ltd')
  await userEvent.selectOptions(within(sheet).getByLabelText('Currency'), 'ZAR')
  await userEvent.click(within(sheet).getByRole('button', { name: 'Save' }))
  await vi.waitFor(() => expect(created).toHaveLength(1))
  expect(await screen.findByText('Priced in ZAR')).toBeInTheDocument()
  const validUntil = screen.getByLabelText('Valid until') as HTMLInputElement
  const issue = (screen.getByLabelText('Quote date') as HTMLInputElement).value
  expect(validUntil.value).toBe(addDays(issue, 14))
  expect(screen.getByLabelText('Terms')).toHaveValue('Deposit 50%.')
})

test('Settings → Quotes saves the validity, numbers, e-mail and terms', async () => {
  const saved: Record<string, unknown>[] = []
  mockFetch({
    ...base,
    'PUT /api/v1/settings/quotes': async (req) => { const b = await req.json(); saved.push(b); return json({ ...b, nextSequence: 3, version: 2 })() },
  })
  window.history.pushState({}, '', '/settings/quotes')
  render(<App />)
  const days = await screen.findByLabelText('Quotes are valid for (days)')
  await userEvent.clear(days)
  await userEvent.type(days, '45')
  await userEvent.click(screen.getByRole('button', { name: 'Save quote settings' }))
  await vi.waitFor(() => expect(saved).toHaveLength(1))
  expect(saved[0]).toMatchObject({ validDays: 45, numberFormat: 'Q-{seq}/{yy}', terms: 'Deposit 50%.', nextSequence: 0, version: 1 })
  expect(await screen.findByText('Saved.')).toBeInTheDocument()
})
