// @vitest-environment node
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import * as sass from 'sass'

// Every CSS class the app uses must exist in its stylesheet (Bootstrap 5.3, Bootstrap Icons
// and src/styles/app.scss): a class that matches nothing, such as a leftover utility from
// another framework, fails here instead of silently not styling the page.
const src = fileURLToPath(new URL('.', import.meta.url))
const root = join(src, '..')

function definedClasses(): Set<string> {
  const css =
    sass.compile(join(src, 'styles', 'app.scss'), {
      loadPaths: [join(root, 'node_modules')],
      quietDeps: true,
      silenceDeprecations: ['import', 'global-builtin', 'color-functions', 'mixed-decls', 'if-function'],
    }).css + readFileSync(join(root, 'node_modules', 'bootstrap-icons', 'font', 'bootstrap-icons.css'), 'utf8')
  return new Set([...css.matchAll(/\.(-?[A-Za-z_][\w-]*)/g)].map((m) => m[1]!))
}

/** The class names in the className attributes of a source file (literals and the static parts of expressions). */
export function usedClasses(text: string): string[] {
  const out: string[] = []
  for (const m of text.matchAll(/className=(?:"([^"]*)"|\{)/g)) {
    let chunk = m[1]
    if (chunk === undefined) {
      // The expression up to its closing brace.
      let depth = 1
      let i = m.index! + m[0].length
      const start = i
      for (; i < text.length && depth > 0; i++) depth += text[i] === '{' ? 1 : text[i] === '}' ? -1 : 0
      const expr = text.slice(start, i - 1)
      chunk = [...expr.matchAll(/'([^']*)'|"([^"]*)"|`([^`]*)`/g)].map((s) => (s[1] ?? s[2] ?? s[3]!).replace(/\$\{[^}]*\}/g, ' ')).join(' ')
    }
    out.push(...chunk.split(/\s+/).filter((c) => /^[a-z][\w-]*[a-z0-9]$/i.test(c)))
  }
  return out
}

test('every class used in web/src is defined by the stylesheet', () => {
  const defined = definedClasses()
  expect(defined.has('btn-primary') && defined.has('bi-house') && defined.has('app-sidebar')).toBe(true)
  const files = (readdirSync(src, { recursive: true }) as string[]).filter((f) => /\.tsx$/.test(f) && !/\.test\.tsx$/.test(f))
  const missing: string[] = []
  for (const f of files) {
    for (const c of new Set(usedClasses(readFileSync(join(src, f), 'utf8')))) if (!defined.has(c)) missing.push(`${f}: ${c}`)
  }
  expect(missing).toEqual([])
})

test('usedClasses reads literals and expressions', () => {
  expect(usedClasses(`<a className="btn btn-primary" /><b className={x ? 'd-flex gap-2' : \`text-\${t} small\`} />`)).toEqual(['btn', 'btn-primary', 'd-flex', 'gap-2', 'small'])
})
