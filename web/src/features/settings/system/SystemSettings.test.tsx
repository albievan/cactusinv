import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../../../App'
import { json, mockFetch } from '../../../test/fetch'

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

const deployment = {
  target: 'dockerdesktop', backupDir: '/data/backups', invoicesDir: '/data/invoices',
  backupHostDir: '/Users/albie/cactusInv/data/backups', invoicesHostDir: '/Users/albie/cactusInv/data/invoices',
}
const saved = { sessionIdleMinutes: 0, sessionMaxHours: 168, backupHostDir: '', invoicesHostDir: '', version: 0, deployment, pendingFolders: false }

function routes(system: object, put?: (req: Request) => Promise<Response>) {
  mockFetch({
    'GET /api/v1/auth/me': json({ administrator: true, permissions: [], id: '1', email: 'admin@example.com', automationPaused: false }),
    'GET /api/v1/version': json({ version: '1.11.0', build: '30' }),
    'GET /api/v1/settings/system': json(system),
    ...(put ? { 'PUT /api/v1/settings/system': put } : {}),
  })
}

test('sets the session timeouts and maps a folder, applied at the next deploy', async () => {
  let body: Record<string, unknown> = {}
  routes(saved, async (req) => {
    body = (await req.json()) as Record<string, unknown>
    return json({ ...body, version: 1, deployment, pendingFolders: true })()
  })
  window.history.pushState({}, '', '/settings/system')
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'System Settings' })).toBeInTheDocument()
  expect(screen.getByText('/Users/albie/cactusInv/data/backups')).toBeInTheDocument()
  const idle = screen.getByLabelText('Sign out after this many minutes without activity')
  await userEvent.clear(idle)
  await userEvent.type(idle, '30')
  const max = screen.getByLabelText('End every sign-in after this many hours')
  await userEvent.clear(max)
  await userEvent.type(max, '12')
  await userEvent.type(screen.getByLabelText('Host folder for backups'), '/Volumes/NAS/backups')
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  expect(await screen.findByText('Saved. The new folders are used after ./deploy.sh --install --dockerdesktop.')).toBeInTheDocument()
  expect(body).toMatchObject({ sessionIdleMinutes: 30, sessionMaxHours: 12, backupHostDir: '/Volumes/NAS/backups', invoicesHostDir: '', version: 0 })
  expect(screen.getByRole('note')).toHaveTextContent('run ./deploy.sh --install --dockerdesktop to use it')
})

test('on Kubernetes the folders are volume claims and cannot be mapped here', async () => {
  routes({ ...saved, deployment: { ...deployment, target: 'k3s', backupHostDir: '', invoicesHostDir: '' } })
  window.history.pushState({}, '', '/settings/system')
  render(<App />)
  expect(await screen.findByText(/On k3s these folders are volume claims/)).toBeInTheDocument()
  expect(screen.queryByLabelText('Host folder for backups')).not.toBeInTheDocument()
  expect(screen.getByText('/data/backups')).toBeInTheDocument()
})
