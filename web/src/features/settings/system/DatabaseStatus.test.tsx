import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../../../App'
import { json, mockFetch } from '../../../test/fetch'
import { formatBytes, formatUptime } from './systemFormat'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
  window.history.pushState({}, '', '/')
})

const runtime = { goroutines: 21, heapBytes: 12 * 1024 * 1024, sysBytes: 40 * 1024 * 1024, gcRuns: 5, cpus: 4, goVersion: 'go1.25', os: 'linux', arch: 'amd64', hostname: 'cactusinv-7d9', startedAt: '2026-11-16T06:00:00Z' }
const pool = { maxOpen: 0, open: 3, inUse: 1, idle: 2, waitCount: 0, waitMs: 0 }
const status = {
  target: 'k3s', version: '1.34.0', build: '61', container: true, runtime, uptimeSeconds: 7260,
  database: {
    host: 'mariadb', port: 3306, name: 'cactusinv', user: 'cactusinv', error: '', version: '11.4.2-MariaDB', versionComment: 'MariaDB Server', uptimeSeconds: 90000,
    pingMs: 1.4, sizeBytes: 3 * 1024 * 1024, tables: 31, connections: 4, maxConnections: 151, maxUsedConnections: 9, queries: 12345, slowQueries: 0,
    charset: 'utf8mb4', collation: 'utf8mb4_unicode_ci', timeZone: 'SYSTEM (UTC)', pool,
    variables: [{ name: 'innodb_buffer_pool_size', value: '134217728' }, { name: 'max_connections', value: '151' }],
    status: [{ name: 'Threads_connected', value: '4' }],
  },
  services: [
    { name: 'cactusInv', role: 'This application', state: 'running', detail: 'version 1.34.0 (build 61) on cactusinv-7d9', latencyMs: 0 },
    { name: 'MariaDB', role: 'Database', state: 'running', detail: '11.4.2-MariaDB, up 1d 1h, 4 of 151 connections', latencyMs: 1.4 },
    { name: 'Mailpit', role: 'Test mailbox', state: 'unreachable', detail: 'mailpit:1025. does not answer', latencyMs: 0 },
  ],
  configuration: [{ name: 'CACTUSINV_DB_HOST', value: 'mariadb' }, { name: 'CACTUSINV_SMTP_HOST', value: '' }],
  secrets: [
    { key: 'db_password', description: 'The database password.', present: true },
    { key: 'smtp_password', description: 'The mail password.', present: false },
  ],
}

function routes(me: Record<string, unknown>, extra: Record<string, (req: Request) => Response | Promise<Response>> = {}) {
  mockFetch({
    'GET /api/v1/auth/me': json({ id: '1', email: 'admin', automationPaused: false, ...me }),
    'GET /api/v1/version': json({ version: '1.34.0', build: '61' }),
    'GET /api/v1/system/status': json(status),
    ...extra,
  })
}

test('shows the containers, the database, the configuration and every variable', async () => {
  routes({ administrator: true, permissions: [] })
  window.history.pushState({}, '', '/settings/database')
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'Database & containers' })).toBeInTheDocument()
  const services = screen.getByRole('region', { name: 'Containers and services' })
  expect(within(services).getByText('Mailpit')).toBeInTheDocument()
  expect(within(services).getByText('Unreachable')).toBeInTheDocument()
  expect(within(services).getAllByText('Running')).toHaveLength(2)
  const db = screen.getByRole('region', { name: 'Database' })
  expect(within(db).getByText('11.4.2-MariaDB (MariaDB Server)')).toBeInTheDocument()
  expect(within(db).getByText('4 of 151 (most at once: 9)')).toBeInTheDocument()
  expect(within(db).getByText('3.0 MB in 31 tables')).toBeInTheDocument()
  expect(screen.getByRole('table', { name: 'Application configuration' })).toHaveTextContent('CACTUSINV_DB_HOST')
  // The variables are behind a summary, filterable.
  await userEvent.click(screen.getByText('All MariaDB variables (2)', { selector: 'summary' }))
  await userEvent.type(screen.getByRole('searchbox', { name: 'Filter All MariaDB variables (2)' }), 'buffer')
  const vars = screen.getByRole('table', { name: 'All MariaDB variables (2)' })
  expect(within(vars).getByText('innodb_buffer_pool_size')).toBeInTheDocument()
  expect(within(vars).queryByText('max_connections')).not.toBeInTheDocument()
})

test('a secret is shown after your own password, and hidden after a minute', async () => {
  const bodies: unknown[] = []
  routes({ administrator: true, permissions: [] }, {
    'POST /api/v1/system/secrets/reveal': async (req) => {
      const b = (await req.json()) as { currentPassword: string }
      bodies.push(b)
      if (b.currentPassword !== 'my own password') return json({ type: 'x', title: 'Validation failed', status: 422, detail: 'currentPassword: is wrong', field: 'currentPassword' }, 422)()
      return json({ key: 'db_password', value: 'pw-of-the-db' })()
    },
  })
  window.history.pushState({}, '', '/settings/database')
  render(<App />)
  const secrets = await screen.findByRole('region', { name: 'Secrets' })
  expect(within(secrets).queryByRole('button', { name: 'View smtp_password' })).not.toBeInTheDocument()
  expect(within(secrets).getByText('Not set')).toBeInTheDocument()
  await userEvent.click(within(secrets).getByRole('button', { name: 'View db_password' }))
  const sheet = screen.getByRole('dialog', { name: 'View db_password' })
  await userEvent.type(within(sheet).getByLabelText('Your password'), 'wrong one')
  await userEvent.click(within(sheet).getByRole('button', { name: 'View' }))
  expect(await within(sheet).findByRole('alert')).toBeInTheDocument()
  await userEvent.clear(within(sheet).getByLabelText('Your password'))
  await userEvent.type(within(sheet).getByLabelText('Your password'), 'my own password')
  await userEvent.click(within(sheet).getByRole('button', { name: 'View' }))
  expect(await within(secrets).findByText('pw-of-the-db')).toBeInTheDocument()
  expect(bodies).toEqual([{ key: 'db_password', currentPassword: 'wrong one' }, { key: 'db_password', currentPassword: 'my own password' }])
  await userEvent.click(within(secrets).getByRole('button', { name: 'Hide db_password' }))
  expect(within(secrets).queryByText('pw-of-the-db')).not.toBeInTheDocument()
})

test('without the secrets permission there is no View button', async () => {
  routes({ administrator: false, permissions: ['settings.database.view'] })
  window.history.pushState({}, '', '/settings/database')
  render(<App />)
  const secrets = await screen.findByRole('region', { name: 'Secrets' })
  expect(within(secrets).queryByRole('button', { name: /^View / })).not.toBeInTheDocument()
  expect(within(secrets).getByText(/Only Administrators and members of Admin settings/)).toBeInTheDocument()
})

test('the dashboard shows health and performance to who may see the database page', async () => {
  routes({ administrator: true, permissions: [] }, {
    'GET /api/v1/dashboard': json({
      today: '2026-11-16', soonDays: 7, totals: [], upcomingEmails: [], upcomingRuns: [], recentInvoices: [], failedEmailCount: 0, months: ['2026-11'],
      invoiced: [], paid: [], topDebtors: [], activity: [], quoteStats: [], expiringQuotes: [], upcomingInvoices: [], dueSoon: [],
    }),
    'GET /api/v1/currencies': json({ items: [] }),
    'GET /api/v1/system/health': json({
      state: 'warning', uptimeSeconds: 3700, runtime, dbPingMs: 1.2, pool, outboxPending: 2, outboxFailed: 0,
      requests: { total: 1500, clientErrors: 3, serverErrors: 0, averageMs: 4.2, p95Ms: 18, lastMinute: 12 },
      checks: [{ name: 'Database', state: 'ok', detail: 'answers in 1 ms' }, { name: 'Backups', state: 'warning', detail: 'the newest backup is older than two days' }],
    }),
  })
  render(<App />)
  const card = await screen.findByRole('region', { name: 'Application health' })
  expect(within(card).getAllByText('Warning')).toHaveLength(2) // overall and the backups check
  expect(within(card).getByText('1,500')).toBeInTheDocument()
  expect(within(card).getByText('95% within 18 ms')).toBeInTheDocument()
  expect(within(card).getByText('the newest backup is older than two days')).toBeInTheDocument()
  expect(within(card).getByRole('link', { name: 'Details' })).toHaveAttribute('href', '/settings/database')
})

test('formatting', () => {
  expect(formatBytes(512)).toBe('512 B')
  expect(formatBytes(3 * 1024 * 1024 * 1024)).toBe('3.0 GB')
  expect(formatUptime(90000)).toBe('1d 1h')
  expect(formatUptime(3700)).toBe('1h 1m')
})
