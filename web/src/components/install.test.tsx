import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../App'
import { json, mockFetch } from '../test/fetch'
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
