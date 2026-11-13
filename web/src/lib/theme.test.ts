import { followSystemTheme } from './theme'

function fakeMedia(matches: boolean) {
  const listeners = new Set<() => void>()
  const mq = {
    matches,
    addEventListener: (_: string, l: () => void) => listeners.add(l),
    removeEventListener: (_: string, l: () => void) => listeners.delete(l),
  }
  vi.stubGlobal('matchMedia', vi.fn(() => mq))
  return { mq, change: (m: boolean) => { mq.matches = m; listeners.forEach((l) => l()) }, listeners }
}

afterEach(() => vi.unstubAllGlobals())

test('the theme follows the system light or dark mode as it changes', () => {
  const root = document.createElement('html')
  const media = fakeMedia(true)
  const stop = followSystemTheme(root)
  expect(window.matchMedia).toHaveBeenCalledWith('(prefers-color-scheme: dark)')
  expect(root).toHaveAttribute('data-bs-theme', 'dark')
  media.change(false)
  expect(root).toHaveAttribute('data-bs-theme', 'light')
  media.change(true)
  expect(root).toHaveAttribute('data-bs-theme', 'dark')
  stop()
  expect(media.listeners.size).toBe(0)
})

test('without matchMedia the app stays light', () => {
  vi.stubGlobal('matchMedia', undefined)
  const root = document.createElement('html')
  followSystemTheme(root)()
  expect(root).toHaveAttribute('data-bs-theme', 'light')
})
