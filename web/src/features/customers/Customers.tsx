import { useEffect, useId, useState, type FormEvent } from 'react'
import { Can } from '../../components/Access'
import { useCan } from '../../components/auth'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, unwrap, type Customer } from '../../lib/api/client'
import { useCurrencies } from '../../lib/lookups'
import { useAsync } from '../../lib/useAsync'
import { usePaged } from '../../lib/usePaged'
import { Badge, ConfirmButton, Empty, ErrorBanner, ShowMore, Field, Page, SearchBox, Spinner, StickyActions } from '../../components/ui'
import { IconDownload, IconPlus, IconSend } from '../../components/icons'
import { uuid } from '../../lib/uuid'

export function CustomerList() {
  const [q, setQ] = useState('')
  const list = usePaged((offset, limit) => unwrap(api.GET('/customers', { params: { query: { q: q || undefined, includeInactive: true, limit, offset } } })), [q])
  const { data, error, loading } = list
  return (
    <Page title="Customers" actions={<Can need="customers.create"><Link to="/customers/new" className="btn btn-primary"><IconPlus width={16} height={16} /> New customer</Link></Can>}>
      <SearchBox value={q} onChange={setQ} placeholder="Search customers" />
      <ErrorBanner error={error} />
      {loading && !data ? <Spinner /> : data?.items.length === 0 ? <Empty>No customers yet.</Empty> : (
        <div className="card overflow-hidden">
          <div className="table-responsive">
            <table className="table table-hover align-middle mb-0 small" style={{ minWidth: 640 }}>
              <thead>
                <tr>
                  <th>Code</th>
                  <th>Name</th>
                  <th>E-mail</th>
                  <th>Currency</th>
                  <th>Payment terms</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {data?.items.map((c) => (
                  <tr key={c.id}>
                    <td className="fw-semibold text-primary">{c.code}</td>
                    <td className="fw-medium">
                      <Link to={`/customers/${c.id}`} className="text-primary-emphasis">{c.name}</Link>
                    </td>
                    <td className="text-body-secondary">{c.email}</td>
                    <td><span className="badge rounded-pill bg-primary-subtle text-primary-emphasis border border-primary-subtle">{c.currency}</span></td>
                    <td className="text-body-secondary">{c.paymentTerms.text}</td>
                    <td><Badge status={c.active ? 'active' : 'ended'} label={c.active ? 'Active' : 'Inactive'} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    <ShowMore list={list} />
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
    <section className="card card-body vstack gap-3">
      <div className="d-flex flex-wrap align-items-center justify-content-between gap-3">
        <div>
          <h2 className="h5">Statement</h2>
          <p className="small text-body-secondary mb-0">Every unpaid invoice as of today.</p>
        </div>
        <div className="text-end">
          <div className="fs-8 text-body-secondary">Balance due</div>
          <div className="fs-4 fw-bold text-primary-emphasis" style={{ fontVariantNumeric: 'tabular-nums' }}>{data.currency} {data.total}</div>
        </div>
      </div>
      <div className="row row-cols-2 row-cols-sm-5 g-2">
        {buckets.map(([label, v], i) => (
          <div key={label}>
            <div className="h-100 rounded-3 bg-body-tertiary border p-3">
              <div className="fs-8 fw-semibold text-uppercase text-body-secondary">{label}</div>
              <div className={`fw-semibold ${i >= 2 && Number(v) > 0 ? 'text-warning-emphasis' : ''}`} style={{ fontVariantNumeric: 'tabular-nums' }}>{v}</div>
            </div>
          </div>
        ))}
      </div>
      <ErrorBanner error={err} />
      <div className="d-flex flex-wrap align-items-center gap-2">
        <a className="btn btn-outline-primary" href={`/api/v1/customers/${customerId}/statement/pdf`} target="_blank" rel="noreferrer"><IconDownload width={16} height={16} /> Statement PDF</a>
        <Can need="statements.send"><button type="button" className="btn btn-primary" onClick={send} disabled={busy}><IconSend width={16} height={16} /> {busy ? 'Queuing…' : 'E-mail statement'}</button></Can>
        <Link className="btn btn-outline-primary" to={`/statements/${customerId}`}>Open full statement</Link>
        <Can need="schedules.create"><Link className="btn btn-outline-primary" to={`/schedules/new?customer=${customerId}&kind=statement`}>Schedule statements…</Link></Can>
        {msg && <span className="small text-success">{msg}</span>}
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
    <div className="vstack gap-2 small">
      {source && (
        <p data-testid="disclaimer-source" className="fw-medium text-primary-emphasis mb-0">
          Its PDFs use {source === 'customer' ? "this customer's own disclaimer" : 'the business disclaimer'}.
        </p>
      )}
      <figure className="rounded-3 bg-body-tertiary border p-3 mb-0">
        <figcaption className="d-flex flex-wrap align-items-center justify-content-between gap-2 fs-8 fw-semibold">
          <span className="text-uppercase text-primary-emphasis">Business disclaimer</span>
          <Link to="/settings/business" className="fw-semibold text-decoration-underline">Edit in Settings</Link>
        </figcaption>
        {error ? (
          <div className="mt-1"><ErrorBanner error={error} /></div>
        ) : (
          <p className="mt-1 mb-0 text-body" style={{ whiteSpace: 'pre-line' }}>{loading ? 'Loading…' : businessText?.trim() || 'None set yet.'}</p>
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
  const can = useCan()
  const activeId = useId()
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
      <form onSubmit={submit} className="vstack gap-3">
        <ErrorBanner error={error ?? existing.error} />
        <div className="card card-body">
          <div className="row row-cols-1 row-cols-md-2 g-3">
            <Field label="Name">{(f) => <input id={f} className="form-control" value={c.name} onChange={(e) => set({ name: e.target.value })} required />}</Field>
            <Field label="Invoice code" hint="Used in invoice numbers, e.g. CD → CD/1195/26">{(f) => <input id={f} className="form-control text-uppercase" maxLength={10} value={c.code} onChange={(e) => set({ code: e.target.value.toUpperCase() })} required />}</Field>
            <Field label="E-mail">{(f) => <input id={f} type="email" inputMode="email" className="form-control" value={c.email} onChange={(e) => set({ email: e.target.value })} required />}</Field>
            <Field label="CC" hint="Comma separated">{(f) => <input id={f} className="form-control" inputMode="email" value={cc} onChange={(e) => setCc(e.target.value)} />}</Field>
            <Field label="Attention">{(f) => <input id={f} className="form-control" value={c.attention} onChange={(e) => set({ attention: e.target.value })} />}</Field>
            <Field label="Phone">{(f) => <input id={f} type="tel" className="form-control" value={c.phone} onChange={(e) => set({ phone: e.target.value })} />}</Field>
          </div>
        </div>
        <div className="card card-body">
          <div className="row row-cols-1 row-cols-md-2 g-3">
            <Field label="Currency" hint="Every invoice for this customer is in this currency.">
              {(f) => (
                <select id={f} className="form-select" value={c.currency} onChange={(e) => set({ currency: e.target.value, bankAccountId: '' })} required>
                  <option value="">Choose…</option>
                  {currencies.filter((x) => x.active || x.code === c.currency).map((x) => <option key={x.code} value={x.code}>{x.code} — {x.name}</option>)}
                </select>
              )}
            </Field>
            <Field label="Bank account" hint="Defaults to the currency's default account.">
              {(f) => (
                <select id={f} className="form-select" value={c.bankAccountId} onChange={(e) => set({ bankAccountId: e.target.value })}>
                  <option value="">Currency default</option>
                  {banks.data?.filter((b) => b.active || b.id === c.bankAccountId).map((b) => <option key={b.id} value={b.id}>{b.label} · {b.accountNumber}{b.active ? '' : ' (inactive: invoices use the currency default)'}</option>)}
                </select>
              )}
            </Field>
            <Field label="Payment terms">
              {(f) => (
                <select id={f} className="form-select" value={termsKind} onChange={(e) => set({ paymentTerms: { kind: e.target.value as Customer['paymentTerms']['kind'], value: e.target.value === 'day_of_next_month' ? 7 : e.target.value === 'days_after' ? 30 : 0 } })}>
                  <option value="on_receipt">On receipt</option>
                  <option value="days_after">Within N days</option>
                  <option value="day_of_next_month">Before day N of next month</option>
                </select>
              )}
            </Field>
            {termsKind !== 'on_receipt' && (
              <Field label={termsKind === 'days_after' ? 'Days' : 'Day of next month'}>
                {(f) => <input id={f} type="number" inputMode="numeric" className="form-control" min={termsKind === 'days_after' ? 0 : 1} max={termsKind === 'days_after' ? 365 : 31} value={c.paymentTerms.value} onChange={(e) => set({ paymentTerms: { ...c.paymentTerms, value: Number(e.target.value) } })} />}
              </Field>
            )}
          </div>
        </div>
        <div className="card card-body">
          <div className="row row-cols-1 row-cols-md-2 g-3">
            <Field label="Billing address">{(f) => <textarea id={f} className="form-control" style={{ minHeight: 96 }} value={c.billingAddress} onChange={(e) => set({ billingAddress: e.target.value })} />}</Field>
            <div className="vstack gap-3">
              <Field label="Ship-to name">{(f) => <input id={f} className="form-control" value={c.shippingName} onChange={(e) => set({ shippingName: e.target.value })} />}</Field>
              <Field label="Shipping address">{(f) => <textarea id={f} className="form-control" style={{ minHeight: 96 }} value={c.shippingAddress} onChange={(e) => set({ shippingAddress: e.target.value })} />}</Field>
            </div>
            <Field label="Tax number">{(f) => <input id={f} className="form-control" value={c.taxNumber} onChange={(e) => set({ taxNumber: e.target.value })} />}</Field>
            <div className="d-flex align-items-center" style={{ minHeight: 44 }}>
              <div className="form-check mb-0">
                <input id={activeId} type="checkbox" className="form-check-input" checked={c.active} onChange={(e) => set({ active: e.target.checked })} />
                <label htmlFor={activeId} className="form-check-label">Active</label>
              </div>
            </div>
            <div className="vstack gap-2 w-100">
              <Field label="Disclaimer for this customer" hint="Empty: the business disclaimer below is used. Printed at the foot of every invoice and statement PDF for this customer, read fresh each time.">
                {(f) => <textarea id={f} className="form-control" style={{ minHeight: 96 }} value={c.defaultNotes} placeholder={business.data?.defaultNotes ?? ''} onChange={(e) => set({ defaultNotes: e.target.value })} />}
              </Field>
              <DisclaimerInUse source={existing.data?.disclaimerSource} businessText={business.data?.defaultNotes} loading={business.loading} error={business.error} />
            </div>
          </div>
        </div>
        {id && <Can need="statements.view"><StatementPanel customerId={id} /></Can>}
        {(id ? can('customers.edit') || can('customers.delete') : can('customers.create')) && (
          <StickyActions>
            {id && can('customers.delete') && <span className="me-auto"><ConfirmButton label="Delete" title="Delete this customer?" message="Its details are deleted. A customer with invoices or schedules cannot be deleted; make them inactive instead." confirmLabel="Delete" onConfirm={remove} /></span>}
            <button type="button" className="btn btn-outline-primary" onClick={() => nav(-1)}>Cancel</button>
            {(!id || can('customers.edit')) && <button className="btn btn-primary" disabled={busy}>{busy ? 'Saving…' : 'Save'}</button>}
          </StickyActions>
        )}
      </form>
    </Page>
  )
}
