import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, unwrap, type AdhocCustomer, type Customer, type Schemas } from '../../lib/api/client'
import { todayISO } from '../../lib/format'
import { useCurrencies } from '../../lib/lookups'
import { useAsync } from '../../lib/useAsync'
import { useCan } from '../../components/auth'
import { LineEditor, newLine, type EditLine } from '../../components/LineEditor'
import { ErrorBanner, Field, Page, SearchBox, Spinner, StickyActions } from '../../components/ui'
import { RichNotesEditor } from '../../components/RichNotesEditor'
import { IconPersonPlus } from '../../components/icons'
import { uuid } from '../../lib/uuid'
import { AdhocSheet, blankAdhoc } from './AdhocCustomers'

type Party = 'customer' | 'adhoc'

/** addDays adds n days to an ISO date (YYYY-MM-DD). */
export function addDays(iso: string, n: number): string {
  const d = new Date(`${iso}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() + n)
  return d.toISOString().slice(0, 10)
}

export function QuoteEditor() {
  const { id } = useParams()
  const [search] = useSearchParams()
  const nav = useNavigate()
  const can = useCan()
  const { minor } = useCurrencies()
  const [party, setParty] = useState<Party>(search.get('adhoc') ? 'adhoc' : 'customer')
  const [query, setQuery] = useState('')
  const customers = useAsync(() => (party === 'customer' ? unwrap(api.GET('/customers', { params: { query: { q: query || undefined, limit: 100 } } })).then((r) => r.items) : Promise.resolve([] as Customer[])), [query, party])
  const adhocs = useAsync(() => (party === 'adhoc' ? unwrap(api.GET('/adhoc-customers', { params: { query: { q: query || undefined, limit: 100 } } })).then((r) => r.items) : Promise.resolve([] as AdhocCustomer[])), [query, party])
  const settings = useAsync(() => unwrap(api.GET('/settings/quotes')).catch(() => undefined), [])
  const business = useAsync(() => unwrap(api.GET('/settings/business')), [])
  const existing = useAsync(() => (id ? unwrap(api.GET('/quotes/{id}', { params: { path: { id } } })) : Promise.resolve(undefined)), [id])
  const [customer, setCustomer] = useState<Customer>()
  const [adhoc, setAdhoc] = useState<AdhocCustomer>()
  const [issueDate, setIssueDate] = useState(todayISO())
  const [validUntil, setValidUntil] = useState('')
  const [salesPerson, setSalesPerson] = useState('')
  const [reference, setReference] = useState('')
  const [notes, setNotes] = useState('')
  const [terms, setTerms] = useState<string | null>(null)
  const [vatPercent, setVatPercent] = useState<string | null>(null)
  const [lines, setLines] = useState<EditLine[]>([newLine()])
  const [adding, setAdding] = useState(false)
  const [error, setError] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)

  // A quote started from an ad-hoc customer's "Quote" button.
  useEffect(() => {
    const a = search.get('adhoc')
    if (!a || id) return
    unwrap(api.GET('/adhoc-customers/{id}', { params: { path: { id: a } } })).then(setAdhoc).catch(() => {})
  }, [search, id])

  useEffect(() => {
    const d = existing.data
    if (!d) return
    const qt = d.quote
    if (qt.adhocCustomerId && !qt.customerId) {
      setParty('adhoc')
      setAdhoc(d.adhocCustomer)
    } else if (qt.customerId) {
      setParty('customer')
      unwrap(api.GET('/customers/{id}', { params: { path: { id: qt.customerId } } })).then(setCustomer).catch(() => {})
    }
    setIssueDate(qt.issueDate)
    setValidUntil(qt.validUntil)
    setSalesPerson(qt.salesPerson)
    setReference(qt.reference)
    setNotes(qt.notes)
    setTerms(qt.terms)
    setVatPercent(qt.vatPercent)
    setLines(qt.lines.map((l) => ({
      key: uuid(), productId: l.productId ?? '', productName: l.description, description: l.description,
      quantity: l.quantity, unitPrice: l.unitPrice, discountPercent: l.discountPercent === '0' ? '' : l.discountPercent,
    })))
  }, [existing.data])

  const validDays = settings.data?.validDays ?? 30
  const shownValidUntil = validUntil || addDays(issueDate, validDays)
  const currency = existing.data?.quote.currency ?? (party === 'customer' ? customer?.currency : adhoc?.currency) ?? ''
  const vat = vatPercent ?? business.data?.vatPercent ?? '0'
  const shownTerms = terms ?? settings.data?.terms ?? ''

  const pickCustomer = (cid: string) => { setCustomer(customers.data?.find((c) => c.id === cid)); resetPrices() }
  const pickAdhoc = (aid: string) => { setAdhoc(adhocs.data?.find((a) => a.id === aid)); resetPrices() }
  // Another party may be in another currency: the products are priced again when saved.
  const resetPrices = () => setLines((cur) => cur.map((l) => ({ ...l, unitPrice: '' })))

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (lines.some((l) => !l.productId)) {
      setError(new Error('Choose a product for every line.'))
      return
    }
    setBusy(true)
    setError(null)
    const cur = existing.data?.quote
    const body: Schemas['QuoteInput'] = {
      ...(cur ? { version: cur.version, billTo: cur.billTo } : party === 'customer' ? { customerId: customer?.id } : { adhocCustomerId: adhoc?.id }),
      issueDate, validUntil: shownValidUntil, salesPerson, reference, notes, terms: shownTerms, vatPercent: vat || '0',
      lines: lines.map((l) => ({ productId: l.productId, quantity: l.quantity, discountPercent: l.discountPercent || '0', ...(l.unitPrice ? { unitPrice: l.unitPrice } : {}) })),
    }
    try {
      const qt = id ? await unwrap(api.PUT('/quotes/{id}', { params: { path: { id } }, body })) : await unwrap(api.POST('/quotes', { body }))
      nav(`/quotes/${qt.id}`, { replace: true })
    } catch (err) {
      setError(err as Error)
      setBusy(false)
    }
  }

  if (id && existing.loading) return <Spinner />
  const chosen = party === 'customer' ? customer : adhoc
  return (
    <Page title={id ? `Edit ${existing.data?.quote.number ?? 'quote'}` : 'New quote'} back={id ? `/quotes/${id}` : '/quotes'}>
      <form onSubmit={submit} className="vstack gap-4">
        <ErrorBanner error={error ?? existing.error} />
        {id && existing.data?.quote.status === 'sent' && (
          <div role="status" className="alert alert-info small mb-0">This quote was sent. Saving a change makes it a draft again: send it again so the customer has the new version.</div>
        )}
        <div className="card card-body vstack gap-3">
          {!id && (
            <div className="btn-group" role="radiogroup" aria-label="Quote for">
              {(['customer', 'adhoc'] as const).map((p) => (
                <button key={p} type="button" role="radio" aria-checked={party === p} className={`btn btn-outline-primary${party === p ? ' active' : ''}`} onClick={() => { setParty(p); setQuery('') }}>
                  {p === 'customer' ? 'Customer' : 'Ad-hoc customer'}
                </button>
              ))}
            </div>
          )}
          <div className="row row-cols-1 row-cols-md-3 g-3">
            <Field label={party === 'customer' ? 'Customer' : 'Ad-hoc customer'} hint={chosen ? `Priced in ${chosen.currency}` : party === 'adhoc' ? 'Someone who is not a customer yet.' : undefined}>
              {(fid) => id ? (
                <input id={fid} className="form-control" value={existing.data?.quote.billTo.name ?? ''} disabled />
              ) : (
                <>
                  <SearchBox value={query} onChange={setQuery} placeholder={party === 'customer' ? 'Search customers' : 'Search ad-hoc customers'} />
                  {party === 'customer' ? (
                    <select id={fid} className="form-select" value={customer?.id ?? ''} onChange={(e) => pickCustomer(e.target.value)} required>
                      <option value="">Choose a customer…</option>
                      {customer && !customers.data?.some((c) => c.id === customer.id) && <option value={customer.id}>{customer.name} ({customer.currency})</option>}
                      {customers.data?.filter((c) => c.active).map((c) => <option key={c.id} value={c.id}>{c.name} ({c.currency})</option>)}
                    </select>
                  ) : (
                    <div className="d-flex gap-2">
                      <select id={fid} className="form-select" value={adhoc?.id ?? ''} onChange={(e) => pickAdhoc(e.target.value)} required>
                        <option value="">Choose…</option>
                        {adhoc && !adhocs.data?.some((a) => a.id === adhoc.id) && <option value={adhoc.id}>{adhoc.name} ({adhoc.currency})</option>}
                        {adhocs.data?.map((a) => <option key={a.id} value={a.id}>{a.name} ({a.currency})</option>)}
                      </select>
                      {can('quotes.create') && <button type="button" className="btn btn-outline-primary text-nowrap" onClick={() => setAdding(true)}><IconPersonPlus width={16} /> New</button>}
                    </div>
                  )}
                </>
              )}
            </Field>
            <Field label="Quote date">{(fid) => <input id={fid} type="date" className="form-control" value={issueDate} onChange={(e) => setIssueDate(e.target.value)} required />}</Field>
            <Field label="Valid until" hint={validUntil ? undefined : `${validDays} days from the quote date (Settings → Quotes).`}>
              {(fid) => <input id={fid} type="date" className="form-control" value={shownValidUntil} min={issueDate} onChange={(e) => setValidUntil(e.target.value)} required />}
            </Field>
          </div>
        </div>
        {currency ? (
          <LineEditor lines={lines} onChange={setLines} currency={currency} minorUnits={minor(currency)} />
        ) : (
          <p className="card card-body small text-body-secondary mb-0">Choose who the quote is for to add products in their currency.</p>
        )}
        <div className="card card-body">
          <div className="row row-cols-1 row-cols-md-3 g-3">
            <Field label="Sales person">{(fid) => <input id={fid} className="form-control" value={salesPerson} onChange={(e) => setSalesPerson(e.target.value)} maxLength={120} />}</Field>
            <Field label="Reference">{(fid) => <input id={fid} className="form-control" value={reference} onChange={(e) => setReference(e.target.value)} maxLength={120} />}</Field>
            <Field label="VAT %" hint="0 = no VAT.">{(fid) => <input id={fid} className="form-control" inputMode="decimal" value={vat} onChange={(e) => setVatPercent(e.target.value)} />}</Field>
          </div>
          <div className="vstack gap-3 mt-3">
            <Field label="Quote notes" hint="Optional; printed on the quote.">
              {(fid) => <RichNotesEditor id={fid} value={notes} onChange={setNotes} minHeight={90} />}
            </Field>
            <Field label="Terms" hint="Printed at the foot of the quote. New quotes start with the terms in Settings → Quotes.">
              {(fid) => <textarea id={fid} className="form-control" style={{ minHeight: 80 }} value={shownTerms} onChange={(e) => setTerms(e.target.value)} maxLength={4000} />}
            </Field>
          </div>
        </div>
        <StickyActions>
          <button type="button" className="btn btn-outline-primary" onClick={() => nav(-1)}>Cancel</button>
          <button className="btn btn-primary" disabled={busy || (!id && !chosen)}>{busy ? 'Saving…' : 'Save quote'}</button>
        </StickyActions>
      </form>
      {adding && <AdhocSheet initial={blankAdhoc()} onClose={() => setAdding(false)} onSaved={(a) => { setAdding(false); setAdhoc(a); adhocs.reload(); resetPrices() }} />}
    </Page>
  )
}
