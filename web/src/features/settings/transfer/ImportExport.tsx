import { useEffect, useState } from 'react'
import { api, idempotencyKey, unwrap, type Schemas } from '../../../lib/api/client'
import { formatDateTime } from '../../../lib/format'
import { ErrorBanner } from '../../../components/ui'
import { SettingsPage } from '../Settings'

type SettingsFile = Schemas['SettingsFile']
type Result = Schemas['SettingsImportResult']
type Section = 'business' | 'emailServer' | 'currencies' | 'bankAccounts' | 'customers'

/** The file's sections, in the order they are imported (customers need their currencies and banks). */
const sections: { key: Section; label: string }[] = [
  { key: 'currencies', label: 'Currencies' },
  { key: 'bankAccounts', label: 'Bank accounts' },
  { key: 'customers', label: 'Customers' },
  { key: 'business', label: 'Business' },
  { key: 'emailServer', label: 'E-mail server' },
]

/** count is how many items a section of the file holds; 0 when the file has none. */
export function count(f: SettingsFile, s: Section): number {
  const v = f[s]
  if (Array.isArray(v)) return v.length
  return v ? 1 : 0
}

/** pick returns the file with only the chosen sections. */
export function pick(f: SettingsFile, chosen: Set<Section>): SettingsFile {
  const out: SettingsFile = { format: f.format, formatVersion: f.formatVersion }
  for (const { key } of sections) {
    if (chosen.has(key) && f[key] !== undefined) Object.assign(out, { [key]: f[key] })
  }
  return out
}

/** parseSettingsFile reads a chosen file, refusing anything that is not a settings file. */
export function parseSettingsFile(text: string): SettingsFile {
  let v: unknown
  try {
    v = JSON.parse(text)
  } catch {
    throw new Error('The file is not JSON. Choose a file exported from Settings → Import/Export Settings.')
  }
  const f = v as Partial<SettingsFile> | null
  if (!f || typeof f !== 'object' || f.format !== 'cactusinv-settings') {
    throw new Error('This is not a cactusInv settings file. Choose a file exported from Settings → Import/Export Settings.')
  }
  if (f.formatVersion !== 1) throw new Error('The file was made by a newer cactusInv; update this one first.')
  return f as SettingsFile
}

/** readText reads a chosen file as text. */
function readText(f: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader()
    r.onload = () => resolve(String(r.result ?? ''))
    r.onerror = () => reject(new Error('The file could not be read.'))
    r.readAsText(f)
  })
}

/** ImportExportSettingsPage is /settings/import-export: the settings as a JSON file. */
export function ImportExportSettingsPage() {
  return (
    <SettingsPage>
      <section className="space-y-4" aria-labelledby="transfer-heading">
        <h2 id="transfer-heading" className="text-xl font-bold text-brand-900">
          Import/Export Settings
        </h2>
        <p className="text-sm text-slate-600">
          Move the business details, e-mail server, currencies, bank accounts and customers to another cactusInv as one JSON file, or keep a
          copy you can read. Invoices, products and schedules are not included: a full copy is a backup (Settings → Backup & restore).
        </p>
        <Export />
        <Import />
      </section>
    </SettingsPage>
  )
}

function Export() {
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState('')
  const exportFile = async () => {
    setErr(null)
    setDone('')
    setBusy(true)
    try {
      const f = await unwrap(api.GET('/settings/export'))
      const name = `cactusinv-settings-${new Date().toISOString().slice(0, 10)}.json`
      const url = URL.createObjectURL(new Blob([JSON.stringify(f, null, 2)], { type: 'application/json' }))
      const a = document.createElement('a')
      a.href = url
      a.download = name
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(url)
      setDone(
        `Downloaded ${name}: ${count(f, 'customers')} customers, ${count(f, 'currencies')} currencies, ${count(f, 'bankAccounts')} bank accounts, the business and the e-mail server.`,
      )
    } catch (x) {
      setErr(x as Error)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="card space-y-3">
      <h3 className="font-semibold">Export</h3>
      <ErrorBanner error={err} />
      <p className="text-sm text-slate-600">
        The file is not encrypted and holds customer contact details and bank account numbers, so store it safely. The e-mail server
        password is never exported.
      </p>
      <button className="btn-primary" onClick={exportFile} disabled={busy}>
        {busy ? 'Exporting…' : 'Export to a JSON file'}
      </button>
      {done && <p className="text-sm text-green-700">{done}</p>}
    </div>
  )
}

function Import() {
  const [file, setFile] = useState<{ name: string; data: SettingsFile } | null>(null)
  const [chosen, setChosen] = useState<Set<Section>>(new Set())
  const [check, setCheck] = useState<Result | null>(null)
  const [done, setDone] = useState<Result | null>(null)
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)

  const choose = async (f: File | undefined) => {
    setErr(null)
    setDone(null)
    setCheck(null)
    setFile(null)
    if (!f) return
    try {
      const data = parseSettingsFile(await readText(f))
      setFile({ name: f.name, data })
      setChosen(new Set(sections.filter((s) => count(data, s.key) > 0).map((s) => s.key)))
    } catch (x) {
      setErr(x as Error)
    }
  }

  // Each choice is checked on the server first (nothing is saved), so the user sees what an
  // import would change, and any error, before importing.
  useEffect(() => {
    if (!file || chosen.size === 0) return
    let alive = true
    setBusy(true)
    setErr(null)
    setCheck(null)
    unwrap(api.POST('/settings/import', { params: { query: { apply: false } }, body: pick(file.data, chosen) }))
      .then((r) => alive && setCheck(r))
      .catch((x: Error) => alive && setErr(x))
      .finally(() => alive && setBusy(false))
    return () => {
      alive = false
    }
  }, [file, chosen])

  const apply = async () => {
    if (!file) return
    setBusy(true)
    setErr(null)
    try {
      const r = await unwrap(
        api.POST('/settings/import', {
          params: { query: { apply: true }, header: { 'Idempotency-Key': idempotencyKey() } },
          body: pick(file.data, chosen),
        }),
      )
      setDone(r)
      setCheck(null)
      setFile(null)
    } catch (x) {
      setErr(x as Error)
    } finally {
      setBusy(false)
    }
  }

  const toggle = (s: Section) => {
    const next = new Set(chosen)
    if (next.has(s)) next.delete(s)
    else next.add(s)
    setDone(null)
    setChosen(next)
  }

  return (
    <div className="card space-y-3">
      <h3 className="font-semibold">Import</h3>
      <p className="text-sm text-slate-600">
        Currencies are matched by code, bank accounts by account number, customers by code: a match is updated, anything else is added.
        Nothing is deleted. The next invoice number only moves forward, and the e-mail server keeps the password stored here.
      </p>
      <label className={`btn-secondary cursor-pointer ${busy ? 'pointer-events-none opacity-50' : ''}`}>
        Choose a settings file
        <input
          type="file"
          accept=".json,application/json"
          className="sr-only"
          aria-label="Choose a settings file"
          disabled={busy}
          onChange={(e) => {
            const f = e.target.files?.[0]
            e.target.value = '' // the same file can be chosen again after a change
            void choose(f)
          }}
        />
      </label>
      <ErrorBanner error={err} />
      {file && (
        <fieldset className="space-y-2">
          <legend className="text-sm">
            <span className="break-all font-medium">{file.name}</span>
            {file.data.exportedAt && <> · exported {formatDateTime(file.data.exportedAt)}</>}
            {file.data.appVersion && <> · cactusInv {file.data.appVersion}</>}
          </legend>
          {sections.map((s) => {
            const n = count(file.data, s.key)
            return (
              <label key={s.key} className={`flex min-h-11 items-center gap-3 text-sm ${n === 0 ? 'text-slate-400' : ''}`}>
                <input type="checkbox" className="h-5 w-5" checked={chosen.has(s.key)} disabled={n === 0 || busy} onChange={() => toggle(s.key)} />
                <span>
                  {s.label}
                  {(s.key === 'currencies' || s.key === 'bankAccounts' || s.key === 'customers') && <span className="text-slate-500"> ({n})</span>}
                  {n === 0 && <span className="text-slate-500"> — not in the file</span>}
                </span>
              </label>
            )
          })}
        </fieldset>
      )}
      {busy && <p className="text-sm text-slate-500">Checking…</p>}
      {check && file && <Changes result={check} chosen={chosen} />}
      {check && file && (
        <button className="btn-primary" onClick={apply} disabled={busy || chosen.size === 0}>
          Import
        </button>
      )}
      {done && (
        <>
          <p className="text-sm font-medium text-green-700">Imported.</p>
          <Changes result={done} chosen={chosen} />
        </>
      )}
    </div>
  )
}

function Changes({ result, chosen }: { result: Result; chosen: Set<Section> }) {
  const rows = sections.filter((s) => chosen.has(s.key))
  return (
    <div className="space-y-2">
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <caption className="sr-only">{result.applied ? 'What was imported' : 'What an import would change'}</caption>
          <thead className="text-left text-xs uppercase tracking-wide text-slate-600">
            <tr>
              <th className="py-1 pr-3">Section</th>
              <th className="py-1 pr-3 text-right">New</th>
              <th className="py-1 pr-3 text-right">Changed</th>
              <th className="py-1 text-right">Unchanged</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((s) => {
              const c = result[s.key]
              return (
                <tr key={s.key} className="border-t border-cream-200">
                  <td className="py-1 pr-3">{s.label}</td>
                  <td className="py-1 pr-3 text-right">{c.created}</td>
                  <td className="py-1 pr-3 text-right">{c.updated}</td>
                  <td className="py-1 text-right">{c.unchanged}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      {result.warnings.map((w) => (
        <p key={w} role="note" className="text-sm font-medium text-amber-800">
          {w}
        </p>
      ))}
    </div>
  )
}
