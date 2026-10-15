import { useState } from 'react'
import { api, unwrap, type Product } from '../lib/api/client'
import { fromMinor, lineAmount, toMinor } from '../lib/money'
import { useAsync } from '../lib/useAsync'
import { Sheet } from './ui'

export interface EditLine {
  key: string
  productId: string
  description: string
  quantity: string
  /** '' means "use the product's price" (only allowed with a product). */
  unitPrice: string
  discountPercent: string
}

export const newLine = (): EditLine => ({ key: crypto.randomUUID(), productId: '', description: '', quantity: '1', unitPrice: '', discountPercent: '' })

/** LineEditor edits invoice or schedule lines, with a product look-up priced in currency. */
export function LineEditor({ lines, onChange, currency, minorUnits, allowListPrice }: {
  lines: EditLine[]
  onChange: (l: EditLine[]) => void
  currency: string
  minorUnits: number
  allowListPrice?: boolean
}) {
  const [pickFor, setPickFor] = useState<string | null>(null)
  const update = (key: string, patch: Partial<EditLine>) => onChange(lines.map((l) => (l.key === key ? { ...l, ...patch } : l)))
  let total = 0n
  let valid = true
  for (const l of lines) {
    const a = l.unitPrice === '' ? null : lineAmount(l, minorUnits)
    if (a === null) valid = false
    else total += a
  }
  return (
    <div className="space-y-3">
      {lines.map((l, i) => {
        const amount = l.unitPrice === '' ? null : lineAmount(l, minorUnits)
        return (
          <div key={l.key} className="card space-y-2">
            <div className="flex items-center justify-between">
              <span className="text-xs font-semibold uppercase text-slate-500">Line {i + 1}</span>
              <div className="flex gap-1">
                <button type="button" className="btn-secondary min-h-[36px] px-3 text-xs" onClick={() => setPickFor(l.key)}>
                  {l.productId ? 'Change product' : 'Pick product'}
                </button>
                <button type="button" aria-label={`Remove line ${i + 1}`} className="btn-secondary min-h-[36px] px-3 text-xs" onClick={() => onChange(lines.filter((x) => x.key !== l.key))} disabled={lines.length === 1}>
                  ✕
                </button>
              </div>
            </div>
            <textarea aria-label="Description" className="input min-h-[64px]" placeholder={allowListPrice ? 'Description (may use {month}, {year}, {period})' : 'Description'} value={l.description} onChange={(e) => update(l.key, { description: e.target.value })} />
            <div className="grid grid-cols-3 gap-2">
              <label className="text-xs text-slate-500">Qty
                <input className="input" inputMode="decimal" value={l.quantity} onChange={(e) => update(l.key, { quantity: e.target.value })} />
              </label>
              <label className="text-xs text-slate-500">Unit price
                <input className="input" inputMode="decimal" placeholder={l.productId && allowListPrice ? 'List price' : '0.00'} value={l.unitPrice} onChange={(e) => update(l.key, { unitPrice: e.target.value })} />
              </label>
              <label className="text-xs text-slate-500">Disc %
                <input className="input" inputMode="decimal" placeholder="0" value={l.discountPercent} onChange={(e) => update(l.key, { discountPercent: e.target.value })} />
              </label>
            </div>
            <div className="text-right text-sm tabular-nums text-slate-600 dark:text-slate-300">
              {amount !== null ? `${currency} ${fromMinor(amount, minorUnits)}` : l.productId && allowListPrice ? 'Priced at run time' : '—'}
            </div>
          </div>
        )
      })}
      <div className="flex items-center justify-between">
        <button type="button" className="btn-secondary" onClick={() => onChange([...lines, newLine()])}>
          + Add line
        </button>
        <div className="text-right">
          <div className="text-xs text-slate-500">Preview (server computes the final total)</div>
          <div className="text-lg font-bold tabular-nums">{valid ? `${currency} ${fromMinor(total, minorUnits)}` : '—'}</div>
        </div>
      </div>
      <ProductPicker
        open={pickFor !== null}
        currency={currency}
        onClose={() => setPickFor(null)}
        onPick={(p) => {
          const price = p.prices.find((x) => x.currency === currency)?.unitPrice ?? ''
          const m = toMinor(price, minorUnits)
          update(pickFor!, {
            productId: p.id ?? '',
            description: p.description || p.name,
            unitPrice: allowListPrice ? '' : m === null ? price : fromMinor(m, minorUnits).replace(/,/g, ''),
          })
          setPickFor(null)
        }}
      />
    </div>
  )
}

function ProductPicker({ open, currency, onClose, onPick }: { open: boolean; currency: string; onClose: () => void; onPick: (p: Product) => void }) {
  const [q, setQ] = useState('')
  const { data, loading } = useAsync(
    () => (open ? unwrap(api.GET('/products', { params: { query: { q: q || undefined, currency, limit: 50 } } })) : Promise.resolve(undefined)),
    [open, q, currency],
  )
  return (
    <Sheet title={`Products priced in ${currency}`} open={open} onClose={onClose}>
      <input type="search" className="input mb-3" placeholder="Search name or SKU" aria-label="Search products" value={q} onChange={(e) => setQ(e.target.value)} autoFocus />
      {loading && <p className="text-sm text-slate-500">Loading…</p>}
      <ul className="space-y-2">
        {data?.items.map((p) => (
          <li key={p.id}>
            <button type="button" className="card flex w-full items-center justify-between text-left" onClick={() => onPick(p)}>
              <span>
                <span className="block font-medium">{p.name}</span>
                <span className="text-xs text-slate-500">{p.sku}</span>
              </span>
              <span className="tabular-nums">{currency} {p.prices.find((x) => x.currency === currency)?.unitPrice}</span>
            </button>
          </li>
        ))}
        {data && data.items.length === 0 && <li className="text-sm text-slate-500">No products have a {currency} price. Add one under Products.</li>}
      </ul>
    </Sheet>
  )
}
