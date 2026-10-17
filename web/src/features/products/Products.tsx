import { useEffect, useState, type FormEvent } from 'react'
import { Can } from '../../components/Access'
import { useCan } from '../../components/auth'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, unwrap, type Product } from '../../lib/api/client'
import { useCurrencies } from '../../lib/lookups'
import { useAsync } from '../../lib/useAsync'
import { usePaged } from '../../lib/usePaged'
import { Badge, ConfirmButton, Empty, ErrorBanner, ShowMore, Field, Page, SearchBox, Spinner, StickyActions } from '../../components/ui'
import { IconPlus } from '../../components/icons'

export function ProductList() {
  const [q, setQ] = useState('')
  const list = usePaged((offset, limit) => unwrap(api.GET('/products', { params: { query: { q: q || undefined, includeInactive: true, limit, offset } } })), [q])
  const { data, error, loading } = list
  return (
    <Page title="Products" actions={<Can need="products.create"><Link to="/products/new" className="btn-primary"><IconPlus width={16} height={16} /> New product</Link></Can>}>
      <SearchBox value={q} onChange={setQ} placeholder="Search name or SKU" />
      <ErrorBanner error={error} />
      {loading && !data ? <Spinner /> : data?.items.length === 0 ? <Empty>No products yet.</Empty> : (
        <div className="card overflow-x-auto p-0">
          <table className="w-full min-w-[560px] text-sm">
            <thead className="table-head">
              <tr>
                <th className="px-4 py-3">Name</th>
                <th className="px-4 py-3">SKU</th>
                <th className="px-4 py-3">Unit</th>
                <th className="px-4 py-3">Unit cost</th>
                <th className="px-4 py-3">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-cream-200">
              {data?.items.map((p) => (
                <tr key={p.id} className="hover:bg-cream-50">
                  <td className="px-4 py-3 font-medium"><Link to={`/products/${p.id}`} className="text-brand-800 hover:underline">{p.name}</Link></td>
                  <td className="px-4 py-3 text-slate-600">{p.sku}</td>
                  <td className="px-4 py-3 text-slate-600">{p.unit}</td>
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap gap-1">
                      {p.prices.map((pr) => <span key={pr.currency} className="chip bg-brand-50 text-brand-700 ring-1 ring-brand-200">{pr.currency} {pr.unitPrice}</span>)}
                    </div>
                  </td>
                  <td className="px-4 py-3"><Badge status={p.active ? 'active' : 'ended'} label={p.active ? 'Active' : 'Inactive'} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    <ShowMore list={list} />
    </Page>
  )
}

const blank: Product = { sku: '', name: '', description: '', unit: '', prices: [], active: true, version: 0 }

export function ProductEditor() {
  const can = useCan()
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
          <div>
            <h2 className="text-lg font-semibold">Currency &amp; unit cost</h2>
            <p className="text-sm text-slate-500">Choose the currency and enter the unit cost. Add another row to sell the product in more currencies — there is no exchange-rate conversion.</p>
          </div>
          {p.prices.map((pr, i) => (
            <div key={i} className="grid grid-cols-[8rem_minmax(0,1fr)_2.75rem] gap-2">
              <select aria-label={`Currency ${i + 1}`} className="input" value={pr.currency} onChange={(e) => set({ prices: p.prices.map((x, j) => (j === i ? { ...x, currency: e.target.value } : x)) })} required>
                <option value="">Currency…</option>
                {currencies.filter((c) => c.code === pr.currency || (c.active && !p.prices.some((x) => x.currency === c.code))).map((c) => <option key={c.code} value={c.code}>{c.code}{c.active ? '' : ' (inactive)'}</option>)}
              </select>
              <input aria-label={`Unit cost ${i + 1}`} className="input" inputMode="decimal" placeholder="Unit cost, e.g. 192.94" value={pr.unitPrice} onChange={(e) => set({ prices: p.prices.map((x, j) => (j === i ? { ...x, unitPrice: e.target.value } : x)) })} required />
              <button type="button" aria-label={`Remove price ${i + 1}`} className="btn-secondary px-0" disabled={p.prices.length === 1} onClick={() => set({ prices: p.prices.filter((_, j) => j !== i) })}>✕</button>
            </div>
          ))}
          {currencies.length === 0 && <p className="text-sm text-amber-700">Add a currency under Settings first.</p>}
          {currencies.some((c) => c.active && !p.prices.some((x) => x.currency === c.code)) && (
            <button type="button" className="btn-secondary" onClick={() => set({ prices: [...p.prices, { currency: '', unitPrice: '' }] })}>+ Add another currency</button>
          )}
        </div>
        {(id ? can('products.edit') || can('products.delete') : can('products.create')) && (
          <StickyActions>
            {id && can('products.delete') && <span className="mr-auto"><ConfirmButton label="Delete" title="Delete this product?" message="It disappears from the look-up. Invoices already issued keep their lines. If it is still used by a schedule or invoice, it cannot be deleted." confirmLabel="Delete" onConfirm={remove} /></span>}
            <button type="button" className="btn-secondary" onClick={() => nav(-1)}>Cancel</button>
            {(!id || can('products.edit')) && <button className="btn-primary" disabled={busy}>{busy ? 'Saving…' : 'Save'}</button>}
          </StickyActions>
        )}
      </form>
    </Page>
  )
}
