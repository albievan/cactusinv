import { useEffect, useId, useState, type FormEvent } from 'react'
import { Can } from '../../components/Access'
import { useCan } from '../../components/auth'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, unwrap, type Product } from '../../lib/api/client'
import { useCurrencies } from '../../lib/lookups'
import { useAsync } from '../../lib/useAsync'
import { usePaged } from '../../lib/usePaged'
import { Badge, ConfirmButton, Empty, ErrorBanner, ShowMore, Field, Page, SearchBox, Spinner, StickyActions } from '../../components/ui'
import { IconClose, IconPlus } from '../../components/icons'

export function ProductList() {
  const [q, setQ] = useState('')
  const list = usePaged((offset, limit) => unwrap(api.GET('/products', { params: { query: { q: q || undefined, includeInactive: true, limit, offset } } })), [q])
  const { data, error, loading } = list
  return (
    <Page title="Products" actions={<Can need="products.create"><Link to="/products/new" className="btn btn-primary"><IconPlus width={16} height={16} /> New product</Link></Can>}>
      <SearchBox value={q} onChange={setQ} placeholder="Search name or SKU" />
      <ErrorBanner error={error} />
      {loading && !data ? <Spinner /> : data?.items.length === 0 ? <Empty>No products yet.</Empty> : (
        <div className="card overflow-hidden">
          <div className="table-responsive">
            <table className="table table-hover align-middle mb-0 small" style={{ minWidth: 560 }}>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>SKU</th>
                  <th>Unit</th>
                  <th>Unit cost</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {data?.items.map((p) => (
                  <tr key={p.id}>
                    <td className="fw-medium"><Link to={`/products/${p.id}`} className="text-primary-emphasis">{p.name}</Link></td>
                    <td className="text-body-secondary">{p.sku}</td>
                    <td className="text-body-secondary">{p.unit}</td>
                    <td>
                      <div className="d-flex flex-wrap gap-1">
                        {p.prices.map((pr) => <span key={pr.currency} className="badge rounded-pill bg-primary-subtle text-primary-emphasis border border-primary-subtle">{pr.currency} {pr.unitPrice}</span>)}
                      </div>
                    </td>
                    <td><Badge status={p.active ? 'active' : 'ended'} label={p.active ? 'Active' : 'Inactive'} /></td>
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

const blank: Product = { sku: '', name: '', description: '', unit: '', prices: [], active: true, version: 0 }

export function ProductEditor() {
  const can = useCan()
  const activeId = useId()
  const { id } = useParams()
  const nav = useNavigate()
  const { currencies } = useCurrencies()
  const existing = useAsync(() => (id ? unwrap(api.GET('/products/{id}', { params: { path: { id } } })) : Promise.resolve(undefined)), [id])
  const [p, setP] = useState<Product>(blank)
  const [error, setError] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (existing.data) setP(existing.data) }, [existing.data])
  // A new product starts with one currency/unit-cost row in the first active currency.
  useEffect(() => {
    if (!id && currencies.length > 0) {
      setP((cur) => (cur.prices.length ? cur : { ...cur, prices: [{ currency: currencies.find((c) => c.active)?.code ?? '', unitPrice: '' }] }))
    }
  }, [id, currencies])
  const set = (patch: Partial<Product>) => setP({ ...p, ...patch })

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      if (id) await unwrap(api.PUT('/products/{id}', { params: { path: { id } }, body: p }))
      else await unwrap(api.POST('/products', { body: p }))
      nav('/products', { replace: true })
    } catch (err) {
      setError(err as Error)
      setBusy(false)
    }
  }
  const remove = async () => {
    if (!id) return
    try {
      await unwrap(api.DELETE('/products/{id}', { params: { path: { id } } }))
      nav('/products', { replace: true })
    } catch (err) {
      setError(err as Error)
    }
  }
  if (id && existing.loading) return <Spinner />
  return (
    <Page title={id ? p.name || 'Product' : 'New product'} back="/products">
      <form onSubmit={submit} className="vstack gap-3">
        <ErrorBanner error={error ?? existing.error} />
        <div className="card card-body">
          <div className="row row-cols-1 row-cols-md-2 g-3">
            <Field label="Name">{(f) => <input id={f} className="form-control" value={p.name} onChange={(e) => set({ name: e.target.value })} required />}</Field>
            <Field label="SKU">{(f) => <input id={f} className="form-control" value={p.sku} onChange={(e) => set({ sku: e.target.value })} required />}</Field>
            <Field label="Unit" hint="e.g. user, hour, month">{(f) => <input id={f} className="form-control" value={p.unit} onChange={(e) => set({ unit: e.target.value })} />}</Field>
            <div className="d-flex align-items-center" style={{ minHeight: 44 }}>
              <div className="form-check mb-0">
                <input id={activeId} type="checkbox" className="form-check-input" checked={p.active} onChange={(e) => set({ active: e.target.checked })} />
                <label htmlFor={activeId} className="form-check-label">Active</label>
              </div>
            </div>
            <div className="w-100">
              <Field label="Invoice line description">{(f) => <textarea id={f} className="form-control" value={p.description} onChange={(e) => set({ description: e.target.value })} />}</Field>
            </div>
          </div>
        </div>
        <div className="card card-body vstack gap-3">
          <div>
            <h2 className="h5">Currency &amp; unit cost</h2>
            <p className="small text-body-secondary mb-0">Choose the currency and enter the unit cost. Add another row to sell the product in more currencies — there is no exchange-rate conversion.</p>
          </div>
          {p.prices.map((pr, i) => (
            <div key={i} className="d-flex align-items-center gap-2">
              <select aria-label={`Currency ${i + 1}`} className="form-select flex-shrink-0" style={{ width: '8rem' }} value={pr.currency} onChange={(e) => set({ prices: p.prices.map((x, j) => (j === i ? { ...x, currency: e.target.value } : x)) })} required>
                <option value="">Currency…</option>
                {currencies.filter((c) => c.code === pr.currency || (c.active && !p.prices.some((x) => x.currency === c.code))).map((c) => <option key={c.code} value={c.code}>{c.code}{c.active ? '' : ' (inactive)'}</option>)}
              </select>
              <input aria-label={`Unit cost ${i + 1}`} className="form-control flex-grow-1 min-w-0" inputMode="decimal" placeholder="Unit cost, e.g. 192.94" value={pr.unitPrice} onChange={(e) => set({ prices: p.prices.map((x, j) => (j === i ? { ...x, unitPrice: e.target.value } : x)) })} required />
              <button type="button" aria-label={`Remove price ${i + 1}`} title="Remove price" className="btn btn-sm btn-outline-danger btn-icon flex-shrink-0" disabled={p.prices.length === 1} onClick={() => set({ prices: p.prices.filter((_, j) => j !== i) })}><IconClose width={16} /></button>
            </div>
          ))}
          {currencies.length === 0 && <p className="small text-warning-emphasis mb-0">Add a currency under Settings first.</p>}
          {currencies.some((c) => c.active && !p.prices.some((x) => x.currency === c.code)) && (
            <button type="button" className="btn btn-outline-primary align-self-start" onClick={() => set({ prices: [...p.prices, { currency: '', unitPrice: '' }] })}><IconPlus width={16} /> Add another currency</button>
          )}
        </div>
        {(id ? can('products.edit') || can('products.delete') : can('products.create')) && (
          <StickyActions>
            {id && can('products.delete') && <span className="me-auto"><ConfirmButton label="Delete" title="Delete this product?" message="It disappears from the look-up. Invoices already issued keep their lines. If it is still used by a schedule or invoice, it cannot be deleted." confirmLabel="Delete" onConfirm={remove} /></span>}
            <button type="button" className="btn btn-outline-primary" onClick={() => nav(-1)}>Cancel</button>
            {(!id || can('products.edit')) && <button className="btn btn-primary" disabled={busy}>{busy ? 'Saving…' : 'Save'}</button>}
          </StickyActions>
        )}
      </form>
    </Page>
  )
}
