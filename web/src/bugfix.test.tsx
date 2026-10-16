import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { App } from './App'
import { Statements } from './features/statements/Statements'
import { ProductEditor } from './features/products/Products'
import { json, mockFetch } from './test/fetch'

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

test('statement totals add exactly in each currency', async () => {
  const row = (id: string, currency: string, total: string) => ({
    customerId: id, code: id, name: id, email: `${id}@example.com`, currency, total, overdueCount: 0, maxDaysOverdue: 0, openCount: 1,
    aging: { current: total, days1to30: '0', days31to60: '0', days61to90: '0', over90: '0' },
  })
  mockFetch({
    'GET /api/v1/currencies': json({ items: [{ code: 'KWD', name: 'Dinar', symbol: 'KD', minorUnits: 3, active: true }, { code: 'JPY', name: 'Yen', symbol: '¥', minorUnits: 0, active: true }] }),
    'GET /api/v1/statements': json({ asOf: '2026-10-16', items: [row('a', 'KWD', '1.235'), row('b', 'KWD', '2.001'), row('c', 'JPY', '1500')] }),
  })
  render(<MemoryRouter><Statements /></MemoryRouter>)
  // The "Owed to you" cards (the table also shows each customer's own balance).
  const card = async (cur: string) => (await screen.findByRole('heading', { name: `Owed to you · ${cur}` })).closest('section')!
  expect(within(await card('KWD')).getByText('KWD 3.236')).toBeInTheDocument()
  expect(within(await card('JPY')).getByText('JPY 1,500')).toBeInTheDocument()
})

test('deleting asks first, and Cancel deletes nothing', async () => {
  let deleted = 0
  mockFetch({
    'GET /api/v1/currencies': json({ items: [{ code: 'ZAR', name: 'Rand', symbol: 'R', minorUnits: 2, active: true }] }),
    'GET /api/v1/products/p1': json({ id: 'p1', sku: 'S', name: 'Widget', description: '', unit: '', prices: [{ currency: 'ZAR', unitPrice: '1.00' }], active: true, version: 1 }),
    'DELETE /api/v1/products/p1': () => {
      deleted++
      return new Response(null, { status: 204 })
    },
    'GET /api/v1/products': json({ items: [], total: 0 }),
  })
  render(
    <MemoryRouter initialEntries={['/products/p1']}>
      <Routes>
        <Route path="/products/:id" element={<ProductEditor />} />
        <Route path="/products" element={<p>list</p>} />
      </Routes>
    </MemoryRouter>,
  )
  await userEvent.click(await screen.findByRole('button', { name: 'Delete' }))
  const sheet = screen.getByRole('dialog', { name: 'Delete this product?' })
  await userEvent.click(within(sheet).getByRole('button', { name: 'Cancel' }))
  expect(deleted).toBe(0)
  await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
  await userEvent.click(within(screen.getByRole('dialog', { name: 'Delete this product?' })).getByRole('button', { name: 'Delete' }))
  expect(await screen.findByText('list')).toBeInTheDocument()
  expect(deleted).toBe(1)
})

test('a timed-out session signs in over the page, keeping what was typed', async () => {
  let expired = false
  mockFetch({
    'GET /api/v1/auth/me': () => (expired ? json({ type: 'u', title: 'Sign in required', status: 401 }, 401)() : json({ id: '1', email: 'admin', administrator: true, permissions: [] })()),
    'GET /api/v1/version': json({ version: '1.7.1', build: '1' }),
    'GET /api/v1/currencies': json({ items: [{ code: 'ZAR', name: 'Rand', symbol: 'R', minorUnits: 2, active: true }] }),
    'POST /api/v1/products': () => {
      expired = true
      return json({ type: 'u', title: 'Sign in required', status: 401 }, 401)()
    },
    'POST /api/v1/auth/login': () => {
      expired = false
      return new Response(null, { status: 204 })
    },
  })
  window.history.pushState({}, '', '/products/new')
  render(<App />)
  const name = await screen.findByLabelText('Name')
  await userEvent.type(name, 'Typed before the timeout')
  await userEvent.type(screen.getByLabelText('SKU'), 'SKU-1')
  await userEvent.type(screen.getByLabelText('Unit cost 1'), '10')
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  const dialog = await screen.findByRole('dialog', { name: 'Sign in again' })
  expect(within(dialog).getByRole('alert')).toHaveTextContent('timed out')
  await userEvent.type(within(dialog).getByLabelText('User name'), 'admin')
  await userEvent.type(within(dialog).getByLabelText('Password'), 'pw')
  await userEvent.click(within(dialog).getByRole('button', { name: 'Sign in' }))
  await vi.waitFor(() => expect(screen.queryByRole('dialog', { name: 'Sign in again' })).not.toBeInTheDocument())
  expect(screen.getByLabelText('Name')).toHaveValue('Typed before the timeout')
})
