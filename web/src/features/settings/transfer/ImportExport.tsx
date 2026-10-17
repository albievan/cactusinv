import { useEffect, useState, type FormEvent } from 'react'
import { api, unwrap, type Schemas } from '../../../lib/api/client'
import { formatDateTime } from '../../../lib/format'
import { ErrorBanner, Field } from '../../../components/ui'
import { SettingsPage } from '../Settings'

type SettingsFile = Schemas['SettingsFile']
type Result = Schemas['SettingsImportResult']
export type Section = Schemas['SettingsSection']

const MIN_PASSPHRASE = 16

/** The file's sections, in the order they are imported (customers need their currencies and banks). */
const sections: { key: Section; label: string; many: boolean }[] = [
  { key: 'currencies', label: 'Currencies', many: true },
  { key: 'bankAccounts', label: 'Bank accounts', many: true },
  { key: 'products', label: 'Products', many: true },
  { key: 'customers', label: 'Customers', many: true },
  { key: 'business', label: 'Business', many: false },
  { key: 'emailServer', label: 'E-mail server', many: false },
  { key: 'schedules', label: 'Schedules', many: true },
]

/** count is how many items a section of a plain file holds; 0 when the file has none. */
export function count(f: SettingsFile, s: Section): number {
  const v = f[s]
  if (Array.isArray(v)) return v.length
  return v ? 1 : 0
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

/** download saves text as a file. */
function download(name: string, text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: 'application/json' }))
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
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
          Move the business details, e-mail server, currencies, bank accounts, products, customers and schedules to another cactusInv as
          one file. Invoices are not included: a full copy is a backup (Settings → Backup & restore).
        </p>
        <Export />
        <Import />
      </section>
    </SettingsPage>
  )
}

function Export() {
  const [plain, setPlain] = useState(false)
  const [pass, setPass] = useState('')
  const [again, setAgain] = useState('')
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState('')
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setErr(null)
    setDone('')
    if (!plain) {
      if (pass.length < MIN_PASSPHRASE) return setErr(new Error(`The passphrase needs at least ${MIN_PASSPHRASE} characters.`))
      if (pass !== again) return setErr(new Error('The two passphrases are not the same.'))
    }
    setBusy(true)
    try {
      const f = await unwrap(api.POST('/settings/export', { body: plain ? {} : { passphrase: pass } }))
      const name = `cactusinv-settings-${new Date().toISOString().slice(0, 10)}.json`
      download(name, JSON.stringify(f, null, 2))
      setPass('')
      setAgain('')
      setDone(
        plain
          ? `Downloaded ${name}: ${count(f, 'customers')} customers, ${count(f, 'products')} products, ${count(f, 'schedules')} schedules, ${count(f, 'currencies')} currencies and ${count(f, 'bankAccounts')} bank accounts, the business and the e-mail server.`
          : `Downloaded ${name}, protected with your passphrase. Keep the passphrase safe: without it the file cannot be imported.`,
      )
    } catch (x) {
      setErr(x as Error)
    } finally {
      setBusy(false)
    }
  }
  return (
    <form onSubmit={submit} className="card space-y-3">
      <h3 className="font-semibold">Export</h3>
      <ErrorBanner error={err} />
      <p className="text-sm text-slate-600">
        The file holds customer contact details and bank account numbers. It is protected with a passphrase unless you choose otherwise. The
        e-mail server password is never exported.
      </p>
      {!plain && (
        <>
          <Field label="Passphrase" hint={`At least ${MIN_PASSPHRASE} characters. It cannot be recovered.`}>
            {(id) => <input id={id} type="password" autoComplete="new-password" className="input" value={pass} onChange={(e) => setPass(e.target.value)} />}
          </Field>
          <Field label="Passphrase again">
            {(id) => <input id={id} type="password" autoComplete="new-password" className="input" value={again} onChange={(e) => setAgain(e.target.value)} />}
          </Field>
        </>
      )}
      <label className="flex min-h-11 items-center gap-3 text-sm">
        <input type="checkbox" className="h-5 w-5" checked={plain} onChange={(e) => setPlain(e.target.checked)} />
        Without a passphrase (readable JSON, not protected)
      </label>
      <button className="btn-primary" disabled={busy}>
        {busy ? 'Exporting…' : 'Export to a JSON file'}
      </button>
      {done && <p className="text-sm text-green-700">{done}</p>}
    </form>
  )
}

type Chosen = { name: string; data: SettingsFile; protected: boolean }

/** run checks (apply false) or imports a file's chosen sections; all of them when only is omitted. */
const run = (apply: boolean, f: Chosen, passphrase: string, only?: Set<Section>) =>
  unwrap(
    api.POST('/settings/import', {
      params: { query: { apply } },
      body: { file: f.data, ...(passphrase ? { passphrase } : {}), ...(only ? { sections: [...only] } : {}) },
    }),
  )

function Import() {
  const [file, setFile] = useState<Chosen | null>(null)
  const [pass, setPass] = useState('')
  // The passphrase that opened a protected file, and how many items each section holds.
  const [opened, setOpened] = useState<{ passphrase: string; counts: Partial<Record<Section, number>> } | null>(null)
  const [chosen, setChosen] = useState<Set<Section>>(new Set())
  const [check, setCheck] = useState<Result | null>(null)
  const [done, setDone] = useState<Result | null>(null)
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)

  const reset = () => {
    setErr(null)
    setDone(null)
    setCheck(null)
    setFile(null)
    setOpened(null)
    setPass('')
  }

  const choose = async (f: File | undefined) => {
    reset()
    if (!f) return
    try {
      const data = parseSettingsFile(await readText(f))
      const isProtected = !!data.encrypted
      setFile({ name: f.name, data, protected: isProtected })
      if (!isProtected) {
        const counts = Object.fromEntries(sections.map((s) => [s.key, count(data, s.key)]))
        setOpened({ passphrase: '', counts })
        setChosen(new Set(sections.filter((s) => counts[s.key]! > 0).map((s) => s.key)))
      }
    } catch (x) {
      setErr(x as Error)
    }
  }

  // A protected file is opened by checking all of it: the counts say what it holds.
  const open = async (e: FormEvent) => {
    e.preventDefault()
    if (!file) return
    setBusy(true)
    setErr(null)
    try {
      const r = await run(false, file, pass)
      const counts = Object.fromEntries(sections.map((s) => [s.key, r[s.key].created + r[s.key].updated + r[s.key].unchanged]))
      setChosen(new Set(sections.filter((s) => counts[s.key]! > 0).map((s) => s.key)))
      setOpened({ passphrase: pass, counts })
    } catch (x) {
      setErr(x as Error)
    } finally {
      setBusy(false)
    }
  }

  // Each choice is checked on the server first (nothing is saved), so the user sees what an
  // import would change, and any error, before importing.
  useEffect(() => {
    if (!file || !opened || chosen.size === 0) return
    let alive = true
    setBusy(true)
    setErr(null)
    setCheck(null)
    run(false, file, opened.passphrase, chosen)
      .then((r) => alive && setCheck(r))
      .catch((x: Error) => alive && setErr(x))
      .finally(() => alive && setBusy(false))
    return () => {
      alive = false
    }
  }, [file, opened, chosen])

  const apply = async () => {
    if (!file || !opened) return
    setBusy(true)
    setErr(null)
    try {
      setDone(await run(true, file, opened.passphrase, chosen))
      setCheck(null)
      setFile(null)
      setOpened(null)
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
        Currencies, customers and products are matched by code or SKU, bank accounts by account number, schedules by customer and name: a
        match is updated, anything else is added. Nothing is deleted. New schedules are added paused, so nothing is invoiced until you resume
        them. The next invoice number only moves forward, and the e-mail server keeps the password stored here.
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
        <p className="text-sm">
          <span className="break-all font-medium">{file.name}</span>
          {file.data.exportedAt && <> · exported {formatDateTime(file.data.exportedAt)}</>}
          {file.data.appVersion && <> · cactusInv {file.data.appVersion}</>}
          {file.protected && <> · protected with a passphrase</>}
        </p>
      )}
      {file?.protected && !opened && (
        <form onSubmit={open} className="space-y-3">
          <Field label="The file's passphrase">
            {(id) => <input id={id} type="password" autoComplete="off" className="input" value={pass} onChange={(e) => setPass(e.target.value)} required />}
          </Field>
          <button className="btn-primary" disabled={busy || !pass}>
            Open
          </button>
        </form>
      )}
      {file && opened && (
        <fieldset className="space-y-1">
          <legend className="text-sm font-medium">Import</legend>
          {sections.map((s) => {
            const n = opened.counts[s.key] ?? 0
            return (
              <label key={s.key} className={`flex min-h-11 items-center gap-3 text-sm ${n === 0 ? 'text-slate-400' : ''}`}>
                <input type="checkbox" className="h-5 w-5" checked={chosen.has(s.key)} disabled={n === 0 || busy} onChange={() => toggle(s.key)} />
                <span>
                  {s.label}
                  {s.many && <span className="text-slate-500"> ({n})</span>}
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
