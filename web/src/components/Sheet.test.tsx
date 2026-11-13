import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Sheet } from './ui'

// The close button sits at the right edge of the header, not right after the title.
test('a sheet has its close button at the right of the header, and closes', async () => {
  const onClose = vi.fn()
  render(<Sheet title="E-mail quote" open onClose={onClose}><p>Body</p></Sheet>)
  const close = screen.getByRole('button', { name: 'Close' })
  expect(close).toHaveClass('ms-auto', 'flex-shrink-0')
  expect(close.parentElement).toHaveClass('modal-header')
  expect(close.previousElementSibling).toHaveTextContent('E-mail quote')
  await userEvent.click(close)
  await userEvent.keyboard('{Escape}')
  expect(onClose).toHaveBeenCalledTimes(2)
})
