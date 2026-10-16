import { useEffect, useState, type FormEvent } from 'react'
import { Can } from '../../components/Access'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, unwrap, type Customer } from '../../lib/api/client'
import { useCurrencies } from '../../lib/lookups'
import { useAsync } from '../../lib/useAsync'
import { Badge, ConfirmButton, Empty, ErrorBanner, Field, Page, SearchBox, Spinner, StickyActions } from '../../components/ui'
import { IconDownload, IconPlus, IconSend } from '../../components/icons'
import { uuid } from '../../lib/uuid'

export function CustomerList() {
  const [q, setQ] = useState('')
  const { data, error, loading } = useAsync(() => unwrap(api.GET('/customers', { params: { query: { q: q || undefined, includeInactive: true, limit: 200 } } })), [q])
  return (
    <Page title="Customers" actions={<Can need="customers.edit"><Link to="/customers/new" className="btn-primary"><IconPlus width={16} height={16} /> New customer</Link></Can>}>
      <SearchBox value={q} onChange={setQ} placeholder="Search customers" />
      <ErrorBanner error={error} />
      {loading && !data ? <Spinner /> : data?.items.length === 0 ? <Empty>No customers yet.</Empty> : (
        <div className="card overflow-x-auto p-0">
          <table className="w-full min-w-[640px] text-sm">
            <thead className="table-head">
              <tr>
                <th className="px-4 py-3">Code</th>
                <th className="px-4 py-3">Name</th>
                <th className="px-4 py-3">E-mail</th>
                <th className="px-4 py-3">Currency</th>
                <th className="px-4 py-3">Payment terms</th>
                <th className="px-4 py-3">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-cream-200">
              {data?.items.map((c) => (
                <tr key={c.id} className="hover:bg-cream-50">
                  <td className="px-4 py-3 font-semibold text-brand-700">{c.code}</td>
                  <td className="px-4 py-3 font-medium">
                    <Link to={`/customers/${c.id}`} className="text-brand-800 hover:underline">{c.name}</Link>
                  </td>
                  <td className="px-4 py-3 text-slate-600">{c.email}</td>
                  <td className="px-4 py-3"><span className="chip bg-brand-50 text-brand-700 ring-1 ring-brand-200">{c.currency}</span></td>
                  <td className="px-4 py-3 text-slate-600">{c.paymentTerms.text}</td>
                  <td className="px-4 py-3"><Badge status={c.active ? 'active' : 'ended'} label={c.active ? 'Active' : 'Inactive'} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Page>
  )
}

/** StatementPanel shows a customer's balance and aging, with download and e-mail. */
function StatementPanel({ customerId }: { customerId: string }) {
  const { data, error, reload } = useAsync(() => unwrap(api.GET('/customers/{id}/statement', { params: { path: { id: customerId } } })), [customerId])
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const send = async () => {
    if (busy) return
    setBusy(true)
    setErr(null)
    setMsg('')
    try {
      await unwrap(api.POST('/customers/{id}/statement/send', { params: { path: { id: customerId }, header: { 'Idempotency-Key': uuid() } }, body: {} }))
      setMsg('Statement queued for e-mail.')
      reload()
    } catch (e) {
      setErr(e as Error)
    } finally {
      setBusy(false)
    }
  }
  if (!data) return error ? <ErrorBanner error={error} /> : null
  const buckets: [string, string][] = [
    ['Current', data.aging.current], ['1–30 days', data.aging.days1to30], ['31–60 days', data.aging.days31to60],
    ['61–90 days', data.aging.days61to90], ['90+ days', data.aging.over90],
  ]
  return (
    <section className="card space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold">Statement</h2>
          <p className="text-sm text-slate-500">Every unpaid invoice as of today.</p>
        </div>
        <div className="text-right">
          <div className="text-xs text-slate-500">Balance due</div>
          <div className="text-2xl font-bold tabular-nums text-brand-900">{data.currency} {data.total}</div>
        </div>
      </div>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
        {buckets.map(([label, v], i) => (
          <div key={label} className="rounded-xl bg-cream-50 p-3 ring-1 ring-cream-200">
            <div className="text-[11px] font-semibold uppercase tracking-wide text-slate-500">{label}</div>
            <div className={`font-semibold tabular-nums ${i >= 2 && Number(v) > 0 ? 'text-amber-700' : ''}`}>{v}</div>
          </div>
        ))}
      </div>
      <ErrorBanner error={err} />
      <div className="flex flex-wrap items-center gap-2">
        <a className="btn-secondary" href={`/api/v1/customers/${customerId}/statement/pdf`} target="_blank" rel="noreferrer"><IconDownload width={16} height={16} /> Statement PDF</a>
        <Can need="statements.send"><button type="button" className="btn-primary" onClick={send} disabled={busy}><IconSend width={16} height={16} /> {busy ? 'Queuing…' : 'E-mail statement'}</button></Can>
        <Link className="btn-secondary" to={`/statements/${customerId}`}>Open full statement</Link>
        <Can need="schedules.edit"><Link className="btn-secondary" to={`/schedules/new?customer=${customerId}&kind=statement`}>Schedule statements…</Link></Can>
        {msg && <span className="text-sm text-green-700">{msg}</span>}
      </div>
    </section>
  )
}

/**
 * DisclaimerInUse shows the business disclaimer the customer falls back to and, for a saved
 * customer, which disclaimer its PDFs print — as the server reports it (disclaimerSource).
 */
function DisclaimerInUse({ source, businessText, loading, error }: { source?: Customer['disclaimerSource']; businessText?: string; loading: boolean; error?: Error }) {
  return (
    <div className="space-y-2 text-sm">
      {source && (
        <p data-testid="disclaimer-source" className="font-medium text-brand-900">
          Its PDFs use {source === 'customer' ? "this customer's own disclaimer" : 'the business disclaimer'}.
        </p>
      )}
      <figure className="rounded-xl bg-cream-50 p-3 ring-1 ring-cream-200">
        <figcaption className="flex flex-wrap items-center justify-between gap-2 text-xs font-semibold uppercase tracking-wide text-brand-800">
          Business disclaimer
          <Link to="/settings/business" className="font-semibold normal-case tracking-normal text-brand-700 underline">Edit in Settings</Link>
        </figcaption>
        {error ? (
          <div className="mt-1"><ErrorBanner error={error} /></div>
        ) : (
          <p className="mt-1 whitespace-pre-line text-slate-700">{loading ? 'Loading…' : businessText?.trim() || 'None set yet.'}</p>
        )}
      </figure>
    </div>
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
  const business = useAsync(() => unwrap(api.GET('/settings/business')), [])
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
          <div className="space-y-2 md:col-span-2">
            <Field label="Disclaimer for this customer" hint="Empty: the business disclaimer below is used. Printed at the foot of every invoice and statement PDF for this customer, read fresh each time.">
              {(f) => <textarea id={f} className="input min-h-[96px]" value={c.defaultNotes} placeholder={business.data?.defaultNotes ?? ''} onChange={(e) => set({ defaultNotes: e.target.value })} />}
            </Field>
            <DisclaimerInUse source={existing.data?.disclaimerSource} businessText={business.data?.defaultNotes} loading={business.loading} error={business.error} />
          </div>
        </div>
        {id && <Can need="statements.view"><StatementPanel customerId={id} /></Can>}
        <Can need="customers.edit">
          <StickyActions>
            {id && <span className="mr-auto"><ConfirmButton label="Delete" title="Delete this customer?" message="Its details are deleted. A customer with invoices or schedules cannot be deleted; make them inactive instead." confirmLabel="Delete" onConfirm={remove} /></span>}
            <button type="button" className="btn-secondary" onClick={() => nav(-1)}>Cancel</button>
            <button className="btn-primary" disabled={busy}>{busy ? 'Saving…' : 'Save'}</button>
          </StickyActions>
        </Can>
      </form>
    </Page>
  )
}
