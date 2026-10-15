import { useEffect, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { api, unwrap, type BankAccount, type BusinessSettings, type Currency } from '../../lib/api/client'
import { useAsync } from '../../lib/useAsync'
import { ErrorBanner, Field, Page, Sheet, Spinner } from '../../components/ui'
import { useAuth } from '../../components/auth'

export function More() {
  const { user, logout } = useAuth()
  const links = [
    { to: '/products', label: 'Products', hint: 'Look-up list with a price per currency' },
    { to: '/emails', label: 'Outbox', hint: 'Scheduled, sent and failed e-mails' },
    { to: '/settings', label: 'Settings', hint: 'Business profile, currencies and bank accounts' },
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
      <ChangePassword />
      <div className="mt-6 text-center text-sm text-slate-500">
        Signed in as {user?.email} · <button className="underline" onClick={logout}>Sign out</button>
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

export function Settings() {
  return (
    <Page title="Settings" back="/more">
      <div className="space-y-6">
        <BusinessForm />
        <CurrenciesAndBanks />
      </div>
    </Page>
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
        <Field label="Tax number">{(f) => <input id={f} className="input" value={b.taxNumber} onChange={(e) => set({ taxNumber: e.target.value })} />}</Field>
        <Field label="Address">{(f) => <textarea id={f} className="input min-h-[96px]" value={b.addressLines} onChange={(e) => set({ addressLines: e.target.value })} />}</Field>
        <div className="space-y-4">
          <Field label="Time zone" hint="IANA name, e.g. Africa/Johannesburg">{(f) => <input id={f} className="input" value={b.timeZone} onChange={(e) => set({ timeZone: e.target.value })} />}</Field>
          <Field label="Invoice number format" hint="{code} {seq} {yy} {yyyy} {mm} — e.g. {code}/{seq}/{yy}">{(f) => <input id={f} className="input" value={b.numberFormat} onChange={(e) => set({ numberFormat: e.target.value })} />}</Field>
          <Field label="Next invoice sequence" hint="May only increase.">{(f) => <input id={f} type="number" inputMode="numeric" className="input" value={b.nextSequence} onChange={(e) => set({ nextSequence: Number(e.target.value) })} />}</Field>
        </div>
        <Field label="Default e-mail subject">{(f) => <input id={f} className="input" value={b.emailSubject} onChange={(e) => set({ emailSubject: e.target.value })} />}</Field>
        <Field label="Default notes">{(f) => <textarea id={f} className="input" value={b.defaultNotes} onChange={(e) => set({ defaultNotes: e.target.value })} />}</Field>
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

const blankBank = (currency: string): BankAccount => ({ currency, label: '', bankName: '', branch: '', branchCode: '', accountName: '', accountNumber: '', accountType: '', swiftBic: '', iban: '', isDefault: false, active: true, version: 0 })

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
        <div key={c.code} className="rounded-xl ring-1 ring-slate-200 dark:ring-slate-700">
          <div className="flex items-center justify-between gap-2 p-3">
            <button className="text-left" onClick={() => setEditCur(c)}>
              <span className="font-semibold">{c.code}</span> <span className="text-slate-500">{c.name} {c.symbol && `(${c.symbol})`}{!c.active && ' · inactive'}</span>
            </button>
            <button className="btn-secondary min-h-[36px] text-xs" onClick={() => setEditBank(blankBank(c.code))}>+ Bank</button>
          </div>
          <ul className="divide-y divide-slate-100 border-t border-slate-100 dark:divide-slate-800 dark:border-slate-800">
            {banks.data?.filter((b) => b.currency === c.code).map((b) => (
              <li key={b.id}>
                <button className="flex w-full items-center justify-between p-3 text-left text-sm" onClick={() => setEditBank(b)}>
                  <span>{b.label} · {b.bankName} · {b.accountNumber}{!b.active && ' · inactive'}</span>
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
      {text('label', 'Label', true)}
      {text('bankName', 'Bank', true)}
      {text('branch', 'Branch')}
      {text('branchCode', 'Branch / bank code')}
      {text('accountName', 'Account holder')}
      {text('accountType', 'Account type (e.g. Savings)')}
      {text('accountNumber', 'Account number', true)}
      {text('swiftBic', 'SWIFT / BIC')}
      {text('iban', 'IBAN')}
      <label className="flex min-h-[44px] items-center gap-3"><input type="checkbox" className="h-5 w-5" checked={b.isDefault} onChange={(e) => setB({ ...b, isDefault: e.target.checked })} /> Default for {b.currency}</label>
      <label className="flex min-h-[44px] items-center gap-3"><input type="checkbox" className="h-5 w-5" checked={b.active} onChange={(e) => setB({ ...b, active: e.target.checked })} /> Active</label>
      <div className="flex gap-2">
        {b.id && <button type="button" className="btn-danger" onClick={() => onDelete(b)}>Delete</button>}
        <button className="btn-primary flex-1">Save</button>
      </div>
    </form>
  )
}
