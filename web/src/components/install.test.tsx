import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../App'
import { json, mockFetch } from '../test/fetch'
import { containerPlatformOf, containerPlatforms, containerSteps } from './containerSteps'
import { fileCommands, InstallCommands, setupCommands, suits } from './install'

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

test('every operating system and processor has a file, named as the release names it', () => {
  const assets = fileCommands.map((c) => c.command.match(/download\/(cactusinv-\S+?)(?:\s|$)/)?.[1])
  expect(assets).toEqual([
    'cactusinv-linux-amd64', 'cactusinv-linux-arm64', 'cactusinv-darwin-arm64',
    'cactusinv-darwin-amd64', 'cactusinv-windows-amd64.exe', 'cactusinv-windows-arm64.exe',
  ])
  for (const p of ['linux/amd64', 'linux/arm64', 'darwin/amd64', 'darwin/arm64', 'windows/amd64', 'windows/arm64']) {
    expect(setupCommands.filter((c) => suits(c, p))).toHaveLength(1)
    expect(fileCommands.filter((c) => suits(c, p))).toHaveLength(1)
  }
  expect(setupCommands.some((c) => suits(c, ''))).toBe(false)
})

test('the copy button puts the command on the clipboard', async () => {
  const user = userEvent.setup()
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
  render(<InstallCommands />)
  await user.click(screen.getByRole('button', { name: 'Copy the command for Linux and macOS · any processor' }))
  expect(await navigator.clipboard.readText()).toBe('curl -fsSL https://raw.githubusercontent.com/albievan/cactusinv/main/setup.sh | sh')
  expect(await screen.findByText('Copied')).toBeInTheDocument()
})

test('the sign-in page offers the install commands below the source link', async () => {
  mockFetch({ 'GET /api/v1/auth/me': json({ type: 'u', title: 'Sign in required', status: 401 }, 401) })
  render(<App />)
  const summary = await screen.findByText('Install cactusInv on your own computer or server')
  const [source] = screen.getAllByText(/Free and open source/)
  expect((source?.compareDocumentPosition(summary) ?? 0) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  await userEvent.click(summary)
  expect(screen.getByRole('button', { name: 'Copy the command for Windows (PowerShell) · any processor' })).toBeVisible()
  expect(screen.getAllByRole('button', { name: /^Copy the command for / })).toHaveLength(8)
  expect(screen.queryByText('This server')).not.toBeInTheDocument()
})

test('every container platform has steps for each computer it lists, ending with its deploy.sh target', () => {
  const flags: Record<string, string> = { docker: '--dockerdesktop', podman: '--podman', k3s: '--k3s', k8s: '--k8s' }
  for (const p of containerPlatforms) {
    for (const os of p.os) {
      const steps = containerSteps(p.key, os)
      const commands = steps.flatMap((s) => s.commands?.map((c) => c.command) ?? [])
      expect(steps.length, `${p.key} on ${os}`).toBeGreaterThanOrEqual(6)
      expect(commands.some((c) => c.includes(`./deploy.sh --install ${flags[p.key]}`)), `${p.key} on ${os}`).toBe(true)
      expect(commands.some((c) => c.includes(`./deploy.sh --upgrade ${flags[p.key]}`)), `${p.key} on ${os}`).toBe(true)
      expect(commands.some((c) => c.includes('git clone https://github.com/albievan/cactusinv.git')), `${p.key} on ${os}`).toBe(true)
      // deploy.sh is bash: on Windows it runs in Ubuntu on WSL 2.
      if (os === 'windows') expect(commands).toContain('wsl --install -d Ubuntu-24.04')
    }
  }
  expect(containerPlatforms.map((p) => p.label)).toEqual(['Docker', 'Podman', 'k3s', 'Kubernetes'])
  expect(containerPlatformOf('dockerdesktop')).toBe('docker')
  expect(containerPlatformOf('podman')).toBe('podman')
  expect(containerPlatformOf('')).toBe('')
})

test('the Container tab shows the steps for the chosen platform and computer, each command copyable', async () => {
  const user = userEvent.setup()
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
  render(<InstallCommands />)
  expect(screen.getByRole('tab', { name: 'Standalone' })).toHaveAttribute('aria-selected', 'true')
  await user.click(screen.getByRole('tab', { name: 'Container' }))
  expect(screen.getByRole('tab', { name: 'Container' })).toHaveAttribute('aria-selected', 'true')
  expect(screen.queryByRole('button', { name: /^Copy the command for / })).not.toBeInTheDocument()

  await user.click(screen.getByRole('radio', { name: 'Podman' }))
  await user.click(screen.getByRole('radio', { name: 'Windows' }))
  expect(screen.getByRole('list', { name: 'Steps: Podman on Windows' })).toBeInTheDocument()
  expect(screen.queryByRole('radio', { name: 'macOS' })).not.toBeInTheDocument()
  expect(screen.getByText('Install Ubuntu on WSL 2')).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Copy: Install' }))
  expect(await navigator.clipboard.readText()).toBe('./deploy.sh --install --podman')

  await user.click(screen.getByRole('radio', { name: 'k3s' }))
  await user.click(screen.getByRole('radio', { name: 'Linux' }))
  expect(screen.getByRole('list', { name: 'Steps: k3s on Linux' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Copy: Install' }))
  expect(await navigator.clipboard.readText()).toBe('./deploy.sh --install --k3s')
})

test('an installation made with deploy.sh opens the Container tab at its platform', () => {
  render(<InstallCommands target="podman" />)
  expect(screen.getByRole('tab', { name: 'Container' })).toHaveAttribute('aria-selected', 'true')
  expect(screen.getByRole('radio', { name: 'Podman' })).toBeChecked()
})
