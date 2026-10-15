import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../../../App'
import { json, mockFetch } from '../../../test/fetch'
import { formatSize } from './Backup'

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

const overview = {
  keyConfigured: true, times: ['02:00'], timeZone: 'Africa/Johannesburg', nextRun: '2026-10-16T00:00:00Z', retentionDays: 14,
  lastRun: { slot: '2026-10-15T00:00:00Z', name: 'cactusinv-20261015T000000Z-s14-scheduled.age', result: 'ok', error: '', startedAt: '2026-10-15T00:00:00Z' },
  files: [
    { name: 'cactusinv-20261015T000000Z-s14-scheduled.age', size: 2048, createdAt: '2026-10-15T00:00:00Z', schema: 14, kind: 'scheduled', legacy: false },
    { name: 'cactusinv-20261001-020000.sql.gz', size: 99, createdAt: '2026-10-01T00:00:00Z', schema: 0, kind: 'legacy', legacy: true },
  ],
  maxUploadBytes: 1024 * 1024,
}

const preview = {
  id: 'r1', expiresAt: '2026-10-15T10:30:00Z', source: overview.files[0]!.name, sha256: 'ab', createdAt: '2026-10-15T00:00:00Z', createdBy: 'Backup scheduler',
  appVersion: '1.5.0', schemaFrom: 14, schemaTo: 14, tables: [{ name: 'customers', backup: 3, live: 4 }],
  audit: { mode: 'keep_current', count: 0, summary: 'keep the current audit trail', backupBroken: false },
  liveAuditOk: true, invoicesSince: 2, canSignIn: true, smtpPasswordCleared: false, safetyBackup: true,
  warnings: ['2 invoices were created after this backup was taken; restoring removes them'],
}

function routes(extra: Record<string, (req: Request) => Response | Promise<Response>> = {}, paused = false) {
  mockFetch({
    'GET /api/v1/auth/me': json({ id: '1', email: 'admin@example.com', automationPaused: paused }),
    'GET /api/v1/version': json({ version: '1.5.0', build: '10' }),
    'GET /api/v1/backups': json(overview),
    'GET /api/v1/restores/current': json({ type: 'x', title: 'Not found', status: 404 }, 404),
    ...extra,
  })
}

test('shows the schedule and stored backups, and backs up now', async () => {
  let created = 0
  routes({
    'POST /api/v1/backups': (req) => {
      created++
      expect(req.headers.get('Idempotency-Key')).toBeTruthy()
      return json({ name: 'cactusinv-20261015T080000Z-s14-manual.age', size: 10, sha256: 'x', schema: 14 }, 201)()
    },
  })
  window.history.pushState({}, '', '/settings/backup')
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'Backup & restore' })).toBeInTheDocument()
  expect(screen.getByText(/Daily at/)).toHaveTextContent('Daily at 02:00 (Africa/Johannesburg), kept for 14 days.')
  const table = screen.getByRole('table', { name: 'Stored backups' })
  expect(within(table).getByText('Scheduled')).toBeInTheDocument()
  expect(within(table).getByText('Old plaintext dump')).toBeInTheDocument()
  expect(within(table).getAllByRole('button', { name: /Restore/ })).toHaveLength(1) // not the legacy dump
  await userEvent.click(screen.getByRole('button', { name: 'Back up now' }))
  expect(await screen.findByText(/Backup stored: cactusinv-20261015T080000Z-s14-manual.age/)).toBeInTheDocument()
  expect(created).toBe(1)
})

test('without the server key it says so and only offers downloads', async () => {
  mockFetch({
    'GET /api/v1/auth/me': json({ id: '1', email: 'a', automationPaused: false }),
    'GET /api/v1/version': json({ version: '1.5.0', build: '10' }),
    'GET /api/v1/backups': json({ ...overview, keyConfigured: false, files: [], lastRun: undefined, nextRun: undefined }),
  })
  window.history.pushState({}, '', '/settings/backup')
  render(<App />)
  expect(await screen.findByText(/backup_key\) is not set up/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Back up now' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Download a backup' })).toBeEnabled()
  expect(screen.getByText('No backups are stored yet.')).toBeInTheDocument()
})

test('downloads a backup encrypted with a confirmed passphrase', async () => {
  let body: unknown
  routes({
    'POST /api/v1/backups/export': async (req) => {
      body = await req.json()
      return new Response(new Blob(['age-encrypted']), { status: 200, headers: { 'Content-Type': 'application/octet-stream', 'Content-Disposition': 'attachment; filename="cactusinv-x.age"' } })
    },
  })
  const create = vi.fn(() => 'blob:x')
  vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: create, revokeObjectURL: vi.fn() }))
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  window.history.pushState({}, '', '/settings/backup')
  render(<App />)
  await userEvent.click(await screen.findByRole('button', { name: 'Download a backup' }))
  const dialog = screen.getByRole('dialog', { name: 'Download a backup' })
  await userEvent.type(within(dialog).getByLabelText('Passphrase'), 'too short')
  await userEvent.type(within(dialog).getByLabelText('Passphrase again'), 'too short')
  await userEvent.click(within(dialog).getByRole('button', { name: 'Download' }))
  expect(within(dialog).getByRole('alert')).toHaveTextContent('at least 16 characters')
  await userEvent.clear(within(dialog).getByLabelText('Passphrase'))
  await userEvent.clear(within(dialog).getByLabelText('Passphrase again'))
  await userEvent.type(within(dialog).getByLabelText('Passphrase'), 'a long enough passphrase')
  await userEvent.type(within(dialog).getByLabelText('Passphrase again'), 'a long enough passphrasE')
  await userEvent.click(within(dialog).getByRole('button', { name: 'Download' }))
  expect(within(dialog).getByRole('alert')).toHaveTextContent('not the same')
  await userEvent.clear(within(dialog).getByLabelText('Passphrase again'))
  await userEvent.type(within(dialog).getByLabelText('Passphrase again'), 'a long enough passphrase')
  await userEvent.click(within(dialog).getByRole('button', { name: 'Download' }))
  expect(await within(dialog).findByText(/Downloaded cactusinv-x.age/)).toBeInTheDocument()
  expect(body).toEqual({ passphrase: 'a long enough passphrase' })
  expect(create).toHaveBeenCalled()
  expect(click).toHaveBeenCalledTimes(1)
  click.mockRestore()
})

test('restores a stored backup: preview with warnings, then password and RESTORE; signed out after', async () => {
  let applied: unknown
  let signedOut = false
  routes({
    'GET /api/v1/auth/me': () =>
      signedOut ? json({ type: 'u', title: 'Sign in required', status: 401 }, 401)() : json({ id: '1', email: 'admin@example.com', automationPaused: false })(),
    'POST /api/v1/restores': json(preview),
    'POST /api/v1/restores/r1/apply': async (req) => {
      applied = await req.json()
      signedOut = true
      return json({ source: preview.source, audit: preview.audit, sequenceKept: true, smtpPasswordCleared: false, safetyBackup: 'cactusinv-20261015T100000Z-s14-prerestore.age' })()
    },
  })
  window.history.pushState({}, '', '/settings/backup')
  render(<App />)
  await userEvent.click(await screen.findByRole('button', { name: `Restore ${overview.files[0]!.name}` }))
  const stage = screen.getByRole('dialog', { name: 'Check a backup' })
  await userEvent.click(within(stage).getByRole('button', { name: 'Check backup' }))
  const card = await screen.findByLabelText('Restore preview')
  expect(within(card).getByRole('list', { name: 'Warnings' })).toHaveTextContent('2 invoices were created after this backup')
  await userEvent.click(within(card).getByRole('button', { name: 'Restore…' }))
  const confirm = screen.getByRole('dialog', { name: 'Replace all data with this backup?' })
  const go = within(confirm).getByRole('button', { name: 'Restore' })
  expect(within(confirm).getByText(/saved first as a stored backup/)).toBeInTheDocument()
  await userEvent.type(within(confirm).getByLabelText('Your password'), 'correct horse battery')
  await userEvent.type(within(confirm).getByLabelText('Type RESTORE to confirm'), 'restore')
  expect(go).toBeDisabled()
  await userEvent.clear(within(confirm).getByLabelText('Type RESTORE to confirm'))
  await userEvent.type(within(confirm).getByLabelText('Type RESTORE to confirm'), 'RESTORE')
  await userEvent.click(go)
  expect(await screen.findByText(/The backup was restored and everyone was signed out/)).toHaveTextContent('cactusinv-20261015T100000Z-s14-prerestore.age')
  expect(screen.getByRole('heading', { name: 'Sign in' })).toBeInTheDocument()
  expect(applied).toEqual({ currentPassword: 'correct horse battery', confirmation: 'RESTORE' })
})

test('restores from an uploaded file with its passphrase, and can discard the stage', async () => {
  let uploaded = ''
  let staged: unknown
  let discarded = false
  routes({
    'POST /api/v1/backups/uploads': async (req) => {
      uploaded = req.headers.get('Content-Type') ?? ''
      return json({ name: 'cactusinv-20261015T090000Z-s0-upload.age', size: 5, createdAt: '2026-10-15T09:00:00Z', schema: 0, kind: 'upload', legacy: false }, 201)()
    },
    'POST /api/v1/restores': async (req) => {
      staged = await req.json()
      return json(preview)()
    },
    'DELETE /api/v1/restores/r1': () => {
      discarded = true
      return new Response(null, { status: 204 })
    },
  })
  window.history.pushState({}, '', '/settings/backup')
  render(<App />)
  const input = await screen.findByLabelText('Restore from file')
  await userEvent.upload(input, new File(['age'], 'backup.age', { type: 'application/octet-stream' }))
  const stage = await screen.findByRole('dialog', { name: 'Check a backup' })
  await userEvent.type(within(stage).getByLabelText('Passphrase'), 'a long enough passphrase')
  await userEvent.click(within(stage).getByRole('button', { name: 'Check backup' }))
  const card = await screen.findByLabelText('Restore preview')
  expect(uploaded).toBe('application/octet-stream')
  expect(staged).toEqual({ backup: 'cactusinv-20261015T090000Z-s0-upload.age', passphrase: 'a long enough passphrase' })
  await userEvent.click(within(card).getByRole('button', { name: 'Discard' }))
  expect(await screen.findByRole('table', { name: 'Stored backups' })).toBeInTheDocument()
  expect(discarded).toBe(true)
})

test('a file larger than the limit is refused before it is sent', async () => {
  routes({ 'POST /api/v1/backups/uploads': () => { throw new Error('must not upload') } })
  window.history.pushState({}, '', '/settings/backup')
  render(<App />)
  const big = new File([new Uint8Array(2 * 1024 * 1024)], 'big.age')
  await userEvent.upload(await screen.findByLabelText('Restore from file'), big)
  expect(await screen.findByText('The file is larger than 1.0 MB.')).toBeInTheDocument()
})

test('the paused banner shows on every page after a restore and resumes sending', async () => {
  let paused = true
  mockFetch({
    'GET /api/v1/auth/me': () => json({ id: '1', email: 'a', automationPaused: paused })(),
    'GET /api/v1/version': json({ version: '1.5.0', build: '10' }),
    'GET /api/v1/backups': json(overview),
    'POST /api/v1/automation/resume': () => {
      paused = false
      return new Response(null, { status: 204 })
    },
  })
  window.history.pushState({}, '', '/settings/backup')
  render(<App />)
  expect(await screen.findByText('Automatic sending is paused after a restore.')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Resume automatic sending' }))
  await screen.findByRole('heading', { name: 'Backup & restore' })
  expect(screen.queryByText('Automatic sending is paused after a restore.')).not.toBeInTheDocument()
})

test('formatSize', () => {
  expect(formatSize(512)).toBe('512 B')
  expect(formatSize(2048)).toBe('2.0 KB')
  expect(formatSize(3 * 1024 * 1024)).toBe('3.0 MB')
})

test('a download that breaks off is reported as failed', async () => {
  routes({
    'POST /api/v1/backups/export': () =>
      new Response(new ReadableStream({ start(c) { c.enqueue(new Uint8Array([1, 2, 3])); c.error(new Error('connection reset')) } }), {
        status: 200, headers: { 'Content-Type': 'application/octet-stream', 'Content-Disposition': 'attachment; filename="x.age"' },
      }),
  })
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  window.history.pushState({}, '', '/settings/backup')
  render(<App />)
  await userEvent.click(await screen.findByRole('button', { name: 'Download a backup' }))
  const dialog = screen.getByRole('dialog', { name: 'Download a backup' })
  await userEvent.type(within(dialog).getByLabelText('Passphrase'), 'a long enough passphrase')
  await userEvent.type(within(dialog).getByLabelText('Passphrase again'), 'a long enough passphrase')
  await userEvent.click(within(dialog).getByRole('button', { name: 'Download' }))
  expect(await within(dialog).findByRole('alert')).toBeInTheDocument()
  expect(within(dialog).queryByText(/Downloaded/)).not.toBeInTheDocument()
  expect(click).not.toHaveBeenCalled()
  click.mockRestore()
})

test('a failed scheduled backup shows its reason in words', async () => {
  mockFetch({
    'GET /api/v1/auth/me': json({ id: '1', email: 'a', automationPaused: false }),
    'GET /api/v1/version': json({ version: '1.5.0', build: '10' }),
    'GET /api/v1/backups': json({ ...overview, lastRun: { ...overview.lastRun, result: 'failed', error: 'disk' } }),
    'GET /api/v1/restores/current': json({ type: 'x', title: 'Not found', status: 404 }, 404),
  })
  window.history.pushState({}, '', '/settings/backup')
  render(<App />)
  expect(await screen.findByText(/Reason: the backups folder could not be written/)).toBeInTheDocument()
})
