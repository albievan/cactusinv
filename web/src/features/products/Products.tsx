import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, unwrap, type Product } from '../../lib/api/client'
import { useCurrencies } from '../../lib/lookups'
import { useAsync } from '../../lib/useAsync'
import { Empty, ErrorBanner, Field, Page, SearchBox, Spinner, StickyActions } from '../../components/ui'

export function ProductList() {
  const [q, setQ] = useState('')
  const { data, error, loading } = useAsync(() => unwrap(api.GET('/products', { params: { query: { q: q || undefined, includeInactive: true, limit: 200 } } })), [q])
  return (
    <Page title="Products" back="/more" actions={<Link to="/products/new" className="btn-primary">+ New</Link>}>
      <SearchBox value={q} onChange={setQ} placeholder="Search name or SKU" />
      <ErrorBanner error={error} />
      {loading && !data ? <Spinner /> : data?.items.length === 0 ? <Empty>No products yet.</Empty> : (
        <ul className="grid gap-2 md:grid-cols-2">
          {data?.items.map((p) => (
            <li key={p.id}>
              <Link to={`/products/${p.id}`} className="card block">
                <div className="flex justify-between gap-2">
                  <span className="font-semibold">{p.name}</span>
                  <span className="text-xs text-slate-500">{p.sku}</span>
                </div>
                <div className="mt-2 flex flex-wrap gap-1">
                  {p.prices.map((pr) => <span key={pr.currency} className="chip bg-brand-50 text-brand-700 dark:bg-slate-800 dark:text-brand-200">{pr.currency} {pr.unitPrice}</span>)}
                  {p.prices.length === 0 && <span className="text-xs text-amber-600">No prices</span>}
                  {!p.active && <span className="chip bg-slate-200">inactive</span>}
                </div>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </Page>
  )
}

const blank: Product = { sku: '', name: '', description: '', unit: '', prices: [], active: true, version: 0 }

export function ProductEditor() {
  const { id } = useParams()
  const nav = useNavigate()
  const { currencies } = useCurrencies()
  const existing = useAsync(() => (id ? unwrap(api.GET('/products/{id}', { params: { path: { id } } })) : Promise.resolve(undefined)), [id])
  const [p, setP] = useState<Product>(blank)
  const [error, setError] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (existing.data) setP(existing.data) }, [existing.data])
  const set = (patch: Partial<Product>) => setP({ ...p, ...patch })
  const unused = currencies.filter((c) => c.active && !p.prices.some((x) => x.currency === c.code))

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
      <form onSubmit={submit} className="space-y-4">
        <ErrorBanner error={error ?? existing.error} />
        <div className="card grid gap-4 md:grid-cols-2">
          <Field label="Name">{(f) => <input id={f} className="input" value={p.name} onChange={(e) => set({ name: e.target.value })} required />}</Field>
          <Field label="SKU">{(f) => <input id={f} className="input" value={p.sku} onChange={(e) => set({ sku: e.target.value })} required />}</Field>
          <Field label="Unit" hint="e.g. user, hour, month">{(f) => <input id={f} className="input" value={p.unit} onChange={(e) => set({ unit: e.target.value })} />}</Field>
          <label className="flex min-h-[44px] items-center gap-3"><input type="checkbox" className="h-5 w-5" checked={p.active} onChange={(e) => set({ active: e.target.checked })} /> Active</label>
          <div className="md:col-span-2">
            <Field label="Invoice line description">{(f) => <textarea id={f} className="input" value={p.description} onChange={(e) => set({ description: e.target.value })} />}</Field>
          </div>
        </div>
        <div className="card space-y-3">
          <h2 className="font-semibold">Prices</h2>
          <p className="text-sm text-slate-500">One explicit price per currency — there is no exchange-rate conversion.</p>
          {p.prices.map((pr, i) => (
            <div key={pr.currency} className="flex items-center gap-2">
              <span className="chip min-w-[3.5rem] justify-center bg-slate-100 py-2 dark:bg-slate-800">{pr.currency}</span>
              <input aria-label={`Price in ${pr.currency}`} className="input flex-1" inputMode="decimal" value={pr.unitPrice} onChange={(e) => set({ prices: p.prices.map((x, j) => (j === i ? { ...x, unitPrice: e.target.value } : x)) })} required />
              <button type="button" aria-label={`Remove ${pr.currency} price`} className="btn-secondary w-11 px-0" onClick={() => set({ prices: p.prices.filter((_, j) => j !== i) })}>✕</button>
            </div>
          ))}
          {unused.length > 0 && (
            <select aria-label="Add a price in" className="input" value="" onChange={(e) => e.target.value && set({ prices: [...p.prices, { currency: e.target.value, unitPrice: '' }] })}>
              <option value="">+ Add price in…</option>
              {unused.map((c) => <option key={c.code} value={c.code}>{c.code} — {c.name}</option>)}
            </select>
          )}
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
