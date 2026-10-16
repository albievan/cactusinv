import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import type { Schemas } from '../../lib/api/client'
import { json, mockFetch } from '../../test/fetch'
import { ScheduleList } from './Schedules'

afterEach(() => vi.unstubAllGlobals())

type Schedule = Schemas['Schedule']

const base: Omit<Schedule, 'id' | 'name'> = {
  customerId: 'c1', kind: 'invoice', status: 'active', header: {}, notes: '', autoSend: true, emailDelayDays: 0, emailSubject: '', emailBody: '', version: 1, lines: [],
  recurrence: { frequency: 'weekly', interval: 1, dayOfMonth: 1, weekday: 5, sendTime: '09:00', timeZone: 'UTC', startDate: '2026-10-01' },
}

function schedules(): Schedule[] {
  return [
    { ...base, id: 's1', name: 'Acme monthly', lastRunAt: '2026-10-09T09:00:00Z', lastRunResult: 'ok', nextRunAt: '2026-10-16T09:00:00Z' },
    { ...base, id: 's2', name: 'Unsent reminder', kind: 'email', emailScope: 'unsent', status: 'paused' },
    { ...base, id: 's3', name: 'Overdue chaser', kind: 'email', emailScope: 'outstanding', lastRunResult: 'failed', failureCount: 2, lastError: 'no bank account', nextRunAt: '2026-10-16T10:00:00Z' },
    { ...base, id: 's4', name: 'Monthly statement', kind: 'email', emailScope: 'statement', status: 'ended' },
  ]
}

/** A server that records each action request and answers the list from its current state. */
function server(fail?: string) {
  const calls: string[] = []
  let items = schedules()
  const action = (what: string, apply: (s: Schedule) => Schedule | null) => (req: Request) => {
    const id = new URL(req.url).pathname.split('/')[4]!
    calls.push(`${what} ${id}`)
    if (fail === what) return json({ type: 'about:blank', title: 'Conflict', status: 409, detail: 'not now' }, 409)()
    items = items.flatMap((s) => (s.id === id ? (apply(s) ? [apply(s)!] : []) : [s]))
    return what === 'delete' ? new Response(null, { status: 204 }) : json({ schedule: items.find((s) => s.id === id), upcoming: [], runs: [] })()
  }
  const routes: Record<string, (req: Request) => Response | Promise<Response>> = {
    'GET /api/v1/schedules': () => json({ items, total: items.length })(),
    'GET /api/v1/customers': json({ items: [{ id: 'c1', name: 'Acme', currency: 'ZAR' }], total: 1 }),
  }
  for (const id of ['s1', 's2', 's3', 's4']) {
    routes[`POST /api/v1/schedules/${id}/pause`] = action('pause', (s) => ({ ...s, status: 'paused' }))
    routes[`POST /api/v1/schedules/${id}/resume`] = action('resume', (s) => ({ ...s, status: 'active' }))
    routes[`POST /api/v1/schedules/${id}/run-now`] = action('run', (s) => s)
    routes[`DELETE /api/v1/schedules/${id}`] = action('delete', () => null)
  }
  mockFetch(routes)
  return { calls }
}

const show = () => render(<MemoryRouter><ScheduleList /></MemoryRouter>)
const table = async () => within(await screen.findByRole('table'))
const row = async (name: string) => within((await table()).getByRole('link', { name }).closest('tr')!)

test('the table shows type, repeats, last and next run in the schedule zone, and status', async () => {
  server()
  show()
  const t = await table()
  expect(t.getAllByRole('columnheader').map((h) => h.textContent)).toEqual(['Name', 'Customer', 'Type', 'Repeats', 'Last run', 'Next run', 'Status', 'Actions'])
  const r1 = await row('Acme monthly')
  expect(r1.getByText('Acme')).toBeInTheDocument()
  expect(r1.getByText('Recurring invoice')).toBeInTheDocument()
  expect(r1.getByText('every week on Friday at 09:00')).toBeInTheDocument()
  expect(r1.getByText('Fri 9 Oct 2026, 09:00 UTC')).toBeInTheDocument()
  expect(r1.getByText('Fri 16 Oct 2026, 09:00 UTC')).toBeInTheDocument()
  expect(r1.getByText('active')).toBeInTheDocument()
  const r2 = await row('Unsent reminder')
  expect(r2.getByText('E-mail unsent invoices')).toBeInTheDocument()
  expect(r2.getByText('Never')).toBeInTheDocument()
  expect(r2.getByText('No upcoming run')).toBeInTheDocument()
  expect(r2.getByRole('button', { name: 'Resume Unsent reminder' })).toBeInTheDocument()
  expect(r2.getByRole('button', { name: 'Run Unsent reminder now' })).toBeDisabled()
  const r3 = await row('Overdue chaser')
  expect(r3.getByText('E-mail outstanding invoices')).toBeInTheDocument()
  expect(r3.getByText('Failed (2×): no bank account')).toBeInTheDocument()
  const r4 = await row('Monthly statement')
  expect(r4.getByText('Statement')).toBeInTheDocument()
  expect(r4.getByRole('button', { name: 'Pause Monthly statement' })).toBeDisabled()
  expect(r1.getByRole('link', { name: 'Edit Acme monthly' })).toHaveAttribute('href', '/schedules/s1/edit')
  expect(r1.getByRole('button', { name: 'Pause Acme monthly' })).toHaveAttribute('title', 'Pause')
  expect(screen.getByRole('link', { name: '+ New' })).toHaveAttribute('href', '/schedules/new')
})

test('pause and resume toggle the schedule and refresh the list', async () => {
  const { calls } = server()
  show()
  await userEvent.click((await row('Acme monthly')).getByRole('button', { name: 'Pause Acme monthly' }))
  expect(await screen.findByRole('status')).toHaveTextContent('Acme monthly paused')
  const resume = await (await row('Acme monthly')).findByRole('button', { name: 'Resume Acme monthly' })
  expect((await row('Acme monthly')).getByText('paused')).toBeInTheDocument()
  await userEvent.click(resume)
  expect(await screen.findByText('Acme monthly resumed')).toBeInTheDocument()
  expect(await (await row('Acme monthly')).findByRole('button', { name: 'Pause Acme monthly' })).toBeEnabled()
  expect(calls).toEqual(['pause s1', 'resume s1'])
})

test('run now and delete ask first; cancel sends nothing', async () => {
  const { calls } = server()
  show()
  await userEvent.click((await row('Acme monthly')).getByRole('button', { name: 'Run Acme monthly now' }))
  const dialog = screen.getByRole('dialog', { name: 'Run schedule now?' })
  expect(dialog).toHaveTextContent('“Acme monthly” raises a real invoice and e-mails it within a minute.')
  await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
  expect(screen.queryByRole('dialog')).toBeNull()
  await userEvent.click((await row('Overdue chaser')).getByRole('button', { name: 'Delete Overdue chaser' }))
  await userEvent.click(within(screen.getByRole('dialog', { name: 'Delete schedule?' })).getByRole('button', { name: 'Cancel' }))
  expect(calls).toEqual([])

  await userEvent.click((await row('Acme monthly')).getByRole('button', { name: 'Run Acme monthly now' }))
  await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Run now' }))
  expect(await screen.findByText('Acme monthly runs within a minute')).toBeInTheDocument()
  await userEvent.click((await row('Overdue chaser')).getByRole('button', { name: 'Delete Overdue chaser' }))
  expect(screen.getByRole('dialog')).toHaveTextContent('Invoices it created are kept.')
  await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete' }))
  expect(await screen.findByText('Overdue chaser deleted')).toBeInTheDocument()
  expect((await table()).queryByRole('link', { name: 'Overdue chaser' })).toBeNull()
  expect(calls).toEqual(['run s1', 'delete s3'])
})

test('a refused action shows the error and the buttons come back', async () => {
  const { calls } = server('pause')
  show()
  await userEvent.click((await row('Acme monthly')).getByRole('button', { name: 'Pause Acme monthly' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Conflict: not now')
  expect((await row('Acme monthly')).getByRole('button', { name: 'Pause Acme monthly' })).toBeEnabled()
  expect(calls).toEqual(['pause s1'])
})

test('a button is disabled while its request runs', async () => {
  server()
  let release: () => void = () => {}
  const held = new Promise<void>((r) => { release = r })
  const inner = globalThis.fetch as unknown as (r: Request) => Promise<Response>
  vi.stubGlobal('fetch', vi.fn(async (req: Request) => {
    if (req.method === 'POST') await held
    return inner(req)
  }))
  show()
  const pause = (await row('Acme monthly')).getByRole('button', { name: 'Pause Acme monthly' })
  await userEvent.click(pause)
  expect(pause).toBeDisabled()
  expect((await row('Acme monthly')).getByRole('button', { name: 'Delete Acme monthly' })).toBeDisabled()
  release()
  expect(await screen.findByText('Acme monthly paused')).toBeInTheDocument()
})

test('phones get compact cards with the same fields and 44 px actions', async () => {
  server()
  show()
  const cards = within(await screen.findByRole('list', { name: 'Schedules' }))
  expect(cards.getAllByRole('listitem')).toHaveLength(4)
  const card = within(cards.getByRole('link', { name: 'Acme monthly' }).closest('li')!)
  expect(card.getByText('Acme · Recurring invoice')).toBeInTheDocument()
  expect(card.getByText('every week on Friday at 09:00')).toBeInTheDocument()
  expect(card.getByText('Last run')).toBeInTheDocument()
  expect(card.getByText('Fri 9 Oct 2026, 09:00 UTC')).toBeInTheDocument()
  expect(card.getByText('Fri 16 Oct 2026, 09:00 UTC')).toBeInTheDocument()
  for (const name of ['Pause Acme monthly', 'Run Acme monthly now', 'Delete Acme monthly']) {
    expect(card.getByRole('button', { name })).toHaveClass('h-11', 'w-11')
  }
  expect(card.getByRole('link', { name: 'Edit Acme monthly' })).toHaveClass('h-11', 'w-11')
  expect(screen.getByRole('list', { name: 'Schedules' })).toHaveClass('md:hidden')
  expect(screen.getByRole('table').parentElement).toHaveClass('hidden', 'md:block')
})
