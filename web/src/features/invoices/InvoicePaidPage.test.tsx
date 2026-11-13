import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../../App'
import { json, mockFetch } from '../../test/fetch'
import { CustomerPaid } from './InvoiceDetail'
import type { Invoice } from '../../lib/api/client'

const token = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNO_-'
const page = {
  number: 'CD/1201/26', businessName: 'Example Software', businessEmail: 'billing@example.com', billToName: 'Acme Ltd', currency: 'ZAR',
  issueDate: '2026-11-01', dueDate: '2026-11-30', total: '3250.00', stage: 'emailed', canMark: true,
}

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

test('the Mark as paid link opens the invoice without sign-in; the customer adds a note and marks it', async () => {
  const posts: unknown[] = []
  mockFetch({
    [`GET /api/v1/public/invoices/${token}`]: json(page),
    [`POST /api/v1/public/invoices/${token}/paid`]: async (req) => {
      posts.push(await req.json())
      return json({ ...page, canMark: false, customerPaidAt: '2026-11-13T09:00:00Z', customerPaidNote: 'EFT ref AC-7. Thanks!' })()
    },
  })
  window.history.pushState({}, '', `/i/${token}`)
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'Invoice CD/1201/26' })).toBeInTheDocument()
  expect(screen.getByText('from Example Software to Acme Ltd')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: /View the invoice/ })).toHaveAttribute('href', `/api/v1/public/invoices/${token}/pdf`)
  expect(screen.queryByLabelText('User name')).not.toBeInTheDocument()
  expect(posts).toEqual([]) // opening the link marks nothing

  await userEvent.type(screen.getByLabelText('Note for the business (optional)'), 'EFT ref AC-7. Thanks!')
  await userEvent.click(screen.getByRole('button', { name: 'Mark as paid' }))
  expect(await screen.findByText(/Example Software has been told that you paid invoice CD\/1201\/26, with your note/)).toBeInTheDocument()
  expect(posts).toEqual([{ note: 'EFT ref AC-7. Thanks!' }])
  expect(screen.queryByRole('button', { name: 'Mark as paid' })).not.toBeInTheDocument()
})

test('a marked or paid invoice says so; an unknown link says it does not work', async () => {
  mockFetch({ [`GET /api/v1/public/invoices/${token}`]: json({ ...page, canMark: false, customerPaidAt: '2026-11-13T09:00:00Z', customerPaidNote: 'Paid Friday' }) })
  window.history.pushState({}, '', `/i/${token}`)
  const first = render(<App />)
  expect(await screen.findByText(/You told Example Software on .* that you paid this invoice/)).toBeInTheDocument()
  expect(screen.getByText('Your note: “Paid Friday”')).toBeInTheDocument()
  first.unmount()

  mockFetch({ [`GET /api/v1/public/invoices/${token}`]: json({ ...page, stage: 'paid', canMark: false }) })
  const second = render(<App />)
  expect(await screen.findByText('This invoice is paid. Thank you.')).toBeInTheDocument()
  second.unmount()

  mockFetch({})
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'This link does not work' })).toBeInTheDocument()
})

test('the invoice page shows that the customer marked it as paid, with their note', () => {
  const inv = { status: 'sent', customerPaidAt: '2026-11-13T09:00:00Z', customerPaidNote: 'EFT ref AC-7' } as Invoice
  const { rerender } = render(<CustomerPaid inv={inv} />)
  expect(screen.getByRole('note')).toHaveTextContent(/Customer marked as paid ·/)
  expect(screen.getByRole('note')).toHaveTextContent('“EFT ref AC-7”')
  expect(screen.getByText(/Check that the payment arrived, then mark the invoice paid/)).toBeInTheDocument()
  rerender(<CustomerPaid inv={{ ...inv, status: 'paid' }} />)
  expect(screen.queryByText(/Check that the payment arrived/)).not.toBeInTheDocument()
  rerender(<CustomerPaid inv={{ ...inv, customerPaidAt: undefined }} />)
  expect(screen.queryByRole('note')).not.toBeInTheDocument()
})
