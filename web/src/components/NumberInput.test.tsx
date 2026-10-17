import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { NumberInput } from './ui'

function Day() {
  const [v, setV] = useState(2)
  return (
    <>
      <label htmlFor="d">Day</label>
      <NumberInput id="d" min={2} max={31} value={v} onChange={setV} />
      <output>{v}</output>
    </>
  )
}

test('a day of 15 can be typed, and an out-of-range one is clamped when leaving the field', async () => {
  render(<Day />)
  const day = screen.getByLabelText('Day')
  await userEvent.clear(day)
  await userEvent.type(day, '15')
  expect(screen.getByRole('status')).toHaveTextContent('15')
  await userEvent.clear(day)
  await userEvent.type(day, '45')
  await userEvent.tab()
  expect(day).toHaveValue(31)
  expect(screen.getByRole('status')).toHaveTextContent('31')
})
