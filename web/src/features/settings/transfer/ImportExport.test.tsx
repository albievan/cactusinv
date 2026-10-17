import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../../../App'
import { json, mockFetch } from '../../../test/fetch'
import { parseSettingsFile } from './ImportExport'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  window.history.pushState({}, '', '/')
})

const file = {
  format: 'cactusinv-settings',
  formatVersion: 1,
  exportedAt: '2026-10-17T08:00:00Z',
  appVersion: '1.10.0',
  currencies: [{ code: 'ZAR', name: 'Rand', symbol: 'R', minorUnits: 2, active: true }],
  bankAccounts: [],
  products: [{ sku: 'SUP', name: 'Support', description: '', unit: '', prices: [{ currency: 'ZAR', unitPrice: '100.00' }], active: true, version: 1 }],
  customers: [
    { code: 'CD', name: 'Acme', email: 'a@acme.example', cc: [], attention: '', billingAddress: '', shippingName: '', shippingAddress: '', phone: '', taxNumber: '', currency: 'ZAR', paymentTerms: { kind: 'on_receipt', value: 0 }, bankAccountId: '', defaultNotes: '', active: true, version: 1 },
  ],
  schedules: [],
  business: { name: 'Example', addressLines: '', phone: '', email: '', taxNumber: '', numberFormat: '{code}/{seq}/{yy}', timeZone: 'UTC', defaultNotes: '', emailSubject: '', emailBody: '', vatPercent: '15', nextSequence: 10, version: 1 },
}
const sealed = { format: 'cactusinv-settings', formatVersion: 1, exportedAt: '2026-10-17T08:00:00Z', appVersion: '1.10.0', encrypted: 'ZmFrZQ==' }

const counts = (created: number, updated: number, unchanged: number) => ({ created, updated, unchanged })
const none = counts(0, 0, 0)
const result = (applied: boolean) => ({
  applied, business: counts(0, 1, 0), emailServer: none, currencies: counts(0, 0, 1), bankAccounts: none, products: counts(1, 0, 0),
  customers: counts(1, 0, 0), schedules: none,
  warnings: ['The next invoice sequence stays 20: the file has 10, and numbers already used are never given again.'],
})

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

function readBlob(b: Blob): Promise<string> {
  return new Promise((resolve) => {
    const r = new FileReader()
    r.onload = () => resolve(String(r.result))
    r.readAsText(b)
  })
}

async function openPage() {
  window.history.pushState({}, '', '/settings/import-export')
  render(<App />)
  await screen.findByRole('heading', { name: 'Import/Export Settings' })
}

test('exports with a passphrase by default, or as readable JSON', async () => {
  const asked: Record<string, unknown>[] = []
  routes({
    'POST /api/v1/settings/export': async (req) => {
      const body = (await req.json()) as Record<string, unknown>
      asked.push(body)
      return json(body.passphrase ? sealed : file)()
    },
  })
  const created: Blob[] = []
  vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: (b: Blob) => (created.push(b), 'blob:x'), revokeObjectURL: vi.fn() }))
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  await openPage()
  expect(screen.getByText(/password is never exported/)).toBeInTheDocument()
  const exportButton = screen.getByRole('button', { name: 'Export to a JSON file' })

  await userEvent.type(screen.getByLabelText('Passphrase'), 'too short')
  await userEvent.click(exportButton)
  expect(await screen.findByRole('alert')).toHaveTextContent('at least 16 characters')
  await userEvent.type(screen.getByLabelText('Passphrase'), ' but now long enough')
  await userEvent.type(screen.getByLabelText('Passphrase again'), 'too short but now long enough')
  await userEvent.click(exportButton)
  expect(await screen.findByText(/protected with your passphrase/)).toBeInTheDocument()
  expect(asked[0]).toEqual({ passphrase: 'too short but now long enough' })
  expect(JSON.parse(await readBlob(created[0]!))).toEqual(sealed)

  await userEvent.click(screen.getByRole('checkbox', { name: /Without a passphrase/ }))
  expect(screen.queryByLabelText('Passphrase')).not.toBeInTheDocument()
  await userEvent.click(exportButton)
  expect(await screen.findByText(/1 customers, 1 products, 0 schedules, 1 currencies and 0 bank accounts/)).toBeInTheDocument()
  expect(asked[1]).toEqual({})
  expect(JSON.parse(await readBlob(created[1]!))).toEqual(file)
  expect(click).toHaveBeenCalledTimes(2)
})

test('checks a plain file first, imports the chosen sections, and says what changed', async () => {
  const bodies: { apply: string | null; body: { file: unknown; sections?: string[]; passphrase?: string } }[] = []
  routes({
    'POST /api/v1/settings/import': async (req) => {
      const apply = new URL(req.url).searchParams.get('apply')
      bodies.push({ apply, body: (await req.json()) as never })
      return json(result(apply === 'true'))()
    },
  })
  await openPage()
  await choose(JSON.stringify(file))
  const table = await screen.findByRole('table', { name: 'What an import would change' })
  expect(within(table).getByRole('row', { name: 'Customers 1 0 0' })).toBeInTheDocument()
  expect(screen.getByText(/stays 20/)).toBeInTheDocument()
  expect(bodies[0]!.apply).toBe('false')
  expect(bodies[0]!.body.file).toEqual(file)
  expect(bodies[0]!.body.sections).toEqual(['currencies', 'products', 'customers', 'business'])
  // Sections the file does not hold cannot be chosen.
  expect(screen.getByRole('checkbox', { name: /E-mail server/ })).toBeDisabled()
  expect(screen.getByRole('checkbox', { name: /Schedules/ })).toBeDisabled()

  // Leaving the business out checks again without it.
  await userEvent.click(screen.getByRole('checkbox', { name: /Business/ }))
  await screen.findByRole('table', { name: 'What an import would change' })
  expect(bodies).toHaveLength(2)
  expect(bodies[1]!.body.sections).not.toContain('business')

  await userEvent.click(screen.getByRole('button', { name: 'Import' }))
  expect(await screen.findByText('Imported.')).toBeInTheDocument()
  expect(bodies[2]!.apply).toBe('true')
  expect(bodies[2]!.body.sections).not.toContain('business')
  expect(screen.getByRole('table', { name: 'What was imported' })).toBeInTheDocument()
})

test('opens a protected file with its passphrase', async () => {
  const bodies: { passphrase?: string; sections?: string[] }[] = []
  routes({
    'POST /api/v1/settings/import': async (req) => {
      const body = (await req.json()) as { passphrase?: string; sections?: string[] }
      bodies.push(body)
      if (body.passphrase !== 'the right passphrase!') {
        return json({ type: 'x', title: 'Validation failed', status: 422, detail: 'passphrase: is wrong, or the file is damaged', field: 'passphrase' }, 422)()
      }
      return json(result(false))()
    },
  })
  await openPage()
  await choose(JSON.stringify(sealed))
  expect(await screen.findByLabelText("The file's passphrase")).toBeInTheDocument()
  expect(screen.queryByRole('checkbox', { name: /Customers/ })).not.toBeInTheDocument()
  await userEvent.type(screen.getByLabelText("The file's passphrase"), 'wrong')
  await userEvent.click(screen.getByRole('button', { name: 'Open' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('passphrase: is wrong')
  await userEvent.clear(screen.getByLabelText("The file's passphrase"))
  await userEvent.type(screen.getByLabelText("The file's passphrase"), 'the right passphrase!')
  await userEvent.click(screen.getByRole('button', { name: 'Open' }))
  // Opened: what the file holds comes from the check of all of it.
  expect(await screen.findByRole('checkbox', { name: /Customers \(1\)/ })).toBeChecked()
  expect(screen.getByRole('checkbox', { name: /Schedules/ })).toBeDisabled()
  await screen.findByRole('table', { name: 'What an import would change' })
  expect(bodies[1]!.sections).toBeUndefined()
  expect(bodies[2]).toMatchObject({ passphrase: 'the right passphrase!', sections: ['currencies', 'products', 'customers', 'business'] })
})

test('refuses a file that is not a settings file, and shows a check that fails', async () => {
  routes({
    'POST /api/v1/settings/import': json({ type: 'x', title: 'Validation failed', status: 422, detail: 'customers[CD].email: must be a valid e-mail address', field: 'customers[CD].email' }, 422),
  })
  await openPage()
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

test('parse', () => {
  expect(parseSettingsFile(JSON.stringify(file)).customers).toHaveLength(1)
  expect(() => parseSettingsFile(JSON.stringify({ ...file, formatVersion: 2 }))).toThrow(/newer cactusInv/)
})
