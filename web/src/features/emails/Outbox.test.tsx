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
  expect(tabs.map((t) => t.textContent)).toEqual(['pending', 'failed', 'sent', 'cancelled'])
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
