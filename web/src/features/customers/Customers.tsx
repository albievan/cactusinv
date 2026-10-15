import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, unwrap, type Customer } from '../../lib/api/client'
import { useCurrencies } from '../../lib/lookups'
import { useAsync } from '../../lib/useAsync'
import { Badge, Empty, ErrorBanner, Field, Page, SearchBox, Spinner, StickyActions } from '../../components/ui'

export function CustomerList() {
  const [q, setQ] = useState('')
  const { data, error, loading } = useAsync(() => unwrap(api.GET('/customers', { params: { query: { q: q || undefined, includeInactive: true, limit: 200 } } })), [q])
  return (
    <Page title="Customers" actions={<Link to="/customers/new" className="btn-primary">+ New</Link>}>
      <SearchBox value={q} onChange={setQ} placeholder="Search customers" />
      <ErrorBanner error={error} />
      {loading && !data ? <Spinner /> : data?.items.length === 0 ? <Empty>No customers yet.</Empty> : (
        <ul className="grid gap-2 md:grid-cols-2">
          {data?.items.map((c) => (
            <li key={c.id}>
              <Link to={`/customers/${c.id}`} className="card flex items-center gap-3">
                <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-brand-50 font-bold text-brand-600 dark:bg-slate-800">{c.code}</span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-semibold">{c.name}</span>
                  <span className="block truncate text-sm text-slate-500">{c.email}</span>
                </span>
                <span className="text-right text-sm">
                  <span className="chip bg-slate-100 dark:bg-slate-800">{c.currency}</span>
                  {!c.active && <div className="mt-1"><Badge status="ended" /></div>}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </Page>
  )
}

const blank: Customer = {
  code: '', name: '', email: '', cc: [], attention: '', billingAddress: '', shippingName: '', shippingAddress: '', phone: '', taxNumber: '',
  currency: '', paymentTerms: { kind: 'days_after', value: 30 }, bankAccountId: '', defaultNotes: '', active: true, version: 0,
}

export function CustomerEditor() {
  const { id } = useParams()
  const nav = useNavigate()
  const { currencies } = useCurrencies()
  const existing = useAsync(() => (id ? unwrap(api.GET('/customers/{id}', { params: { path: { id } } })) : Promise.resolve(undefined)), [id])
  const [c, setC] = useState<Customer>(blank)
  const [cc, setCc] = useState('')
  const banks = useAsync(() => (c.currency ? unwrap(api.GET('/bank-accounts', { params: { query: { currency: c.currency } } })).then((r) => r.items) : Promise.resolve([])), [c.currency])
  const [error, setError] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (existing.data) {
      setC(existing.data)
      setCc(existing.data.cc.join(', '))
    }
  }, [existing.data])
  const set = (patch: Partial<Customer>) => setC({ ...c, ...patch })

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    const body = { ...c, cc: cc.split(/[,;]/).map((s) => s.trim()).filter(Boolean) }
    try {
      if (id) await unwrap(api.PUT('/customers/{id}', { params: { path: { id } }, body }))
      else await unwrap(api.POST('/customers', { body }))
      nav('/customers', { replace: true })
    } catch (err) {
      setError(err as Error)
      setBusy(false)
    }
  }
  const remove = async () => {
    if (!id) return
    try {
      await unwrap(api.DELETE('/customers/{id}', { params: { path: { id } } }))
      nav('/customers', { replace: true })
    } catch (err) {
      setError(err as Error)
    }
  }
  if (id && existing.loading) return <Spinner />
  const termsKind = c.paymentTerms.kind
  return (
    <Page title={id ? c.name || 'Customer' : 'New customer'} back="/customers">
      <form onSubmit={submit} className="space-y-4">
        <ErrorBanner error={error ?? existing.error} />
        <div className="card grid gap-4 md:grid-cols-2">
          <Field label="Name">{(f) => <input id={f} className="input" value={c.name} onChange={(e) => set({ name: e.target.value })} required />}</Field>
          <Field label="Invoice code" hint="Used in invoice numbers, e.g. CD → CD/1195/26">{(f) => <input id={f} className="input uppercase" maxLength={10} value={c.code} onChange={(e) => set({ code: e.target.value.toUpperCase() })} required />}</Field>
          <Field label="E-mail">{(f) => <input id={f} type="email" inputMode="email" className="input" value={c.email} onChange={(e) => set({ email: e.target.value })} required />}</Field>
          <Field label="CC" hint="Comma separated">{(f) => <input id={f} className="input" inputMode="email" value={cc} onChange={(e) => setCc(e.target.value)} />}</Field>
          <Field label="Attention">{(f) => <input id={f} className="input" value={c.attention} onChange={(e) => set({ attention: e.target.value })} />}</Field>
          <Field label="Phone">{(f) => <input id={f} type="tel" className="input" value={c.phone} onChange={(e) => set({ phone: e.target.value })} />}</Field>
        </div>
        <div className="card grid gap-4 md:grid-cols-2">
          <Field label="Currency" hint="Every invoice for this customer is in this currency.">
            {(f) => (
              <select id={f} className="input" value={c.currency} onChange={(e) => set({ currency: e.target.value, bankAccountId: '' })} required>
                <option value="">Choose…</option>
                {currencies.filter((x) => x.active || x.code === c.currency).map((x) => <option key={x.code} value={x.code}>{x.code} — {x.name}</option>)}
              </select>
            )}
          </Field>
          <Field label="Bank account" hint="Defaults to the currency's default account.">
            {(f) => (
              <select id={f} className="input" value={c.bankAccountId} onChange={(e) => set({ bankAccountId: e.target.value })}>
                <option value="">Currency default</option>
                {banks.data?.filter((b) => b.active).map((b) => <option key={b.id} value={b.id}>{b.label} · {b.accountNumber}</option>)}
              </select>
            )}
          </Field>
          <Field label="Payment terms">
            {(f) => (
              <select id={f} className="input" value={termsKind} onChange={(e) => set({ paymentTerms: { kind: e.target.value as Customer['paymentTerms']['kind'], value: e.target.value === 'day_of_next_month' ? 7 : e.target.value === 'days_after' ? 30 : 0 } })}>
                <option value="on_receipt">On receipt</option>
                <option value="days_after">Within N days</option>
                <option value="day_of_next_month">Before day N of next month</option>
              </select>
            )}
          </Field>
          {termsKind !== 'on_receipt' && (
            <Field label={termsKind === 'days_after' ? 'Days' : 'Day of next month'}>
              {(f) => <input id={f} type="number" inputMode="numeric" className="input" min={termsKind === 'days_after' ? 0 : 1} max={termsKind === 'days_after' ? 365 : 31} value={c.paymentTerms.value} onChange={(e) => set({ paymentTerms: { ...c.paymentTerms, value: Number(e.target.value) } })} />}
            </Field>
          )}
        </div>
        <div className="card grid gap-4 md:grid-cols-2">
          <Field label="Billing address">{(f) => <textarea id={f} className="input min-h-[96px]" value={c.billingAddress} onChange={(e) => set({ billingAddress: e.target.value })} />}</Field>
          <div className="space-y-4">
            <Field label="Ship-to name">{(f) => <input id={f} className="input" value={c.shippingName} onChange={(e) => set({ shippingName: e.target.value })} />}</Field>
            <Field label="Shipping address">{(f) => <textarea id={f} className="input min-h-[96px]" value={c.shippingAddress} onChange={(e) => set({ shippingAddress: e.target.value })} />}</Field>
          </div>
          <Field label="Tax number">{(f) => <input id={f} className="input" value={c.taxNumber} onChange={(e) => set({ taxNumber: e.target.value })} />}</Field>
          <label className="flex min-h-[44px] items-center gap-3"><input type="checkbox" className="h-5 w-5" checked={c.active} onChange={(e) => set({ active: e.target.checked })} /> Active</label>
          <div className="md:col-span-2">
            <Field label="Default notes / disclaimer">{(f) => <textarea id={f} className="input min-h-[96px]" value={c.defaultNotes} onChange={(e) => set({ defaultNotes: e.target.value })} />}</Field>
          </div>
        </div>
        <StickyActions>
          {id && <button type="button" className="btn-danger mr-auto" onClick={remove}>Delete</button>}
          <button type="button" className="btn-secondary" onClick={() => nav(-1)}>Cancel</button>
          <button className="btn-primary" disabled={busy}>{busy ? 'Saving…' : 'Save'}</button>
        </StickyActions>
      </form>
    </Page>
  )
}
