import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../../App'
import { json, mockFetch } from '../../test/fetch'

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

const customer = { id: 'c1', code: 'CD', name: 'City Deep', email: 'j@citydeep.example', currency: 'ZAR', active: true, paymentTerms: { kind: 'days_after', value: 30, text: '30 days' } }
const adhoc = { id: 'a1', name: 'Ah-hoc customet', email: 'albie@example.com', attention: 'Albie', address: '', phone: '', taxNumber: '', currency: 'ZAR', version: 1 }

function routes(me: object, put?: (req: Request) => Promise<Response>) {
  const adhocQueries: string[] = []
  mockFetch({
    'GET /api/v1/auth/me': json({ id: '1', email: 'admin', automationPaused: false, ...me }),
    'GET /api/v1/version': json({ version: '1.29.0', build: '1' }),
    'GET /api/v1/currencies': json({ items: [{ code: 'ZAR', name: 'Rand', symbol: 'R', minorUnits: 2, active: true }] }),
    'GET /api/v1/customers': json({ items: [customer], total: 1 }),
    'GET /api/v1/adhoc-customers': (req) => {
      adhocQueries.push(new URL(req.url).search)
      return json({ items: [adhoc], total: 1 })()
    },
    ...(put ? { 'PUT /api/v1/adhoc-customers/a1': put } : {}),
  })
  return adhocQueries
}

test('the Customers page shows the ad-hoc customers not made customers yet, and edits one there', async () => {
  const puts: Record<string, unknown>[] = []
  const queries = routes({ administrator: true, permissions: [] }, async (req) => {
    const body = (await req.json()) as Record<string, unknown>
    puts.push(body)
    return json({ ...adhoc, ...body, version: 2 })()
  })
  window.history.pushState({}, '', '/customers')
  render(<App />)
  expect(await screen.findByText('City Deep')).toBeInTheDocument()
  expect(screen.queryByText('Ah-hoc customet')).not.toBeInTheDocument()

  await userEvent.click(screen.getByRole('radio', { name: 'Ad-hoc (not yet customers)' }))
  expect(window.location.search).toBe('?show=adhoc')
  expect(await screen.findByText('Ah-hoc customet')).toBeInTheDocument()
  expect(screen.queryByText('City Deep')).not.toBeInTheDocument()
  expect(queries.every((s) => !s.includes('includeConverted'))).toBe(true) // only those not converted
  expect(screen.getByRole('button', { name: 'Make Ah-hoc customet a customer' })).toBeInTheDocument()

  await userEvent.click(screen.getByRole('button', { name: 'Edit Ah-hoc customet' }))
  const sheet = screen.getByRole('dialog', { name: 'Edit Ah-hoc customet' })
  expect(within(sheet).getByText(/their open quotes/)).toBeInTheDocument()
  const name = within(sheet).getByLabelText('Name')
  await userEvent.clear(name)
  await userEvent.type(name, 'Ad-hoc customer')
  await userEvent.click(within(sheet).getByRole('button', { name: 'Save' }))
  await vi.waitFor(() => expect(puts).toEqual([expect.objectContaining({ id: 'a1', name: 'Ad-hoc customer', version: 1 })]))
  expect(await screen.findByText('Ad-hoc customer saved.')).toBeInTheDocument()

  await userEvent.click(screen.getByRole('radio', { name: 'Customers' }))
  expect(window.location.search).toBe('')
  expect(await screen.findByText('City Deep')).toBeInTheDocument()
})

test('without quotes.view there is no ad-hoc filter; ?show=adhoc shows customers', async () => {
  routes({ administrator: false, permissions: ['customers.view'] })
  window.history.pushState({}, '', '/customers?show=adhoc')
  render(<App />)
  expect(await screen.findByText('City Deep')).toBeInTheDocument()
  expect(screen.queryByRole('radio', { name: 'Ad-hoc (not yet customers)' })).not.toBeInTheDocument()
})

test('with quotes.view but not quotes.edit the ad-hoc list has no Edit', async () => {
  routes({ administrator: false, permissions: ['customers.view', 'quotes.view'] })
  window.history.pushState({}, '', '/customers?show=adhoc')
  render(<App />)
  expect(await screen.findByText('Ah-hoc customet')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Edit Ah-hoc customet' })).not.toBeInTheDocument()
})
