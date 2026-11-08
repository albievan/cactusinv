// @vitest-environment node
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

// The app has one light theme (PLAN 13.1). Bootstrap 5.3 switches colour modes only on
// data-bs-theme (its default $color-mode-type: data, never media), and index.html pins it to
// light, so a phone in dark mode keeps light-theme text on light backgrounds.
const src = fileURLToPath(new URL('.', import.meta.url))
const sources = () => (readdirSync(src, { recursive: true }) as string[]).filter((f) => /\.(ts|tsx|scss|css)$/.test(f) && !/\.d\.ts$|\.test\.tsx?$/.test(f))
const dark = ['dark', ':'].join('') // spelled out so this file does not match itself

test('the page is pinned to the light colour mode', () => {
  const html = readFileSync(join(src, '..', 'index.html'), 'utf8')
  expect(html).toMatch(/<html[^>]*data-bs-theme="light"/)
})

test('nothing switches to the dark mode or follows the system setting', () => {
  const files = sources()
  expect(files.length).toBeGreaterThan(10)
  const offenders = files.filter((f) => {
    const text = readFileSync(join(src, f), 'utf8')
    return /data-bs-theme=["']dark|\$color-mode-type:\s*media|prefers-color-scheme/.test(text) || new RegExp(`(?<![\\w-])${dark}`).test(text)
  })
  expect(offenders).toEqual([])
})
