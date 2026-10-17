import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, unwrap, type Customer, type Schemas } from '../../lib/api/client'
import { todayISO } from '../../lib/format'
import { useCurrencies } from '../../lib/lookups'
import { useAsync } from '../../lib/useAsync'
import { LineEditor, newLine, type EditLine } from '../../components/LineEditor'
import { ErrorBanner, Field, Page, SearchBox, Spinner, StickyActions } from '../../components/ui'
import { uuid } from '../../lib/uuid'

export function InvoiceEditor() {
  const { id } = useParams()
  const nav = useNavigate()
  const { minor } = useCurrencies()
  // The customer list is searched on the server: it used to stop at the first 200.
  const [customerQuery, setCustomerQuery] = useState('')
  const customerPage = useAsync(() => unwrap(api.GET('/customers', { params: { query: { q: customerQuery || undefined, limit: 100 } } })), [customerQuery])
  const [picked, setPicked] = useState<Customer>()
  const customers = { ...customerPage, data: customerPage.data && (picked && !customerPage.data.items.some((c) => c.id === picked.id) ? [picked, ...customerPage.data.items] : customerPage.data.items) }
  const business = useAsync(() => unwrap(api.GET('/settings/business')), [])
  const existing = useAsync(() => (id ? unwrap(api.GET('/invoices/{id}', { params: { path: { id } } })) : Promise.resolve(undefined)), [id])
  const [customerId, setCustomerId] = useState('')
  const [issueDate, setIssueDate] = useState(todayISO())
  const [notes, setNotes] = useState('')
  const [vatPercent, setVatPercent] = useState<string | null>(null)
  const [header, setHeader] = useState<Schemas['InvoiceHeader']>({})
  const [lines, setLines] = useState<EditLine[]>([newLine()])
  const [error, setError] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    const inv = existing.data?.invoice
    if (!inv) return
    setCustomerId(inv.customerId)
    // The draft's customer, also when it is not among the first page of the list.
    unwrap(api.GET('/customers/{id}', { params: { path: { id: inv.customerId } } })).then(setPicked).catch(() => {})
    setIssueDate(inv.issueDate)
    setNotes(inv.notes)
    setVatPercent(inv.vatPercent)
    setHeader(inv.header)
    setLines(
      inv.lines.map((l) => ({
        key: uuid(), productId: l.productId ?? '', productName: l.description, description: l.description,
        quantity: l.quantity, unitPrice: l.unitPrice, discountPercent: l.discountPercent === '0' ? '' : l.discountPercent,
      })),
    )
  }, [existing.data])

  const customer: Customer | undefined = customers.data?.find((c) => c.id === customerId)
  const currency = existing.data?.invoice.currency ?? customer?.currency ?? ''
  // A different customer may bill in another currency: every chosen product is priced again
  // in it, and a product with no price there is cleared so the user picks another.
  const changeCustomer = (next: string) => {
    const chosen = customers.data?.find((c) => c.id === next)
    const nextCur = chosen?.currency ?? ''
    setPicked(chosen)
    setCustomerId(next)
    if (!nextCur || nextCur === currency) return
    const picked = lines.filter((l) => l.productId)
    if (picked.length === 0) return
    // The old prices are in the old currency: cleared at once, so a save before the new
    // ones arrive lets the server price the lines instead of reading them in the new one.
    setLines((cur) => cur.map((l) => ({ ...l, unitPrice: '' })))
    void Promise.all(picked.map((l) => unwrap(api.GET('/products/{id}', { params: { path: { id: l.productId } } })).catch(() => null))).then((products) =>
      setLines((cur) =>
        cur.map((l) => {
          if (!l.productId) return l
          const price = products.find((p) => p?.id === l.productId)?.prices.find((x) => x.currency === nextCur)?.unitPrice
          return price ? { ...l, unitPrice: price } : { ...l, productId: '', productName: '', description: '', unitPrice: '' }
        }),
      ),
    )
  }
  const vat = vatPercent ?? business.data?.vatPercent ?? '0'

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (lines.some((l) => !l.productId)) {
      setError(new Error('Choose a product for every line.'))
      return
    }
    setBusy(true)
    setError(null)
    const inv = existing.data?.invoice
    // Lines keep the unit price shown (the draft's own, or the product's when it was picked),
    // and an edited draft keeps its parties and terms: otherwise saving any change re-priced
    // the lines and reset them to the customer's current defaults.
    const body: Schemas['InvoiceInput'] = {
      customerId, issueDate, header, notes, vatPercent: vat || '0', version: inv?.version,
      ...(inv ? { billTo: inv.billTo, shipTo: inv.shipTo, paymentTerms: inv.paymentTerms } : {}),
      lines: lines.map((l) => ({ productId: l.productId, quantity: l.quantity, discountPercent: l.discountPercent || '0', ...(l.unitPrice ? { unitPrice: l.unitPrice } : {}) })),
    }
    try {
      const inv = id ? await unwrap(api.PUT('/invoices/{id}', { params: { path: { id } }, body })) : await unwrap(api.POST('/invoices', { body }))
      nav(`/invoices/${inv.id}`, { replace: true })
    } catch (err) {
      setError(err as Error)
      setBusy(false)
    }
  }

  if ((customers.loading && !customers.data) || (id && existing.loading)) return <Spinner />
  return (
    <Page title={id ? 'Edit draft' : 'New invoice'} back={id ? `/invoices/${id}` : '/invoices'}>
      <form onSubmit={submit} className="space-y-5">
        <ErrorBanner error={error ?? customers.error ?? existing.error} />
        <div className="card grid gap-4 md:grid-cols-3">
          <Field
            label="Customer"
            hint={customer ? `Billed in ${customer.currency} · ${customer.paymentTerms.text}` : (customerPage.data?.total ?? 0) > (customerPage.data?.items.length ?? 0) ? 'Showing the first 100: search to find others.' : undefined}
          >
            {(fid) => (
              <>
              {!id && <SearchBox value={customerQuery} onChange={setCustomerQuery} placeholder="Search customers" />}
              <select id={fid} className="input" value={customerId} onChange={(e) => changeCustomer(e.target.value)} disabled={!!id} required>
                <option value="">Choose a customer…</option>
                {customers.data?.map((c) => <option key={c.id} value={c.id}>{c.name} ({c.currency})</option>)}
              </select>
              </>
            )}
          </Field>
          <Field label="Invoice date">{(fid) => <input id={fid} type="date" className="input" value={issueDate} onChange={(e) => setIssueDate(e.target.value)} required />}</Field>
          <Field label="VAT %" hint="0 = no VAT; nothing about VAT is printed.">
            {(fid) => <input id={fid} className="input" inputMode="decimal" value={vat} onChange={(e) => setVatPercent(e.target.value)} />}
          </Field>
        </div>
        {currency ? (
          <LineEditor lines={lines} onChange={setLines} currency={currency} minorUnits={minor(currency)} />
        ) : (
          <p className="card text-sm text-slate-500">Choose a customer to add products in their currency.</p>
        )}
        <details className="card">
          <summary className="cursor-pointer font-semibold text-brand-900">More details</summary>
          <div className="mt-4 grid gap-4 md:grid-cols-2">
            <Field label="Sales person">{(fid) => <input id={fid} className="input" value={header.salesPerson ?? ''} onChange={(e) => setHeader({ ...header, salesPerson: e.target.value })} />}</Field>
            <Field label="Job">{(fid) => <input id={fid} className="input" value={header.job ?? ''} onChange={(e) => setHeader({ ...header, job: e.target.value })} />}</Field>
            <Field label="Shipping method">{(fid) => <input id={fid} className="input" value={header.shippingMethod ?? ''} onChange={(e) => setHeader({ ...header, shippingMethod: e.target.value })} />}</Field>
            <Field label="Shipping terms">{(fid) => <input id={fid} className="input" value={header.shippingTerms ?? ''} onChange={(e) => setHeader({ ...header, shippingTerms: e.target.value })} />}</Field>
            <Field label="Delivery date">{(fid) => <input id={fid} type="date" className="input" value={header.deliveryDate ?? ''} onChange={(e) => setHeader({ ...header, deliveryDate: e.target.value || undefined })} />}</Field>
            <div className="md:col-span-2">
              <Field label="Invoice notes" hint="Optional. The disclaimer is added to every PDF automatically from the customer or business settings.">
                {(fid) => <textarea id={fid} className="input min-h-[80px]" value={notes} onChange={(e) => setNotes(e.target.value)} />}
              </Field>
            </div>
          </div>
        </details>
        <StickyActions>
          <button type="button" className="btn-secondary" onClick={() => nav(-1)}>Cancel</button>
          <button className="btn-primary" disabled={busy || !customerId}>{busy ? 'Saving…' : 'Save draft'}</button>
        </StickyActions>
      </form>
    </Page>
  )
}
