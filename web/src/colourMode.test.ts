// @vitest-environment node
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

// The colour mode follows the system's light or dark mode, with no toggle (M38): only
// lib/theme.ts reads the system setting and sets data-bs-theme, and nothing stores a choice.
// index.html starts light, the mode the page has before the script runs.
const src = fileURLToPath(new URL('.', import.meta.url))
const sources = () => (readdirSync(src, { recursive: true }) as string[]).filter((f) => /\.(ts|tsx|scss|css)$/.test(f) && !/\.d\.ts$|\.test\.tsx?$/.test(f))

test('the page starts in the light colour mode', () => {
  const html = readFileSync(join(src, '..', 'index.html'), 'utf8')
  expect(html).toMatch(/<html[^>]*data-bs-theme="light"/)
})

test('only lib/theme.ts follows the system setting, and no theme choice is stored', () => {
  const files = sources()
  expect(files.length).toBeGreaterThan(10)
  const setters = files.filter((f) => /prefers-color-scheme|data-bs-theme['"]?\s*,|\$color-mode-type:\s*media/.test(readFileSync(join(src, f), 'utf8')))
  expect(setters).toEqual([join('lib', 'theme.ts')])
  const stored = files.filter((f) => /localStorage[^\n]*theme|theme[^\n]*localStorage/i.test(readFileSync(join(src, f), 'utf8')))
  expect(stored).toEqual([])
})
