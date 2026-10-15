import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, unwrap, type Customer, type Schemas } from '../../lib/api/client'
import { todayISO } from '../../lib/format'
import { useCurrencies } from '../../lib/lookups'
import { useAsync } from '../../lib/useAsync'
import { LineEditor, newLine, type EditLine } from '../../components/LineEditor'
import { ErrorBanner, Field, Page, Spinner, StickyActions } from '../../components/ui'
import { uuid } from '../../lib/uuid'

export function InvoiceEditor() {
  const { id } = useParams()
  const nav = useNavigate()
  const { minor } = useCurrencies()
  const customers = useAsync(() => unwrap(api.GET('/customers', { params: { query: { limit: 200 } } })).then((r) => r.items), [])
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
  const vat = vatPercent ?? business.data?.vatPercent ?? '0'

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (lines.some((l) => !l.productId)) {
      setError(new Error('Choose a product for every line.'))
      return
    }
    setBusy(true)
    setError(null)
    const body: Schemas['InvoiceInput'] = {
      customerId, issueDate, header, notes, vatPercent: vat || '0', version: existing.data?.invoice.version,
      lines: lines.map((l) => ({ productId: l.productId, quantity: l.quantity, discountPercent: l.discountPercent || '0' })),
    }
    try {
      const inv = id ? await unwrap(api.PUT('/invoices/{id}', { params: { path: { id } }, body })) : await unwrap(api.POST('/invoices', { body }))
      nav(`/invoices/${inv.id}`, { replace: true })
    } catch (err) {
      setError(err as Error)
      setBusy(false)
    }
  }

  if (customers.loading || (id && existing.loading)) return <Spinner />
  return (
    <Page title={id ? 'Edit draft' : 'New invoice'} back={id ? `/invoices/${id}` : '/invoices'}>
      <form onSubmit={submit} className="space-y-5">
        <ErrorBanner error={error ?? customers.error ?? existing.error} />
        <div className="card grid gap-4 md:grid-cols-3">
          <Field label="Customer" hint={customer ? `Billed in ${customer.currency} · ${customer.paymentTerms.text}` : undefined}>
            {(fid) => (
              <select id={fid} className="input" value={customerId} onChange={(e) => setCustomerId(e.target.value)} disabled={!!id} required>
                <option value="">Choose a customer…</option>
                {customers.data?.map((c) => <option key={c.id} value={c.id}>{c.name} ({c.currency})</option>)}
              </select>
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
