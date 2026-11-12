import { useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Can } from '../../components/Access'
import { useCan } from '../../components/auth'
import { api, unwrap, type AdhocCustomer, type Schemas } from '../../lib/api/client'
import { useCurrencies } from '../../lib/lookups'
import { usePaged } from '../../lib/usePaged'
import { ConfirmButton, Empty, ErrorBanner, Field, Page, SearchBox, Sheet, ShowMore, Spinner } from '../../components/ui'
import { IconEdit, IconPersonPlus, IconPlus, IconTrash } from '../../components/icons'

export const blankAdhoc = (currency = ''): AdhocCustomer => ({ name: '', email: '', attention: '', address: '', phone: '', taxNumber: '', currency, version: 0 })

/** AdhocCustomers is /quotes/adhoc-customers: prospects quoted before they are customers. */
export function AdhocCustomers() {
  const [q, setQ] = useState('')
  const [all, setAll] = useState(false)
  const [editing, setEditing] = useState<AdhocCustomer>()
  const [converting, setConverting] = useState<AdhocCustomer>()
  const [notice, setNotice] = useState('')
  const [err, setErr] = useState<Error | null>(null)
  const nav = useNavigate()
  const list = usePaged((offset, limit) => unwrap(api.GET('/adhoc-customers', { params: { query: { q: q || undefined, includeConverted: all || undefined, limit, offset } } })), [q, all])
  const { data, error, loading, reload } = list
  const done = (msg: string) => { setErr(null); setNotice(msg); reload() }
  return (
    <Page title="Ad-hoc customers" back="/quotes" actions={<Can need="quotes.create"><button className="btn btn-primary" onClick={() => setEditing(blankAdhoc())}><IconPlus width={16} /> Add</button></Can>}>
      <p className="small text-body-secondary">People you quote before they are customers. When a quote turns into a sale, make them a customer: their quotes move to the new customer and can be turned into invoices.</p>
      <SearchBox value={q} onChange={setQ} placeholder="Search name or e-mail" />
      <div className="form-check mb-3">
        <input id="adhoc-all" className="form-check-input" type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} />
        <label htmlFor="adhoc-all" className="form-check-label small">Also show those already made customers</label>
      </div>
      <ErrorBanner error={err ?? error} />
      {notice && <div role="status" className="mb-3 rounded-3 border-start border-4 border-primary bg-white p-3 small fw-medium text-primary-emphasis shadow-sm">{notice}</div>}
      {loading && !data ? <Spinner /> : data && data.items.length === 0 ? <Empty>No ad-hoc customers.</Empty> : (
        <ul className="list-unstyled vstack gap-2 mb-0">
          {data?.items.map((a) => (
            <li key={a.id} className="card card-body">
              <div className="d-flex flex-wrap align-items-start justify-content-between gap-2">
                <div className="min-w-0">
                  <div className="fw-semibold">{a.name} <span className="small text-body-secondary">· {a.currency}</span></div>
                  <div className="small text-body-secondary text-truncate">{[a.attention && `Att: ${a.attention}`, a.email, a.phone].filter(Boolean).join(' · ') || 'No contact details yet'}</div>
                  {a.customerId && <div className="small mt-1">Now a customer — <Link to={`/customers/${a.customerId}`}>open</Link></div>}
                </div>
                {!a.customerId && (
                  <div className="d-flex flex-wrap gap-2">
                    <Link to={`/quotes/new?adhoc=${a.id}`} className="btn btn-sm btn-outline-primary">Quote</Link>
                    <Can need="quotes.edit"><button className="btn btn-sm btn-outline-primary" onClick={() => setEditing(a)}><IconEdit width={14} /> Edit</button></Can>
                    <Can need="customers.create"><button className="btn btn-sm btn-primary" onClick={() => setConverting(a)}><IconPersonPlus width={14} /> Make customer</button></Can>
                    <Can need="quotes.delete">
                      <ConfirmButton
                        label={<IconTrash width={14} />}
                        className="btn btn-sm btn-outline-danger"
                        title={`Delete ${a.name}?`}
                        message="Only an ad-hoc customer no quote was made out to can be deleted."
                        confirmLabel="Delete"
                        onConfirm={async () => {
                          try { await unwrap(api.DELETE('/adhoc-customers/{id}', { params: { path: { id: a.id! } } })); done(`${a.name} deleted.`) } catch (x) { setErr(x as Error) }
                        }}
                      />
                    </Can>
                  </div>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      <ShowMore list={list} />
      {editing && <AdhocSheet initial={editing} onClose={() => setEditing(undefined)} onSaved={(a) => { setEditing(undefined); done(`${a.name} saved.`) }} />}
      {converting && <ConvertSheet adhoc={converting} onClose={() => setConverting(undefined)} onDone={(c) => { setConverting(undefined); done(`${c.name} is now customer ${c.code}.`); nav(`/customers/${c.id}`) }} />}
    </Page>
  )
}

/** AdhocSheet adds or edits an ad-hoc customer. */
export function AdhocSheet({ initial, onClose, onSaved }: { initial: AdhocCustomer; onClose: () => void; onSaved: (a: AdhocCustomer) => void }) {
  const { currencies } = useCurrencies()
  const [a, setA] = useState(initial)
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const set = (patch: Partial<AdhocCustomer>) => setA({ ...a, ...patch })
  const save = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setErr(null)
    try {
      const out = a.id
        ? await unwrap(api.PUT('/adhoc-customers/{id}', { params: { path: { id: a.id } }, body: a }))
        : await unwrap(api.POST('/adhoc-customers', { body: a }))
      onSaved(out)
    } catch (x) {
      setErr(x as Error)
      setBusy(false)
    }
  }
  const active = currencies.filter((c) => c.active)
  return (
    <Sheet title={a.id ? `Edit ${initial.name}` : 'New ad-hoc customer'} open onClose={onClose}>
      <form onSubmit={save} className="vstack gap-3">
        <ErrorBanner error={err} />
        <Field label="Name">{(f) => <input id={f} className="form-control" value={a.name} onChange={(e) => set({ name: e.target.value })} required maxLength={200} />}</Field>
        <div className="row row-cols-1 row-cols-sm-2 g-3">
          <Field label="Attention">{(f) => <input id={f} className="form-control" value={a.attention} onChange={(e) => set({ attention: e.target.value })} />}</Field>
          <Field label="Currency" hint={a.id ? 'Fixed once they have a quote.' : 'Their quotes are priced in it.'}>
            {(f) => (
              <select id={f} className="form-select" value={a.currency} onChange={(e) => set({ currency: e.target.value })} required>
                <option value="">Choose…</option>
                {active.map((c) => <option key={c.code} value={c.code}>{c.code} · {c.name}</option>)}
              </select>
            )}
          </Field>
          <Field label="E-mail (optional)" hint="Needed to e-mail a quote, or type it when sending.">{(f) => <input id={f} type="email" className="form-control" value={a.email} onChange={(e) => set({ email: e.target.value })} />}</Field>
          <Field label="Phone">{(f) => <input id={f} className="form-control" value={a.phone} onChange={(e) => set({ phone: e.target.value })} />}</Field>
        </div>
        <Field label="Address">{(f) => <textarea id={f} className="form-control" style={{ minHeight: 80 }} value={a.address} onChange={(e) => set({ address: e.target.value })} />}</Field>
        <Field label="VAT number">{(f) => <input id={f} className="form-control" value={a.taxNumber} onChange={(e) => set({ taxNumber: e.target.value })} />}</Field>
        <button className="btn btn-primary w-100" disabled={busy}>{busy ? 'Saving…' : 'Save'}</button>
      </form>
    </Sheet>
  )
}

type Terms = Schemas['PaymentTerms']

/** ConvertSheet makes a customer of an ad-hoc customer: what a customer needs that a prospect does not have. */
export function ConvertSheet({ adhoc, onClose, onDone }: { adhoc: AdhocCustomer; onClose: () => void; onDone: (c: Schemas['Customer']) => void }) {
  const can = useCan()
  const [code, setCode] = useState('')
  const [email, setEmail] = useState(adhoc.email)
  const [terms, setTerms] = useState<Terms>({ kind: 'days_after', value: 30, text: '' })
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setErr(null)
    try {
      onDone(await unwrap(api.POST('/adhoc-customers/{id}/convert', { params: { path: { id: adhoc.id! } }, body: { code, email, paymentTerms: terms } })))
    } catch (x) {
      setErr(x as Error)
      setBusy(false)
    }
  }
  return (
    <Sheet title={`Make ${adhoc.name} a customer`} open onClose={onClose}>
      <form onSubmit={submit} className="vstack gap-3">
        <p className="small text-body-secondary mb-0">A customer is made with {adhoc.name}'s details, billed in {adhoc.currency}. Their quotes move to the customer and can then be turned into invoices.</p>
        <ErrorBanner error={err} />
        <Field label="Invoice code" hint="1–10 letters or digits, e.g. PR in PR/1196/26; not used by another customer.">
          {(f) => <input id={f} className="form-control text-uppercase" value={code} onChange={(e) => setCode(e.target.value.toUpperCase())} required maxLength={10} pattern="[A-Za-z0-9]{1,10}" />}
        </Field>
        <Field label="E-mail" hint="Invoices are e-mailed here.">{(f) => <input id={f} type="email" className="form-control" value={email} onChange={(e) => setEmail(e.target.value)} required />}</Field>
        <div className="row row-cols-2 g-3">
          <Field label="Payment terms">
            {(f) => (
              <select id={f} className="form-select" value={terms.kind} onChange={(e) => setTerms({ ...terms, kind: e.target.value as Terms['kind'], value: e.target.value === 'on_receipt' ? 0 : terms.value || 30 })}>
                <option value="on_receipt">On receipt</option>
                <option value="days_after">Days after invoice</option>
                <option value="day_of_next_month">Day of next month</option>
              </select>
            )}
          </Field>
          {terms.kind !== 'on_receipt' && (
            <Field label={terms.kind === 'days_after' ? 'Days' : 'Day'}>
              {(f) => <input id={f} type="number" min={1} max={terms.kind === 'days_after' ? 365 : 31} className="form-control" value={terms.value} onChange={(e) => setTerms({ ...terms, value: Number(e.target.value) })} />}
            </Field>
          )}
        </div>
        <button className="btn btn-primary w-100" disabled={busy || !can('customers.create')}>{busy ? 'Making customer…' : 'Make customer'}</button>
      </form>
    </Sheet>
  )
}
