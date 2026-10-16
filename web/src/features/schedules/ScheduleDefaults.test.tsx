import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { json, mockFetch } from '../../test/fetch'
import { ScheduleEditor } from './Schedules'

afterEach(() => vi.unstubAllGlobals())

const t = (subject: string, body: string, placeholders = ['{{customer_name}}', '{{business_name}}']) => ({ subject, body, placeholders })
const defaults = {
  invoice: t('Invoice {{invoice_number}} from {{business_name}}', 'Invoice body', ['{{customer_name}}', '{{invoice_number}}', '{{business_name}}']),
  unsent: t('Unsent subject', 'Unsent body'),
  outstanding: t('Reminder subject', 'Reminder body'),
  statement: t('Statement subject', 'Statement body', ['{{customer_name}}', '{{total}}', '{{date}}', '{{business_name}}']),
}

function serve(schedule?: unknown) {
  mockFetch({
    'GET /api/v1/schedules/defaults': json(defaults),
    'GET /api/v1/settings/business': json({ defaultNotes: 'Business disclaimer: third-party software.' }),
    'GET /api/v1/currencies': json({ items: [{ code: 'ZAR', minorUnits: 2 }] }),
    'GET /api/v1/customers': json({ items: [
      { id: 'c1', name: 'Acme', currency: 'ZAR', defaultNotes: '', disclaimerSource: 'business' },
      { id: 'c2', name: 'Own Co', currency: 'ZAR', defaultNotes: 'Own Co terms apply.', disclaimerSource: 'customer' },
    ], total: 2 }),
    'POST /api/v1/schedules/preview': json({ runs: [], timeZone: 'UTC' }),
    ...(schedule ? { 'GET /api/v1/schedules/s1': json({ schedule, upcoming: [], runs: [] }) } : {}),
  })
}

const editor = (path: string) =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/schedules/new" element={<ScheduleEditor />} />
        <Route path="/schedules/:id/edit" element={<ScheduleEditor />} />
      </Routes>
    </MemoryRouter>,
  )

const subject = () => screen.getByLabelText('E-mail subject')
const body = () => screen.getByLabelText('E-mail body')

test('a new schedule starts with the texts for its type and follows the type until edited', async () => {
  serve()
  editor('/schedules/new?customer=c1')
  expect(await screen.findByDisplayValue('Invoice {{invoice_number}} from {{business_name}}')).toBe(subject())
  expect(body()).toHaveValue('Invoice body')
  expect(screen.getByText(/Placeholders: \{\{customer_name\}\} \{\{invoice_number\}\} \{\{business_name\}\}/)).toBeInTheDocument()

  await userEvent.click(screen.getByRole('radio', { name: /E-mail a statement/ }))
  expect(subject()).toHaveValue('Statement subject')
  expect(body()).toHaveValue('Statement body')
  expect(screen.getByText(/Placeholders: .*\{\{date\}\}/)).toBeInTheDocument()

  await userEvent.clear(body())
  await userEvent.type(body(), 'My own words')
  await userEvent.click(screen.getByRole('radio', { name: /E-mail all outstanding invoices/ }))
  expect(subject()).toHaveValue('Reminder subject') // untouched: follows the type
  expect(body()).toHaveValue('My own words') // edited: kept
})

test('editing an existing schedule keeps its texts', async () => {
  serve({
    id: 's1', customerId: 'c1', name: 'Acme', kind: 'email', emailScope: 'statement', status: 'active', header: {}, notes: '', autoSend: true, emailDelayDays: 0,
    emailSubject: 'Saved subject', emailBody: '', version: 2, lines: [],
    recurrence: { frequency: 'monthly', interval: 1, dayOfMonth: 1, weekday: 1, sendTime: '09:00', timeZone: 'UTC', startDate: '2026-10-01' },
  })
  editor('/schedules/s1/edit')
  expect(await screen.findByDisplayValue('Saved subject')).toBe(subject())
  expect(body()).toHaveValue('')
})

test('invoice schedules show the disclaimer every invoice prints, with its source and where to edit it', async () => {
  serve()
  editor('/schedules/new?customer=c1')
  const note = await screen.findByTestId('schedule-disclaimer')
  expect(note).toHaveTextContent('Disclaimer printed on every invoice — the business disclaimer:')
  expect(await screen.findByText('Business disclaimer: third-party software.')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Edit the business disclaimer' })).toHaveAttribute('href', '/settings/business')
  expect(screen.getByLabelText('Invoice notes')).toHaveValue('') // never copied into the notes

  await userEvent.selectOptions(screen.getByLabelText('Customer'), 'c2')
  expect(screen.getByTestId('schedule-disclaimer')).toHaveTextContent("this customer's own:Own Co terms apply.")
  expect(screen.getByRole('link', { name: "Edit this customer's disclaimer" })).toHaveAttribute('href', '/customers/c2')

  await userEvent.click(screen.getByRole('radio', { name: /E-mail a statement/ }))
  expect(screen.queryByTestId('schedule-disclaimer')).toBeNull()
})
