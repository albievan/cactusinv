// @vitest-environment node
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import config from '../tailwind.config'

// The app has one light theme. A class with the dark variant would apply on a phone in dark mode under the
// default 'media' strategy and put light-theme text on a dark background (PLAN 13.1).
const src = fileURLToPath(new URL('.', import.meta.url))
const variant = ['dark', ':'].join('') // spelled out so this file does not match itself

test('Tailwind dark-variant styles apply only under a class the app never sets', () => {
  expect(config.darkMode).toBe('class')
  const html = readFileSync(join(src, '..', 'index.html'), 'utf8')
  expect(html).not.toMatch(/class="[^"]*\bdark\b/)
})

test('no dark-variant class anywhere in web/src', () => {
  const files = (readdirSync(src, { recursive: true }) as string[]).filter((f) => /\.(ts|tsx|css)$/.test(f) && !/\.d\.ts$|\.test\.tsx?$/.test(f))
  expect(files.length).toBeGreaterThan(10)
  // Not preceded by a word character or '-': catches 'dark:x' and stacked 'md:dark:x', not 'non-dark:'.
  const darkClass = new RegExp(`(?<![\\w-])${variant}`)
  const offenders = files.filter((f) => darkClass.test(readFileSync(join(src, f), 'utf8')))
  expect(offenders).toEqual([])
})
