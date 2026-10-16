import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import type { Schemas } from '../../lib/api/client'
import { json, mockFetch } from '../../test/fetch'
import { describe as describeRecurrence, ScheduleEditor } from './Schedules'

afterEach(() => vi.unstubAllGlobals())

function serve() {
  const previews: Schemas['Recurrence'][] = []
  mockFetch({
    'GET /api/v1/schedules/defaults': json({}),
    'GET /api/v1/settings/business': json({ defaultNotes: '' }),
    'GET /api/v1/currencies': json({ items: [] }),
    'GET /api/v1/customers': json({ items: [], total: 0 }),
    // The server moves the run: 1 Nov 2026 is a Sunday, so it runs Fri 30 Oct.
    'POST /api/v1/schedules/preview': async (req: Request) => {
      const r = (await req.json()) as Schemas['Recurrence']
      previews.push(r)
      // Omitted, the server applies its default for a new schedule and says which it used.
      const rule = r.weekendRule ?? 'previous-friday'
      return json({ runs: rule === 'none' ? ['2026-11-01T07:00:00Z'] : ['2026-10-30T07:00:00Z'], timeZone: 'Africa/Johannesburg', weekendRule: rule })()
    },
  })
  render(
    <MemoryRouter initialEntries={['/schedules/new']}>
      <Routes><Route path="/schedules/new" element={<ScheduleEditor />} /></Routes>
    </MemoryRouter>,
  )
  return previews
}

test('monthly offers first day, last day or a day, and the weekend choice; next runs are the server’s', async () => {
  const previews = serve()
  const on = await screen.findByLabelText('On')
  expect(on).toHaveValue('first')
  // The form does not choose the rule: it shows the one the server's preview applied.
  await waitFor(() => expect(screen.getByLabelText('If it falls on a weekend')).toHaveValue('previous-friday'))
  expect(previews[0]!.weekendRule).toBeUndefined()
  const nextRuns = () => screen.getByText('Next runs:').parentElement!
  await waitFor(() => expect(nextRuns()).toHaveTextContent('Next runs: Fri 30 Oct 2026, 09:00 Africa/Johannesburg'))
  expect(previews.at(-1)).toMatchObject({ frequency: 'monthly', dayOfMonth: 1 })

  await userEvent.selectOptions(on, 'last')
  await waitFor(() => expect(previews.at(-1)).toMatchObject({ dayOfMonth: 0 }))
  expect(screen.queryByLabelText('Day')).toBeNull()

  await userEvent.selectOptions(on, 'day')
  const day = screen.getByLabelText('Day')
  expect(day).toHaveValue(15)
  await userEvent.clear(day)
  await userEvent.type(day, '31')
  await waitFor(() => expect(previews.at(-1)).toMatchObject({ dayOfMonth: 31 }))
  expect(screen.getByText("A day past the month's end runs on its last day.")).toBeInTheDocument()

  await userEvent.selectOptions(screen.getByLabelText('If it falls on a weekend'), 'none')
  await waitFor(() => expect(previews.at(-1)).toMatchObject({ weekendRule: 'none' }))
  await waitFor(() => expect(nextRuns()).toHaveTextContent('Next runs: Sun 1 Nov 2026, 09:00 Africa/Johannesburg'))

  await userEvent.selectOptions(screen.getByLabelText('Repeats'), 'weekly')
  expect(screen.queryByLabelText('If it falls on a weekend')).toBeNull()
})

test('the summary names the day and the weekend rule', () => {
  const r: Schemas['Recurrence'] = { frequency: 'monthly', interval: 1, dayOfMonth: 1, weekday: 0, sendTime: '09:00', startDate: '2026-10-01', weekendRule: 'previous-friday' }
  expect(describeRecurrence(r)).toBe('every month on the first day at 09:00 (the Friday before if on a weekend)')
  expect(describeRecurrence({ ...r, dayOfMonth: 0, weekendRule: 'none' })).toBe('every month on the last day at 09:00')
  expect(describeRecurrence({ ...r, frequency: 'quarterly', dayOfMonth: 15 })).toBe('every quarter on day 15 at 09:00 (the Friday before if on a weekend)')
})
