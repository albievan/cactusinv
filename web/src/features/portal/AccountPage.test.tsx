import { render, screen, within } from '@testing-library/react'
import { App } from '../../App'
import { json, mockFetch } from '../../test/fetch'

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

const token = 'A'.repeat(43)

test('the account page shows what is owed, invoice by invoice, with the PDFs, without sign-in', async () => {
  mockFetch({
    [`GET /api/v1/public/accounts/${token}`]: json({
      customerName: 'Acme', businessName: 'Example', businessEmail: 'billing@example.com', currency: 'ZAR', asOf: '2026-11-24', balance: '600.00',
      aging: { current: '600.00', days1to30: '0.00', days31to60: '0.00', days61to90: '0.00', over90: '0.00' },
      open: [{ id: 'i1', number: 'CD/1/26', issueDate: '2026-11-01', dueDate: '2026-11-30', total: '1000.00', balance: '600.00', stage: 'emailed', daysOverdue: 0 }],
      paid: [{ id: 'i0', number: 'CD/0/26', issueDate: '2026-10-01', dueDate: '2026-10-31', total: '250.00', balance: '0.00', stage: 'paid', daysOverdue: 0 }],
    }),
  })
  window.history.pushState({}, '', `/a/${token}`)
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'Acme' })).toBeInTheDocument()
  const open = screen.getByRole('region', { name: 'Unpaid invoices' })
  expect(open).toHaveTextContent('CD/1/26')
  expect(open).toHaveTextContent(/ZAR 600\.00of ZAR 1,000\.00/)
  expect(within(open).getByRole('link', { name: 'Invoice CD/1/26 (PDF)' })).toHaveAttribute('href', `/api/v1/public/accounts/${token}/invoices/i1/pdf`)
  expect(screen.getByRole('region', { name: 'Recently paid' })).toHaveTextContent('CD/0/26')
  expect(screen.getByRole('link', { name: /Statement \(PDF\)/ })).toHaveAttribute('href', `/api/v1/public/accounts/${token}/statement`)
})

test('an expired or unknown link says so', async () => {
  mockFetch({ [`GET /api/v1/public/accounts/${token}`]: json({ type: 'x', title: 'Not found', status: 404 }, 404) })
  window.history.pushState({}, '', `/a/${token}`)
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'This link does not work' })).toBeInTheDocument()
})
