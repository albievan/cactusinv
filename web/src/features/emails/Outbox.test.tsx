import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { json, mockFetch } from '../../test/fetch'
import { Outbox } from './Outbox'

afterEach(() => vi.unstubAllGlobals())

test('the status filter tabs are readable whether selected or not', async () => {
  mockFetch({ 'GET /api/v1/emails': json({ items: [], total: 0 }) })
  render(<MemoryRouter><Outbox /></MemoryRouter>)
  expect(await screen.findByText('Nothing pending.')).toBeInTheDocument()

  const tabs = screen.getAllByRole('tab')
  expect(tabs.map((t) => t.textContent)).toEqual(['pending', 'sending', 'failed', 'sent', 'cancelled'])
  const selected = screen.getByRole('tab', { name: 'pending' })
  expect(selected).toHaveAttribute('aria-selected', 'true')
  expect(selected).toHaveClass('bg-brand-500', 'text-white')
  for (const name of ['failed', 'sent', 'cancelled']) {
    const tab = screen.getByRole('tab', { name })
    expect(tab).toHaveAttribute('aria-selected', 'false')
    expect(tab).toHaveClass('bg-white', 'text-brand-900', 'ring-cream-300')
    expect(tab.className).not.toMatch(/dark:/)
  }

  await userEvent.click(screen.getByRole('tab', { name: 'failed' }))
  expect(screen.getByRole('tab', { name: 'failed' })).toHaveClass('bg-brand-500', 'text-white')
  expect(screen.getByRole('tab', { name: 'pending' })).toHaveClass('bg-white', 'text-brand-900')
})

test('older messages page in, and a message being sent can be seen', async () => {
  const msg = (i: number) => ({
    id: `m${i}`, kind: 'invoice', invoiceId: 'i1', to: ['a@b.example'], cc: [], subject: `Invoice ${i}`, body: '', status: 'pending',
    attempts: 0, sendAt: '2026-10-17T08:00:00Z', nextAttemptAt: '2026-10-17T08:00:00Z', lastError: '', createdAt: '2026-10-17T08:00:00Z',
  })
  mockFetch({
    'GET /api/v1/emails': (req) => {
      const q = new URL(req.url).searchParams
      if (q.get('status') === 'sending') return json({ items: [{ ...msg(999), status: 'sending' }], total: 1 })()
      const offset = Number(q.get('offset') ?? 0)
      const items = Array.from({ length: Math.min(100, 130 - offset) }, (_, i) => msg(offset + i))
      return json({ items, total: 130 })()
    },
  })
  render(<MemoryRouter><Outbox /></MemoryRouter>)
  expect(await screen.findByText('Invoice 99')).toBeInTheDocument()
  expect(screen.queryByText('Invoice 100')).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Show more (30 older)' }))
  expect(await screen.findByText('Invoice 129')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /Show more/ })).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('tab', { name: 'sending' }))
  expect(await screen.findByText('Invoice 999')).toBeInTheDocument()
})
