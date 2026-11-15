import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../../../App'
import { json, mockFetch } from '../../../test/fetch'

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

const deployment = {
  target: 'dockerdesktop', platform: 'linux/amd64', backupDir: '/data/backups', invoicesDir: '/data/invoices',
  backupHostDir: '/Users/albie/cactusInv/data/backups', invoicesHostDir: '/Users/albie/cactusInv/data/invoices',
}
const saved = { sessionIdleMinutes: 0, sessionMaxHours: 168, backupHostDir: '', invoicesHostDir: '', version: 0, deployment, pendingFolders: false }

function routes(system: object, put?: (req: Request) => Promise<Response>, update?: object) {
  mockFetch({
    'GET /api/v1/auth/me': json({ administrator: true, permissions: [], id: '1', email: 'admin@example.com', automationPaused: false }),
    'GET /api/v1/version': json({ version: '1.11.0', build: '30' }),
    'GET /api/v1/settings/system': json(system),
    ...(put ? { 'PUT /api/v1/settings/system': put } : {}),
    ...(update ? { 'GET /api/v1/settings/system/update': json(update) } : {}),
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
  await userEvent.click(screen.getByRole('tab', { name: 'Sessions' }))
  const idle = screen.getByLabelText('Sign out after this many minutes without activity')
  await userEvent.clear(idle)
  await userEvent.type(idle, '30')
  const max = screen.getByLabelText('End every sign-in after this many hours')
  await userEvent.clear(max)
  await userEvent.type(max, '12')
  // Changes on one tab stay while another is open; Save stores them all.
  await userEvent.click(screen.getByRole('tab', { name: 'Folders' }))
  expect(screen.getByText('/Users/albie/cactusInv/data/backups')).toBeInTheDocument()
  await userEvent.type(screen.getByLabelText('Host folder for backups'), '/Volumes/NAS/backups')
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  expect(await screen.findByText('Saved. The new folders are used after ./deploy.sh --install --dockerdesktop.')).toBeInTheDocument()
  expect(body).toMatchObject({ sessionIdleMinutes: 30, sessionMaxHours: 12, backupHostDir: '/Volumes/NAS/backups', invoicesHostDir: '', version: 0 })
  expect(screen.getByRole('note')).toHaveTextContent('run ./deploy.sh --install --dockerdesktop to use it')
})

test('on Kubernetes the folders are volume claims and cannot be mapped here', async () => {
  routes({ ...saved, deployment: { ...deployment, target: 'k3s', backupHostDir: '', invoicesHostDir: '' } })
  window.history.pushState({}, '', '/settings/system?tab=folders')
  render(<App />)
  expect(await screen.findByText(/On k3s these folders are volume claims/)).toBeInTheDocument()
  expect(screen.queryByLabelText('Host folder for backups')).not.toBeInTheDocument()
  expect(screen.getByText('/data/backups')).toBeInTheDocument()
})

test('checks for updates on request and shows the upgrade command for this deployment', async () => {
  routes(saved, undefined, { current: '1.11.0', latest: '1.13.0', available: true, problem: '' })
  window.history.pushState({}, '', '/settings/system?tab=version')
  render(<App />)
  expect(await screen.findByText('cactusInv 1.11.0 (build 30)')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Check for updates' }))
  expect(await screen.findByText('Version 1.13.0 is available (this is 1.11.0).')).toBeInTheDocument()
  expect(screen.getByText('./deploy.sh --upgrade --dockerdesktop')).toBeInTheDocument()
})

test('says when this is the newest version, and why a check could not be made', async () => {
  routes({ ...saved, deployment: { ...deployment, target: 'k3s' } }, undefined, { current: '1.11.0', latest: '1.11.0', available: false, problem: '' })
  window.history.pushState({}, '', '/settings/system?tab=version')
  render(<App />)
  await userEvent.click(await screen.findByRole('button', { name: 'Check for updates' }))
  expect(await screen.findByText('You have the newest version (1.11.0).')).toBeInTheDocument()
  expect(screen.queryByText(/deploy.sh --upgrade/)).not.toBeInTheDocument()
  vi.unstubAllGlobals()

  routes(saved, undefined, { current: '1.11.0', latest: '', available: false, problem: 'The newest version could not be read.' })
  await userEvent.click(screen.getByRole('button', { name: 'Check for updates' }))
  expect(await screen.findByText('The newest version could not be read.')).toBeInTheDocument()
})

test('a standalone server installs the newest version itself after a second click, and reports a failure', async () => {
  let posts = 0
  mockFetch({
    'GET /api/v1/auth/me': json({ administrator: true, permissions: [], id: '1', email: 'admin@example.com', automationPaused: false }),
    'GET /api/v1/version': json({ version: '1.15.0', build: '37' }),
    'GET /api/v1/settings/system': json({ ...saved, deployment: { ...deployment, target: '' } }),
    'GET /api/v1/settings/system/update': json({ current: '1.15.0', latest: '1.16.0', available: true, problem: '', canInstall: true, installProblem: '' }),
    'POST /api/v1/settings/system/update': () => {
      posts++
      return json({ state: 'downloading', from: '1.15.0', to: '1.16.0', done: 0, total: 100, error: '', backup: 'cactusinv-x-manual.age' }, 202)()
    },
    'GET /api/v1/settings/system/update/progress': json({ state: 'failed', from: '1.15.0', to: '1.16.0', done: 100, total: 100, error: 'installing: SHA-256 does not match', backup: '' }),
  })
  window.history.pushState({}, '', '/settings/system?tab=version')
  render(<App />)
  await userEvent.click(await screen.findByRole('button', { name: 'Check for updates' }))
  await userEvent.click(await screen.findByRole('button', { name: 'Install and restart' }))
  expect(posts).toBe(0)
  await userEvent.click(screen.getByRole('button', { name: 'Yes, install 1.16.0 and restart' }))
  expect(await screen.findByText(/Downloading… Backup: cactusinv-x-manual.age./)).toBeInTheDocument()
  expect(posts).toBe(1)
  expect(screen.queryByText(/deploy.sh --upgrade/)).not.toBeInTheDocument()
  expect(await screen.findByText('The update failed and 1.15.0 keeps running: installing: SHA-256 does not match', {}, { timeout: 3000 })).toBeInTheDocument()
})

test('a container explains why it cannot install itself and gives the upgrade command', async () => {
  routes({ ...saved, deployment: { ...deployment, target: 'k3s' } }, undefined, {
    current: '1.15.0', latest: '1.16.0', available: true, problem: '', canInstall: false,
    installProblem: 'This cactusInv runs in a container: a new version would only last until the container restarts.',
  })
  window.history.pushState({}, '', '/settings/system?tab=version')
  render(<App />)
  await userEvent.click(await screen.findByRole('button', { name: 'Check for updates' }))
  expect(await screen.findByText(/runs in a container/)).toBeInTheDocument()
  expect(screen.getByText('./deploy.sh --upgrade --k3s')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Install and restart' })).not.toBeInTheDocument()
})

test('sets the external address links point at and switches HTTPS only on (M45)', async () => {
  let body: Record<string, unknown> = {}
  routes(saved, async (req) => {
    body = (await req.json()) as Record<string, unknown>
    return json({ ...body, version: 1, deployment, pendingFolders: false })()
  })
  window.history.pushState({}, '', '/settings/system')
  render(<App />)
  const address = await screen.findByLabelText('External address')
  expect(address).toHaveValue('')
  await userEvent.type(address, 'https://invoice.primesoft.co.za')
  const hsts = screen.getByLabelText('HTTPS only (HSTS)')
  expect(hsts).not.toBeChecked()
  expect(screen.getByText(/refuse to continue past a certificate warning/)).toBeInTheDocument()
  await userEvent.click(hsts)
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await vi.waitFor(() => expect(body).toMatchObject({ externalUrl: 'https://invoice.primesoft.co.za', hsts: true }))
})

test('System Settings are in tabs: Web settings, Sessions, Folders, Version; the open one is in the address', async () => {
  routes(saved, undefined, { current: '1.11.0', latest: '1.11.0', available: false, problem: '' })
  window.history.pushState({}, '', '/settings/system')
  render(<App />)
  const tabs = within(await screen.findByRole('tablist', { name: 'System Settings sections' })).getAllByRole('tab')
  expect(tabs.map((t) => t.textContent)).toEqual(['Web settings', 'Sessions', 'Folders', 'Version'])
  expect(screen.getByRole('tab', { name: 'Web settings' })).toHaveAttribute('aria-selected', 'true')
  expect(screen.getByLabelText('External address')).toBeInTheDocument()
  expect(screen.queryByLabelText('Sign out after this many minutes without activity')).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Check for updates' })).not.toBeInTheDocument()

  await userEvent.click(screen.getByRole('tab', { name: 'Version' }))
  expect(window.location.search).toBe('?tab=version')
  expect(await screen.findByRole('button', { name: 'Check for updates' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument() // nothing to save here
  expect(screen.queryByLabelText('External address')).not.toBeInTheDocument()

  await userEvent.click(screen.getByRole('tab', { name: 'Web settings' }))
  expect(window.location.search).toBe('')
  expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument()
})

test('Version lists the install command for every system and marks this server\'s', async () => {
  routes({ ...saved, deployment: { ...deployment, target: '', platform: 'linux/arm64' } })
  window.history.pushState({}, '', '/settings/system?tab=version')
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'Install or update by hand' })).toBeInTheDocument()
  for (const label of [
    'Linux and macOS · any processor', 'Windows (PowerShell) · any processor',
    'Linux · Intel/AMD 64-bit (amd64, x86_64)', 'Linux · ARM 64-bit (arm64, aarch64: Raspberry Pi 4/5, Graviton)',
    'macOS · Apple Silicon (M1–M4, darwin arm64)', 'macOS · Intel (darwin amd64)',
    'Windows · Intel/AMD 64-bit (amd64, x64)', 'Windows · ARM 64-bit (arm64: Snapdragon, Surface Pro X)',
  ]) {
    expect(screen.getByRole('button', { name: `Copy the command for ${label}` })).toBeInTheDocument()
  }
  // This server (linux/arm64): the Linux/macOS script and the Linux ARM file, nothing else.
  const marked = screen.getAllByText('This server').map((b) => b.closest('li')?.textContent ?? '')
  expect(marked).toHaveLength(2)
  expect(marked[0]).toContain('Linux and macOS · any processor')
  expect(marked[1]).toContain('Linux · ARM 64-bit')
  expect(screen.getByText(/releases\/latest\/download\/cactusinv-windows-arm64\.exe/)).toBeInTheDocument()
})

test('in a container the install commands are for another computer: none is marked', async () => {
  routes({ ...saved, deployment: { ...deployment, target: 'k3s' } })
  window.history.pushState({}, '', '/settings/system?tab=version')
  render(<App />)
  expect(await screen.findByText(/This installation is a container image/)).toBeInTheDocument()
  expect(screen.queryByText('This server')).not.toBeInTheDocument()
})
