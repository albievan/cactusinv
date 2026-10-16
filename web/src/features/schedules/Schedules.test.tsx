import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import type { Schemas } from '../../lib/api/client'
import { json, mockFetch } from '../../test/fetch'
import { ScheduleDetailPage, ScheduleEditor } from './Schedules'

afterEach(() => vi.unstubAllGlobals())

type Line = Schemas['ScheduleLine']
type Product = Schemas['Product']

const products: Product[] = [
  { id: 'p1', sku: 'SS-USERS', name: 'Stock system users', description: 'Stock system usage', unit: 'user', active: true, version: 1, prices: [{ currency: 'ZAR', unitPrice: '192.94' }] },
  { id: 'p2', sku: 'INTEG', name: 'Integration subscription', description: '', unit: 'month', active: true, version: 1, prices: [{ currency: 'ZAR', unitPrice: '550.00' }] },
  { id: 'p3', sku: 'SUPPORT', name: 'Support hours', description: '', unit: 'hour', active: true, version: 1, prices: [{ currency: 'ZAR', unitPrice: '100.00' }] },
]

/** A server that stores lines as the API does (description empty) and answers reads with the product's name and price from the look-up. */
function server(stored: Line[], extra: Record<string, () => Response> = {}) {
  const puts: Schemas['Schedule'][] = []
  let lines = stored
  let version = 3
  const read = (): Line[] => lines.map((l) => {
    const p = products.find((x) => x.id === l.productId)
    return { ...l, description: '', productName: p?.name ?? '', productStatus: p ? 'active' : 'missing', productUnitPrice: p?.prices.find((x) => x.currency === 'ZAR')?.unitPrice }
  })
  const schedule = (): Schemas['Schedule'] => ({
    id: 's1', customerId: 'c1', name: 'Acme monthly', kind: 'invoice', status: 'active', header: {}, notes: '', autoSend: false, emailDelayDays: 0, emailSubject: '', emailBody: '', version,
    recurrence: { frequency: 'weekly', interval: 1, dayOfMonth: 1, weekday: 5, sendTime: '09:00', timeZone: 'UTC', startDate: '2026-10-01' },
    lines: read(), nextRunAt: '2026-10-16T09:00:00Z',
  })
  const detail = () => json({ schedule: schedule(), upcoming: ['2026-10-16T09:00:00Z', '2026-10-30T09:00:00Z'], runs: [] })()
  mockFetch({
    'GET /api/v1/customers': json({ items: [{ id: 'c1', code: 'CD', name: 'Acme', currency: 'ZAR' }], total: 1 }),
    'GET /api/v1/currencies': json({ items: [{ code: 'ZAR', name: 'Rand', symbol: 'R', minorUnits: 2, active: true }] }),
    'GET /api/v1/products': json({ items: products, total: products.length }),
    'POST /api/v1/schedules/preview': json({ runs: ['2026-10-16T09:00:00Z', '2026-10-23T09:00:00Z', '2026-10-30T09:00:00Z'], timeZone: 'UTC' }),
    'GET /api/v1/schedules/s1': detail,
    'PUT /api/v1/schedules/s1': async (req: Request) => {
      const body = (await req.json()) as Schemas['Schedule']
      puts.push(body)
      lines = body.lines.map((l) => ({ productId: l.productId, quantity: l.quantity, unitPrice: l.unitPrice, discountPercent: l.discountPercent }))
      version++
      return detail()
    },
    ...extra,
  })
  return { puts }
}

function app() {
  return render(
    <MemoryRouter initialEntries={['/schedules/s1/edit']}>
      <Routes>
        <Route path="/schedules/:id" element={<ScheduleDetailPage />} />
        <Route path="/schedules/:id/edit" element={<ScheduleEditor />} />
      </Routes>
    </MemoryRouter>,
  )
}

const product = (n: number) => screen.findByRole('combobox', { name: `Product for line ${n}` })

test('editing an existing schedule shows each product and its amount', async () => {
  server([
    { productId: 'p1', quantity: '14', discountPercent: '0' },
    { productId: 'p2', quantity: '1', unitPrice: '600.00', discountPercent: '0' },
  ])
  app()
  expect(await product(1)).toHaveValue('Stock system users')
  expect(await product(2)).toHaveValue('Integration subscription')
  expect(screen.getByText('192.94')).toBeInTheDocument()
  expect(screen.getByText('2,701.16')).toBeInTheDocument()
  expect(screen.getAllByText('600.00')).toHaveLength(2) // unit price and amount: the fixed price, not the product's 550.00
  expect(screen.queryByText('550.00')).toBeNull()
  expect(screen.getByText('fixed price')).toBeInTheDocument()
  expect(screen.getByText('ZAR 3,301.16')).toBeInTheDocument()
  expect(screen.queryByText('Choose a product for every line')).toBeNull()
})

test('a product chosen again is saved, and shown when the schedule is reopened', async () => {
  const { puts } = server([
    { productId: 'p1', quantity: '14', discountPercent: '0' },
    { productId: 'p2', quantity: '1', unitPrice: '600.00', discountPercent: '0' },
  ])
  app()
  const box = await product(1)
  await userEvent.clear(box)
  await userEvent.type(box, 'sup')
  await userEvent.click(await within(await screen.findByRole('listbox')).findByText('Support hours'))
  expect(box).toHaveValue('Support hours')
  expect(screen.getByText('1,400.00')).toBeInTheDocument()
  // Choosing the same product again keeps its fixed price.
  const box2 = await product(2)
  await userEvent.click(box2)
  await userEvent.click(await within(await screen.findByRole('listbox')).findByText('Integration subscription'))
  expect(screen.getByText('ZAR 2,000.00')).toBeInTheDocument()

  await userEvent.click(screen.getByRole('button', { name: 'Save schedule' }))
  expect(await screen.findByRole('heading', { name: 'Acme monthly' })).toBeInTheDocument()
  expect(puts).toHaveLength(1)
  expect(puts[0]!.lines).toEqual([
    { productId: 'p3', quantity: '14', discountPercent: '0' },
    { productId: 'p2', quantity: '1', unitPrice: '600.00', discountPercent: '0' },
  ])

  await userEvent.click(screen.getByRole('link', { name: 'Edit' }))
  expect(await product(1)).toHaveValue('Support hours')
  expect(await product(2)).toHaveValue('Integration subscription')
  expect(screen.getByText('ZAR 2,000.00')).toBeInTheDocument()
})

test('a product no longer available, or without a price in the currency, says what to do', async () => {
  server([{ productId: 'p1', quantity: '1', discountPercent: '0' }], {
    'GET /api/v1/schedules/s1': json({
      schedule: {
        id: 's1', customerId: 'c1', name: 'Acme monthly', kind: 'invoice', status: 'active', header: {}, notes: '', autoSend: false, emailDelayDays: 0, emailSubject: '', emailBody: '', version: 1,
        recurrence: { frequency: 'monthly', interval: 1, dayOfMonth: 1, weekday: 1, sendTime: '09:00', timeZone: 'UTC', startDate: '2026-10-01' },
        lines: [
          { productId: 'gone', quantity: '1', discountPercent: '0', description: '', productName: '', productStatus: 'missing' },
          { productId: 'old', quantity: '1', discountPercent: '0', description: '', productName: 'Old support plan', productStatus: 'inactive', productUnitPrice: '250.00' },
          { productId: 'usd', quantity: '1', discountPercent: '0', description: '', productName: 'Dollar thing', productStatus: 'active' },
        ],
      },
      upcoming: [], runs: [],
    }),
  })
  app()
  expect(await product(2)).toHaveValue('Old support plan')
  expect(screen.getByText('Product no longer available — choose another.')).toBeInTheDocument()
  expect(screen.getByText('Old support plan: product no longer available — choose another.')).toBeInTheDocument()
  expect(screen.getByText('No ZAR price for this product — choose another product.')).toBeInTheDocument()
  expect(screen.getByText('Choose a product for every line')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Save schedule' }))
  expect(await screen.findByText('Choose a product for every line.')).toBeInTheDocument()
})

test('next runs are shown in the time zone the server names', async () => {
  server([{ productId: 'p1', quantity: '1', discountPercent: '0' }])
  app()
  expect(await screen.findByText(/Fri 16 Oct 2026, 09:00 UTC · Fri 23 Oct 2026, 09:00 UTC/)).toBeInTheDocument()
  expect(screen.getByText('UTC time')).toBeInTheDocument()
})

test('the schedule page lists upcoming runs in the schedule time zone', async () => {
  server([{ productId: 'p1', quantity: '1', discountPercent: '0' }])
  render(
    <MemoryRouter initialEntries={['/schedules/s1']}>
      <Routes><Route path="/schedules/:id" element={<ScheduleDetailPage />} /></Routes>
    </MemoryRouter>,
  )
  expect(await screen.findByText('Fri 16 Oct 2026, 09:00 UTC')).toBeInTheDocument()
  expect(screen.getByText('Fri 30 Oct 2026, 09:00 UTC')).toBeInTheDocument()
})
