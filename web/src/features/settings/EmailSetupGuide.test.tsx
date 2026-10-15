import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { EmailSetupGuide, type GuideProvider } from './EmailSetupGuide'

function Harness({ provider }: { provider: GuideProvider }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Setup guide
      </button>
      <EmailSetupGuide open={open} onClose={() => setOpen(false)} provider={provider} />
    </>
  )
}

test('opens on the selected provider and shows its steps in order', async () => {
  render(<Harness provider="Gmail" />)
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Setup guide' }))

  const dialog = screen.getByRole('dialog', { name: 'Set up the e-mail server' })
  expect(within(dialog).getByRole('tab', { name: 'Gmail' })).toHaveAttribute('aria-selected', 'true')
  const steps = within(within(dialog).getByRole('list', { name: 'Steps for Gmail' })).getAllByRole('listitem')
  expect(steps).toHaveLength(6)
  expect(steps[0]).toHaveTextContent('2-Step Verification')
  expect(steps[1]).toHaveTextContent('App passwords')
  expect(steps[2]).toHaveTextContent('smtp.gmail.com')
  expect(steps[4]).toHaveTextContent('Send mail as')
  expect(within(dialog).getByText('If the test e-mail fails')).toBeInTheDocument()
})

test('switches to another provider', async () => {
  render(<Harness provider="Gmail" />)
  await userEvent.click(screen.getByRole('button', { name: 'Setup guide' }))
  const dialog = screen.getByRole('dialog')

  await userEvent.click(within(dialog).getByRole('tab', { name: 'Outlook / Microsoft 365' }))
  const outlook = within(dialog).getByRole('list', { name: 'Steps for Outlook / Microsoft 365' })
  expect(outlook).toHaveTextContent('smtp.office365.com')
  expect(outlook).toHaveTextContent('Authenticated SMTP')

  await userEvent.click(within(dialog).getByRole('tab', { name: 'Custom' }))
  const custom = within(dialog).getByRole('list', { name: 'Steps for Custom' })
  expect(custom).toHaveTextContent('SPF and DKIM')
  expect(within(dialog).getByRole('tab', { name: 'Custom' })).toHaveAttribute('aria-selected', 'true')
})

test('opens on the provider the form has selected', async () => {
  render(<Harness provider="Outlook / Microsoft 365" />)
  await userEvent.click(screen.getByRole('button', { name: 'Setup guide' }))
  expect(screen.getByRole('list', { name: 'Steps for Outlook / Microsoft 365' })).toBeInTheDocument()
})

test('closes with the Close button and with Escape', async () => {
  render(<Harness provider="Custom" />)
  await userEvent.click(screen.getByRole('button', { name: 'Setup guide' }))
  await userEvent.click(screen.getByRole('button', { name: 'Close' }))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

  await userEvent.click(screen.getByRole('button', { name: 'Setup guide' }))
  await userEvent.keyboard('{Escape}')
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})
