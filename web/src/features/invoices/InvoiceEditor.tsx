import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, unwrap, type Customer, type Schemas } from '../../lib/api/client'
import { todayISO } from '../../lib/format'
import { useCurrencies } from '../../lib/lookups'
import { useAsync } from '../../lib/useAsync'
import { LineEditor, newLine, type EditLine } from '../../components/LineEditor'
import { ErrorBanner, Field, Page, Spinner, StickyActions } from '../../components/ui'

export function InvoiceEditor() {
  const { id } = useParams()
  const nav = useNavigate()
  const { minor } = useCurrencies()
  const customers = useAsync(() => unwrap(api.GET('/customers', { params: { query: { limit: 200 } } })).then((r) => r.items), [])
  const existing = useAsync(() => (id ? unwrap(api.GET('/invoices/{id}', { params: { path: { id } } })) : Promise.resolve(undefined)), [id])
  const [customerId, setCustomerId] = useState('')
  const [issueDate, setIssueDate] = useState(todayISO())
  const [notes, setNotes] = useState<string | null>(null)
  const [taxPercent, setTaxPercent] = useState('')
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
    setTaxPercent(inv.taxPercent === '0' ? '' : inv.taxPercent)
    setHeader(inv.header)
    setLines(inv.lines.map((l) => ({ key: crypto.randomUUID(), productId: l.productId ?? '', description: l.description, quantity: l.quantity, unitPrice: l.unitPrice, discountPercent: l.discountPercent === '0' ? '' : l.discountPercent })))
  }, [existing.data])

  const customer: Customer | undefined = customers.data?.find((c) => c.id === customerId)
  const currency = existing.data?.invoice.currency ?? customer?.currency ?? ''

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    const body: Schemas['InvoiceInput'] = {
      customerId,
      issueDate,
      header,
      taxPercent: taxPercent || '0',
      notes: notes ?? undefined,
      version: existing.data?.invoice.version,
      lines: lines.map((l) => ({ productId: l.productId || undefined, description: l.description, quantity: l.quantity, unitPrice: l.unitPrice === '' ? undefined : l.unitPrice, discountPercent: l.discountPercent || '0' })),
    }
    try {
      const inv = id
        ? await unwrap(api.PUT('/invoices/{id}', { params: { path: { id } }, body }))
        : await unwrap(api.POST('/invoices', { body }))
      nav(`/invoices/${inv.id}`, { replace: true })
    } catch (err) {
      setError(err as Error)
      setBusy(false)
    }
  }

  if (customers.loading || (id && existing.loading)) return <Spinner />
  return (
    <Page title={id ? 'Edit draft' : 'New invoice'} back={id ? `/invoices/${id}` : '/invoices'}>
      <form onSubmit={submit} className="space-y-4">
        <ErrorBanner error={error ?? customers.error ?? existing.error} />
        <div className="card grid gap-4 md:grid-cols-2">
          <Field label="Customer" hint={customer ? `Billed in ${customer.currency} · ${customer.paymentTerms.text}` : undefined}>
            {(fid) => (
              <select id={fid} className="input" value={customerId} onChange={(e) => setCustomerId(e.target.value)} disabled={!!id} required>
                <option value="">Choose a customer…</option>
                {customers.data?.map((c) => <option key={c.id} value={c.id}>{c.name} ({c.currency})</option>)}
              </select>
            )}
          </Field>
          <Field label="Invoice date">{(fid) => <input id={fid} type="date" className="input" value={issueDate} onChange={(e) => setIssueDate(e.target.value)} required />}</Field>
        </div>
        {currency && (
          <>
            <LineEditor lines={lines} onChange={setLines} currency={currency} minorUnits={minor(currency)} />
            <details className="card">
              <summary className="cursor-pointer font-semibold">More details</summary>
              <div className="mt-3 grid gap-4 md:grid-cols-2">
                <Field label="Sales person">{(fid) => <input id={fid} className="input" value={header.salesPerson ?? ''} onChange={(e) => setHeader({ ...header, salesPerson: e.target.value })} />}</Field>
                <Field label="Job">{(fid) => <input id={fid} className="input" value={header.job ?? ''} onChange={(e) => setHeader({ ...header, job: e.target.value })} />}</Field>
                <Field label="Shipping method">{(fid) => <input id={fid} className="input" value={header.shippingMethod ?? ''} onChange={(e) => setHeader({ ...header, shippingMethod: e.target.value })} />}</Field>
                <Field label="Shipping terms">{(fid) => <input id={fid} className="input" value={header.shippingTerms ?? ''} onChange={(e) => setHeader({ ...header, shippingTerms: e.target.value })} />}</Field>
                <Field label="Delivery date">{(fid) => <input id={fid} type="date" className="input" value={header.deliveryDate ?? ''} onChange={(e) => setHeader({ ...header, deliveryDate: e.target.value || undefined })} />}</Field>
                <Field label="Tax %">{(fid) => <input id={fid} className="input" inputMode="decimal" placeholder="0" value={taxPercent} onChange={(e) => setTaxPercent(e.target.value)} />}</Field>
                <div className="md:col-span-2">
                  <Field label="Notes / disclaimer" hint="Defaults to the customer's notes.">{(fid) => <textarea id={fid} className="input min-h-[96px]" value={notes ?? customer?.defaultNotes ?? ''} onChange={(e) => setNotes(e.target.value)} />}</Field>
                </div>
              </div>
            </details>
          </>
        )}
        <StickyActions>
          <button type="button" className="btn-secondary" onClick={() => nav(-1)}>Cancel</button>
          <button className="btn-primary" disabled={busy || !customerId}>{busy ? 'Saving…' : 'Save draft'}</button>
        </StickyActions>
      </form>
    </Page>
  )
}
