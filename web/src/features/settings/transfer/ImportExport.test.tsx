import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../../../App'
import { json, mockFetch } from '../../../test/fetch'
import { parseSettingsFile, pick } from './ImportExport'

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

const file = {
  format: 'cactusinv-settings',
  formatVersion: 1,
  exportedAt: '2026-10-17T08:00:00Z',
  appVersion: '1.10.0',
  currencies: [{ code: 'ZAR', name: 'Rand', symbol: 'R', minorUnits: 2, active: true }],
  bankAccounts: [],
  customers: [
    { code: 'CD', name: 'Acme', email: 'a@acme.example', cc: [], attention: '', billingAddress: '', shippingName: '', shippingAddress: '', phone: '', taxNumber: '', currency: 'ZAR', paymentTerms: { kind: 'on_receipt', value: 0 }, bankAccountId: '', defaultNotes: '', active: true, version: 1 },
  ],
  business: { name: 'Example', addressLines: '', phone: '', email: '', taxNumber: '', numberFormat: '{code}/{seq}/{yy}', timeZone: 'UTC', defaultNotes: '', emailSubject: '', emailBody: '', vatPercent: '15', nextSequence: 10, version: 1 },
}

const counts = (created: number, updated: number, unchanged: number) => ({ created, updated, unchanged })
const none = counts(0, 0, 0)

function routes(extra: Record<string, (req: Request) => Response | Promise<Response>> = {}, administrator = true) {
  mockFetch({
    'GET /api/v1/auth/me': json({ administrator, permissions: administrator ? [] : ['settings.view', 'settings.business'], id: '1', email: 'admin@example.com', automationPaused: false }),
    'GET /api/v1/version': json({ version: '1.10.0', build: '30' }),
    ...extra,
  })
}

function choose(content: string, name = 'settings.json') {
  return userEvent.upload(screen.getByLabelText('Choose a settings file'), new File([content], name, { type: 'application/json' }))
}

test('exports the settings as a JSON file', async () => {
  routes({ 'GET /api/v1/settings/export': json(file) })
  const created: Blob[] = []
  vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: (b: Blob) => (created.push(b), 'blob:x'), revokeObjectURL: vi.fn() }))
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  window.history.pushState({}, '', '/settings/import-export')
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'Import/Export Settings' })).toBeInTheDocument()
  expect(screen.getByText(/password is never exported/)).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Export to a JSON file' }))
  expect(await screen.findByText(/Downloaded cactusinv-settings-\d{4}-\d{2}-\d{2}\.json: 1 customers, 1 currencies, 0 bank accounts/)).toBeInTheDocument()
  expect(click).toHaveBeenCalledOnce()
  const text = await new Promise<string>((resolve) => {
    const r = new FileReader()
    r.onload = () => resolve(String(r.result))
    r.readAsText(created[0]!)
  })
  expect(JSON.parse(text)).toEqual(file)
  click.mockRestore()
})

test('checks a file first, imports the chosen sections, and says what changed', async () => {
  const bodies: { apply: string | null; body: Record<string, unknown> }[] = []
  routes({
    'POST /api/v1/settings/import': async (req) => {
      const apply = new URL(req.url).searchParams.get('apply')
      bodies.push({ apply, body: (await req.json()) as Record<string, unknown> })
      if (apply === 'true') expect(req.headers.get('Idempotency-Key')).toBeTruthy()
      return json({
        applied: apply === 'true', business: none, emailServer: none, currencies: counts(0, 0, 1), bankAccounts: none,
        customers: counts(1, 0, 0), warnings: ['The next invoice sequence stays 20: the file has 10, and numbers already used are never given again.'],
      })()
    },
  })
  window.history.pushState({}, '', '/settings/import-export')
  render(<App />)
  await screen.findByRole('heading', { name: 'Import/Export Settings' })
  await choose(JSON.stringify(file))
  const table = await screen.findByRole('table', { name: 'What an import would change' })
  expect(within(table).getByRole('row', { name: 'Customers 1 0 0' })).toBeInTheDocument()
  expect(screen.getByText(/stays 20/)).toBeInTheDocument()
  expect(bodies[0]!.apply).toBe('false')
  expect(Object.keys(bodies[0]!.body).sort()).toEqual(['business', 'currencies', 'customers', 'format', 'formatVersion'])
  // Sections the file does not hold cannot be chosen.
  expect(screen.getByRole('checkbox', { name: /E-mail server/ })).toBeDisabled()

  // Leaving the business out checks again without it.
  await userEvent.click(screen.getByRole('checkbox', { name: /Business/ }))
  await screen.findByRole('table', { name: 'What an import would change' })
  expect(bodies).toHaveLength(2)
  expect(bodies[1]!.body).not.toHaveProperty('business')

  await userEvent.click(screen.getByRole('button', { name: 'Import' }))
  expect(await screen.findByText('Imported.')).toBeInTheDocument()
  expect(bodies[2]!.apply).toBe('true')
  expect(bodies[2]!.body).not.toHaveProperty('business')
  expect(screen.getByRole('table', { name: 'What was imported' })).toBeInTheDocument()
})

test('refuses a file that is not a settings file, and shows a check that fails', async () => {
  routes({
    'POST /api/v1/settings/import': json({ type: 'x', title: 'Validation failed', status: 422, detail: 'customers[CD].email: must be a valid e-mail address', field: 'customers[CD].email' }, 422),
  })
  window.history.pushState({}, '', '/settings/import-export')
  render(<App />)
  await screen.findByRole('heading', { name: 'Import/Export Settings' })
  await choose('not json')
  expect(await screen.findByRole('alert')).toHaveTextContent('The file is not JSON')
  await choose(JSON.stringify({ format: 'something', formatVersion: 1 }))
  expect(await screen.findByRole('alert')).toHaveTextContent('This is not a cactusInv settings file')
  await choose(JSON.stringify(file))
  expect(await screen.findByRole('alert')).toHaveTextContent('customers[CD].email: must be a valid e-mail address')
  expect(screen.queryByRole('button', { name: 'Import' })).not.toBeInTheDocument()
})

test('only administrators see the page', async () => {
  routes({}, false)
  window.history.pushState({}, '', '/settings/business')
  render(<App />)
  expect(await screen.findAllByRole('link', { name: /Business/ })).not.toHaveLength(0)
  expect(screen.queryByRole('link', { name: /Import\/Export Settings/ })).not.toBeInTheDocument()
})

test('pick and parse', () => {
  const f = parseSettingsFile(JSON.stringify(file))
  expect(pick(f, new Set(['customers']))).toEqual({ format: 'cactusinv-settings', formatVersion: 1, customers: file.customers })
  expect(() => parseSettingsFile(JSON.stringify({ ...file, formatVersion: 2 }))).toThrow(/newer cactusInv/)
})
