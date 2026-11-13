import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../../App'
import { json, mockFetch } from '../../test/fetch'

const token = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNO_-'
const page = {
  number: 'QU26-7', businessName: 'Example Software', businessEmail: 'billing@example.com', billToName: 'Prospect Ltd', currency: 'ZAR',
  issueDate: '2026-11-01', validUntil: '2026-12-01', total: '15000.00', stage: 'sent', expired: false, canAnswer: true,
  lines: [{ description: 'Website build', quantity: '1', amount: '15000.00' }],
}

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

test('the Accept link opens the quote without sign-in, and one click accepts it', async () => {
  const answers: unknown[] = []
  mockFetch({
    [`GET /api/v1/public/quotes/${token}`]: json(page),
    [`POST /api/v1/public/quotes/${token}/response`]: async (req) => {
      answers.push(await req.json())
      return json({ ...page, stage: 'accepted', canAnswer: false, answeredBy: 'customer' })()
    },
  })
  window.history.pushState({}, '', `/q/${token}?answer=accept`)
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'Quotation QU26-7' })).toBeInTheDocument()
  expect(screen.getByText('from Example Software for Prospect Ltd')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: /View the quotation/ })).toHaveAttribute('href', `/api/v1/public/quotes/${token}/pdf`)
  expect(screen.queryByLabelText('User name')).not.toBeInTheDocument() // no sign-in
  expect(answers).toEqual([]) // opening the link answers nothing

  await userEvent.click(screen.getByRole('button', { name: 'Accept quote' }))
  expect(await screen.findByText(/Thank you: you accepted this quotation/)).toBeInTheDocument()
  expect(answers).toEqual([{ answer: 'accept' }])
  expect(screen.queryByRole('button', { name: 'Accept quote' })).not.toBeInTheDocument()
})

test('the Decline link asks for an optional reason', async () => {
  const answers: unknown[] = []
  mockFetch({
    [`GET /api/v1/public/quotes/${token}`]: json(page),
    [`POST /api/v1/public/quotes/${token}/response`]: async (req) => {
      answers.push(await req.json())
      return json({ ...page, stage: 'declined', canAnswer: false, answeredBy: 'customer', declineReason: 'Too dear' })()
    },
  })
  window.history.pushState({}, '', `/q/${token}?answer=decline`)
  render(<App />)
  await userEvent.type(await screen.findByLabelText('Reason (optional)'), 'Too dear')
  expect(screen.queryByRole('button', { name: 'Accept quote' })).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Decline quote' }))
  expect(await screen.findByText(/you declined this quotation/)).toBeInTheDocument()
  expect(answers).toEqual([{ answer: 'decline', reason: 'Too dear' }])
})

test('an expired quote cannot be answered; an unknown link says so', async () => {
  mockFetch({ [`GET /api/v1/public/quotes/${token}`]: json({ ...page, stage: 'expired', expired: true, canAnswer: false }) })
  window.history.pushState({}, '', `/q/${token}?answer=accept`)
  const { unmount } = render(<App />)
  expect(await screen.findByText(/expired on .* and can no longer be accepted/)).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'billing@example.com' })).toHaveAttribute('href', 'mailto:billing@example.com')
  expect(screen.queryByRole('button', { name: 'Accept quote' })).not.toBeInTheDocument()
  unmount()

  mockFetch({})
  window.history.pushState({}, '', `/q/${token}`)
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'This link does not work' })).toBeInTheDocument()
})
