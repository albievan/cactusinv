import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { api, unwrap, type BankAccount, type BusinessSettings, type Currency, type Schemas } from '../../lib/api/client'
import { useVersion } from '../../lib/version'
import { useAsync } from '../../lib/useAsync'
import { ErrorBanner, Field, Page, Sheet, Spinner } from '../../components/ui'
import { useAuth } from '../../components/auth'
import { IconInfo } from '../../components/icons'
import { EmailSetupGuide, type GuideProvider } from './EmailSetupGuide'
import { settingsHref, settingsNav } from './settingsNav'

type EmailSettings = Schemas['EmailSettings']

export function More() {
  const { user, logout } = useAuth()
  const version = useVersion()
  const links = [
    { to: '/statements', label: 'Statements', hint: "Every customer's balance and aging; e-mail statements in bulk" },
    { to: '/products', label: 'Products', hint: 'Look-up list with a unit cost per currency' },
    { to: '/emails', label: 'Outbox', hint: 'Scheduled, sent and failed e-mails' },
    { to: '/audit', label: 'Audit log', hint: 'Who did what, when — permanent and tamper-evident' },
  ]
  return (
    <Page title="More">
      <ul className="space-y-2">
        {links.map((l) => (
          <li key={l.to}>
            <Link to={l.to} className="card block">
              <div className="font-semibold">{l.label}</div>
              <div className="text-sm text-slate-500">{l.hint}</div>
            </Link>
          </li>
        ))}
      </ul>
      <h2 id="more-settings" className="mb-2 mt-6 px-1 text-sm font-semibold uppercase tracking-wide text-brand-800">Settings</h2>
      <ul className="space-y-2" aria-labelledby="more-settings">
        {settingsNav.map((s) => (
          <li key={s.path}>
            <Link to={settingsHref(s)} className="card flex items-center gap-3">
              <s.icon className="shrink-0 text-brand-600" />
              <span>
                <span className="block font-semibold">{s.label}</span>
                <span className="block text-sm text-slate-500">{s.hint}</span>
              </span>
            </Link>
          </li>
        ))}
      </ul>
      <ChangePassword />
      <div className="mt-6 text-center text-sm text-slate-500">
        Signed in as {user?.email} · <button className="underline" onClick={logout}>Sign out</button>
        <div className="mt-1 text-xs">{version}</div>
      </div>
    </Page>
  )
}

function ChangePassword() {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState<Error | null>(null)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setErr(null)
    setMsg('')
    try {
      await unwrap(api.POST('/auth/password', { body: { currentPassword: current, newPassword: next } }))
      setCurrent('')
      setNext('')
      setMsg('Password changed.')
    } catch (x) {
      setErr(x as Error)
    }
  }
  return (
    <form onSubmit={submit} className="card mt-4 space-y-3">
      <h2 className="font-semibold">Change password</h2>
      <ErrorBanner error={err} />
      <Field label="Current password">{(f) => <input id={f} type="password" autoComplete="current-password" className="input" value={current} onChange={(e) => setCurrent(e.target.value)} required />}</Field>
      <Field label="New password" hint="At least 12 characters.">{(f) => <input id={f} type="password" autoComplete="new-password" minLength={12} className="input" value={next} onChange={(e) => setNext(e.target.value)} required />}</Field>
      <div className="flex items-center justify-end gap-3">
        {msg && <span className="text-sm text-green-700">{msg}</span>}
        <button className="btn-primary">Change password</button>
      </div>
    </form>
  )
}

/**
 * SettingsSections links the Settings sub-pages. On a phone it is the way between them
 * (the sidebar, which nests them under Settings, is hidden below md).
 */
function SettingsSections() {
  return (
    <nav aria-label="Settings sections" className="-mx-3 mb-4 overflow-x-auto px-3 md:hidden">
      <ul className="flex gap-2">
        {settingsNav.map((s) => (
          <li key={s.path} className="shrink-0">
            <NavLink
              to={settingsHref(s)}
              className={({ isActive }) => `inline-flex min-h-[44px] items-center gap-2 whitespace-nowrap rounded-full px-4 text-sm font-semibold ${isActive ? 'bg-brand-600 text-white' : 'bg-white text-brand-900 ring-1 ring-cream-300'}`}
            >
              {s.label}
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  )
}

function SettingsPage({ children }: { children: ReactNode }) {
  return (
    <Page title="Settings">
      <SettingsSections />
      {children}
    </Page>
  )
}

/** BusinessSettingsPage is /settings/business. */
export function BusinessSettingsPage() {
  return <SettingsPage><BusinessForm /></SettingsPage>
}

/** EmailSettingsPage is /settings/email. */
export function EmailSettingsPage() {
  return <SettingsPage><EmailServer /></SettingsPage>
}

/** CurrencySettingsPage is /settings/currencies. */
export function CurrencySettingsPage() {
  return <SettingsPage><CurrenciesAndBanks /></SettingsPage>
}

const presets: Record<string, { host: string; port: number; tls: 'starttls' | 'tls' | 'none'; hint: string }> = {
  Gmail: { host: 'smtp.gmail.com', port: 587, tls: 'starttls', hint: 'Use your Gmail address as user name and a Google App password (Google Account → Security → 2-Step Verification → App passwords), not your normal password.' },
  'Outlook / Microsoft 365': { host: 'smtp.office365.com', port: 587, tls: 'starttls', hint: 'Use your Microsoft 365 address and password (SMTP AUTH must be enabled for the mailbox).' },
  Custom: { host: '', port: 587, tls: 'starttls', hint: 'Enter your provider\'s SMTP host, port and security.' },
}

function EmailServer() {
  const { data, error } = useAsync(() => unwrap(api.GET('/settings/email')), [])
  const { user } = useAuth()
  const [e, setE] = useState<EmailSettings>()
  const [password, setPassword] = useState('')
  const [testTo, setTestTo] = useState('')
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState<Error | null>(null)
  const [guide, setGuide] = useState(false)
  useEffect(() => { if (data) setE(data) }, [data])
  if (!e) return error ? <ErrorBanner error={error} /> : <Spinner />
  const preset = Object.entries(presets).find(([, p]) => p.host === e.host)?.[0] ?? 'Custom'
  const set = (patch: Partial<EmailSettings>) => setE({ ...e, ...patch })
  const save = async (ev: FormEvent) => {
    ev.preventDefault()
    setErr(null)
    setMsg('')
    try {
      setE(await unwrap(api.PUT('/settings/email', { body: { ...e, password: password === '' ? undefined : password } })))
      setPassword('')
      setMsg('Saved.')
    } catch (x) {
      setErr(x as Error)
    }
  }
  const test = async () => {
    setErr(null)
    setMsg('')
    try {
      await unwrap(api.POST('/settings/email/test', { body: { to: testTo || e.username || user?.email || '' } }))
      setMsg('Test e-mail sent — check the inbox.')
    } catch (x) {
      setErr(x as Error)
    }
  }
  return (
    <form onSubmit={save} className="card space-y-4">
      <div>
        <div className="flex items-center justify-between gap-3">
          <h2 className="text-lg font-semibold">E-mail server</h2>
          <button type="button" className="btn-secondary min-h-[44px]" onClick={() => setGuide(true)} aria-haspopup="dialog">
            <IconInfo /> Setup guide
          </button>
        </div>
        <p className="text-sm text-slate-500">The server cactusInv sends invoices and statements through. Until it is enabled, e-mails go to the deployment's default server (the test mailbox in a local install).</p>
      </div>
      <ErrorBanner error={err} />
      <div className="flex flex-wrap gap-2" role="radiogroup" aria-label="Provider">
        {Object.entries(presets).map(([name, p]) => (
          <button key={name} type="button" role="radio" aria-checked={preset === name} className={preset === name ? 'btn-primary' : 'btn-secondary'} onClick={() => set({ host: p.host, port: p.port, tls: p.tls })}>
            {name}
          </button>
        ))}
      </div>
      <p className="rounded-xl bg-cream-50 p-3 text-sm text-slate-600 ring-1 ring-cream-200">{presets[preset]!.hint}</p>
      <div className="grid gap-4 md:grid-cols-2">
        <Field label="SMTP host">{(f) => <input id={f} className="input" value={e.host} onChange={(x) => set({ host: x.target.value })} placeholder="smtp.gmail.com" />}</Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Port">{(f) => <input id={f} type="number" className="input" value={e.port} onChange={(x) => set({ port: Number(x.target.value) })} />}</Field>
          <Field label="Security">
            {(f) => (
              <select id={f} className="input" value={e.tls} onChange={(x) => set({ tls: x.target.value as EmailSettings['tls'] })}>
                <option value="starttls">STARTTLS</option><option value="tls">SSL/TLS</option><option value="none">None</option>
              </select>
            )}
          </Field>
        </div>
        <Field label="User name" hint="For Gmail: your full Gmail address.">{(f) => <input id={f} className="input" autoComplete="off" value={e.username} onChange={(x) => set({ username: x.target.value })} />}</Field>
        <Field label="Password" hint={e.passwordSet ? 'A password is saved (encrypted). Leave empty to keep it.' : 'Stored encrypted.'}>
          {(f) => <input id={f} type="password" className="input" autoComplete="new-password" placeholder={e.passwordSet ? '•••••••• saved' : ''} value={password} onChange={(x) => setPassword(x.target.value)} />}
        </Field>
        <Field label="Send as" hint='e.g. "Example Ltd <billing@example.com>". Defaults to the user name.'>{(f) => <input id={f} className="input" value={e.from} onChange={(x) => set({ from: x.target.value })} />}</Field>
        <label className="flex min-h-[44px] items-center gap-3 md:pt-6"><input type="checkbox" className="h-5 w-5" checked={e.enabled} onChange={(x) => set({ enabled: x.target.checked })} /> Use this server for all e-mail</label>
      </div>
      <div className="flex flex-wrap items-end gap-3 border-t border-cream-200 pt-4">
        <Field label="Send a test e-mail to">{(f) => <input id={f} type="email" className="input md:w-72" value={testTo} placeholder={e.username || 'you@example.com'} onChange={(x) => setTestTo(x.target.value)} />}</Field>
        <button type="button" className="btn-secondary" onClick={test}>Send test e-mail</button>
        <span className="flex-1" />
        {msg && <span className="text-sm text-green-700">{msg}</span>}
        <button className="btn-primary">Save e-mail server</button>
      </div>
      <EmailSetupGuide open={guide} onClose={() => setGuide(false)} provider={preset as GuideProvider} />
    </form>
  )
}

function BusinessForm() {
  const { data, error, loading } = useAsync(() => unwrap(api.GET('/settings/business')), [])
  const [b, setB] = useState<BusinessSettings>()
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState<Error | null>(null)
  useEffect(() => { if (data) setB(data) }, [data])
  if (loading || !b) return <Spinner />
  const set = (patch: Partial<BusinessSettings>) => setB({ ...b, ...patch })
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setErr(null)
    try {
      setB(await unwrap(api.PUT('/settings/business', { body: b })))
      setMsg('Saved.')
    } catch (x) {
      setErr(x as Error)
    }
  }
  return (
    <form onSubmit={submit} className="card space-y-4">
      <h2 className="text-lg font-semibold">Business</h2>
      <ErrorBanner error={err ?? error} />
      <div className="grid gap-4 md:grid-cols-2">
        <Field label="Business name">{(f) => <input id={f} className="input" value={b.name} onChange={(e) => set({ name: e.target.value })} required />}</Field>
        <Field label="Phone">{(f) => <input id={f} className="input" value={b.phone} onChange={(e) => set({ phone: e.target.value })} />}</Field>
        <Field label="Reply-to e-mail">{(f) => <input id={f} type="email" className="input" value={b.email} onChange={(e) => set({ email: e.target.value })} />}</Field>
        <Field label="VAT number" hint="Printed only when set.">{(f) => <input id={f} className="input" value={b.taxNumber} onChange={(e) => set({ taxNumber: e.target.value })} />}</Field>
        <Field label="Default VAT %" hint="Used for new invoices and schedules. 0 = no VAT: nothing about VAT appears on invoices.">{(f) => <input id={f} className="input" inputMode="decimal" value={b.vatPercent} onChange={(e) => set({ vatPercent: e.target.value })} />}</Field>
        <Field label="Address">{(f) => <textarea id={f} className="input min-h-[96px]" value={b.addressLines} onChange={(e) => set({ addressLines: e.target.value })} />}</Field>
        <div className="space-y-4">
          <Field label="Time zone" hint="IANA name, e.g. Africa/Johannesburg">{(f) => <input id={f} className="input" value={b.timeZone} onChange={(e) => set({ timeZone: e.target.value })} />}</Field>
          <Field label="Invoice number format" hint="{code} customer code, {seq} sequence, {dd} day, {mm} month, {yy} or {yyyy} year — e.g. {code}/{dd}{mm}-{seq}/{yy} → CD/2108-1195/26">{(f) => <input id={f} className="input" value={b.numberFormat} onChange={(e) => set({ numberFormat: e.target.value })} />}</Field>
          <Field label="Next invoice sequence" hint="May only increase.">{(f) => <input id={f} type="number" inputMode="numeric" className="input" value={b.nextSequence} onChange={(e) => set({ nextSequence: Number(e.target.value) })} />}</Field>
        </div>
        <Field label="Default e-mail subject">{(f) => <input id={f} className="input" value={b.emailSubject} onChange={(e) => set({ emailSubject: e.target.value })} />}</Field>
        <Field label="Disclaimer" hint="Printed at the foot of every invoice and statement PDF — read fresh each time a PDF is made. A customer's own disclaimer replaces it.">{(f) => <textarea id={f} className="input min-h-[96px]" value={b.defaultNotes} onChange={(e) => set({ defaultNotes: e.target.value })} />}</Field>
        <div className="md:col-span-2">
          <Field label="Default e-mail body">{(f) => <textarea id={f} className="input min-h-[120px]" value={b.emailBody} onChange={(e) => set({ emailBody: e.target.value })} />}</Field>
        </div>
      </div>
      <div className="flex items-center justify-end gap-3">
        {msg && <span className="text-sm text-green-700">{msg}</span>}
        <button className="btn-primary">Save business</button>
      </div>
    </form>
  )
}

const blankBank = (currency: string): BankAccount => ({ currency, label: '', bankName: '', branch: '', branchCode: '', accountName: '', accountNumber: '', accountType: '', swiftBic: '', iban: '', country: '', isDefault: false, active: true, version: 0 })

function CurrenciesAndBanks() {
  const currencies = useAsync(() => unwrap(api.GET('/currencies')).then((r) => r.items), [])
  const banks = useAsync(() => unwrap(api.GET('/bank-accounts')).then((r) => r.items), [])
  const [editCur, setEditCur] = useState<Currency & { isNew?: boolean }>()
  const [editBank, setEditBank] = useState<BankAccount>()
  const [err, setErr] = useState<Error | null>(null)
  const saveCurrency = async (c: Currency & { isNew?: boolean }) => {
    setErr(null)
    try {
      const body = { code: c.code, name: c.name, symbol: c.symbol, minorUnits: c.minorUnits, active: c.active }
      if (c.isNew) await unwrap(api.POST('/currencies', { body }))
      else await unwrap(api.PUT('/currencies/{code}', { params: { path: { code: c.code } }, body }))
      setEditCur(undefined)
      currencies.reload()
    } catch (x) { setErr(x as Error) }
  }
  const saveBank = async (b: BankAccount) => {
    setErr(null)
    try {
      if (b.id) await unwrap(api.PUT('/bank-accounts/{id}', { params: { path: { id: b.id } }, body: b }))
      else await unwrap(api.POST('/bank-accounts', { body: b }))
      setEditBank(undefined)
      banks.reload()
    } catch (x) { setErr(x as Error) }
  }
  const deleteBank = async (b: BankAccount) => {
    setErr(null)
    try {
      await unwrap(api.DELETE('/bank-accounts/{id}', { params: { path: { id: b.id! } } }))
      setEditBank(undefined)
      banks.reload()
    } catch (x) { setErr(x as Error) }
  }
  return (
    <section className="card space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Currencies & bank accounts</h2>
        <button className="btn-secondary" onClick={() => setEditCur({ code: '', name: '', symbol: '', minorUnits: 2, active: true, isNew: true })}>+ Currency</button>
      </div>
      <p className="text-sm text-slate-500">Each currency has its own bank accounts; the default account is printed on invoices in that currency.</p>
      <ErrorBanner error={err ?? currencies.error ?? banks.error} />
      {currencies.data?.map((c) => (
        <div key={c.code} className="rounded-xl ring-1 ring-slate-200">
          <div className="flex items-center justify-between gap-2 p-3">
            <button className="text-left" onClick={() => setEditCur(c)}>
              <span className="font-semibold">{c.code}</span> <span className="text-slate-500">{c.name} {c.symbol && `(${c.symbol})`}{!c.active && ' · inactive'}</span>
            </button>
            <button className="btn-secondary min-h-[36px] text-xs" onClick={() => setEditBank(blankBank(c.code))}>+ Bank</button>
          </div>
          <ul className="divide-y divide-slate-100 border-t border-slate-100">
            {banks.data?.filter((b) => b.currency === c.code).map((b) => (
              <li key={b.id}>
                <button className="flex w-full items-center justify-between p-3 text-left text-sm" onClick={() => setEditBank(b)}>
                  <span>{b.label} · {b.bankName} · {b.accountNumber}{b.country && ` · ${b.country}`}{!b.active && ' · inactive'}</span>
                  {b.isDefault && <span className="chip bg-green-100 text-green-800">default</span>}
                </button>
              </li>
            ))}
          </ul>
        </div>
      ))}
      <Sheet title={editCur?.isNew ? 'New currency' : `Currency ${editCur?.code ?? ''}`} open={!!editCur} onClose={() => setEditCur(undefined)}>
        {editCur && <CurrencyForm c={editCur} onSave={saveCurrency} />}
      </Sheet>
      <Sheet title={editBank?.id ? 'Bank account' : `New ${editBank?.currency ?? ''} bank account`} open={!!editBank} onClose={() => setEditBank(undefined)}>
        {editBank && <BankForm b={editBank} onSave={saveBank} onDelete={deleteBank} />}
      </Sheet>
    </section>
  )
}

function CurrencyForm({ c: initial, onSave }: { c: Currency & { isNew?: boolean }; onSave: (c: Currency & { isNew?: boolean }) => void }) {
  const [c, setC] = useState(initial)
  return (
    <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); onSave(c) }}>
      <Field label="ISO code">{(f) => <input id={f} className="input uppercase" maxLength={3} value={c.code} disabled={!c.isNew} onChange={(e) => setC({ ...c, code: e.target.value.toUpperCase() })} required />}</Field>
      <Field label="Name">{(f) => <input id={f} className="input" value={c.name} onChange={(e) => setC({ ...c, name: e.target.value })} required />}</Field>
      <Field label="Symbol">{(f) => <input id={f} className="input" value={c.symbol} onChange={(e) => setC({ ...c, symbol: e.target.value })} />}</Field>
      <Field label="Decimal places" hint="Cannot change later.">{(f) => <input id={f} type="number" min={0} max={3} className="input" value={c.minorUnits} disabled={!c.isNew} onChange={(e) => setC({ ...c, minorUnits: Number(e.target.value) })} />}</Field>
      <label className="flex min-h-[44px] items-center gap-3"><input type="checkbox" className="h-5 w-5" checked={c.active} onChange={(e) => setC({ ...c, active: e.target.checked })} /> Active</label>
      <button className="btn-primary w-full">Save</button>
    </form>
  )
}

function BankForm({ b: initial, onSave, onDelete }: { b: BankAccount; onSave: (b: BankAccount) => void; onDelete: (b: BankAccount) => void }) {
  const [b, setB] = useState(initial)
  const text = (k: keyof BankAccount, label: string, required = false) => (
    <Field label={label}>{(f) => <input id={f} className="input" value={String(b[k] ?? '')} onChange={(e) => setB({ ...b, [k]: e.target.value })} required={required} />}</Field>
  )
  return (
    <form className="grid gap-4" onSubmit={(e) => { e.preventDefault(); onSave(b) }}>
      {text('label', 'Label (for you, e.g. "Main ZAR account")', true)}
      {text('bankName', 'Name of bank', true)}
      {text('branch', 'Branch name')}
      {text('branchCode', 'Sort code / Branch code')}
      {text('accountNumber', 'Account number', true)}
      {text('accountName', 'Account holder')}
      {text('accountType', 'Account type (e.g. Savings, Current)')}
      {text('swiftBic', 'SWIFT')}
      {text('iban', 'IBAN')}
      {text('country', 'Country')}
      <label className="flex min-h-[44px] items-center gap-3"><input type="checkbox" className="h-5 w-5" checked={b.isDefault} onChange={(e) => setB({ ...b, isDefault: e.target.checked })} /> Default for {b.currency}</label>
      <label className="flex min-h-[44px] items-center gap-3"><input type="checkbox" className="h-5 w-5" checked={b.active} onChange={(e) => setB({ ...b, active: e.target.checked })} /> Active</label>
      <div className="flex gap-2">
        {b.id && <button type="button" className="btn-danger" onClick={() => onDelete(b)}>Delete</button>}
        <button className="btn-primary flex-1">Save</button>
      </div>
    </form>
  )
}
