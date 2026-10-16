import { useEffect, useId, useRef, useState } from 'react'
import { api, unwrap, type Product } from '../lib/api/client'
import { fromMinor, lineAmount } from '../lib/money'
import { uuid } from '../lib/uuid'

export interface EditLine {
  key: string
  productId: string
  productName: string
  /** From the product — not editable (no free-text lines). */
  description: string
  quantity: string
  /** The unit price shown and used for the preview: the fixed price if any, else the product's. */
  unitPrice: string
  discountPercent: string
  /** Schedules: the product's state as the server read it; inactive or missing means choose another. */
  productStatus?: 'active' | 'inactive' | 'missing'
  /** Schedules: a fixed unit price stored on the line (kept on save unless another product is chosen). */
  priceOverride?: string
}

/** unavailable: the line's product was deactivated or no longer exists. */
const unavailable = (l: EditLine) => l.productStatus === 'inactive' || l.productStatus === 'missing'

export const newLine = (): EditLine => ({ key: uuid(), productId: '', productName: '', description: '', quantity: '1', unitPrice: '', discountPercent: '' })

/** LineEditor edits invoice or schedule lines. Every line is a product from the look-up. */
export function LineEditor({ lines, onChange, currency, minorUnits }: {
  lines: EditLine[]
  onChange: (l: EditLine[]) => void
  currency: string
  minorUnits: number
}) {
  const update = (key: string, patch: Partial<EditLine>) => onChange(lines.map((l) => (l.key === key ? { ...l, ...patch } : l)))
  let total = 0n
  let problem = ''
  for (const l of lines) {
    if (!l.productId || l.productStatus === 'missing') {
      problem = 'Choose a product for every line'
      break
    }
    const a = lineAmount(l, minorUnits)
    if (a !== null) total += a
    else if (!l.unitPrice) problem ||= `No ${currency} price for a product — choose another`
    else problem ||= 'Check the quantities and discounts'
  }
  return (
    <div className="card space-y-3 p-0">
      <div className="hidden grid-cols-[minmax(0,1fr)_6rem_8rem_6rem_8rem_2.5rem] gap-3 rounded-t-2xl px-4 py-2 md:grid table-head">
        <span>Product</span>
        <span className="text-right">Qty</span>
        <span className="text-right">Unit price</span>
        <span className="text-right">Disc %</span>
        <span className="text-right">Amount</span>
        <span />
      </div>
      <ul className="divide-y divide-cream-200">
        {lines.map((l, i) => {
          const amount = l.productId && l.productStatus !== 'missing' ? lineAmount(l, minorUnits) : null
          return (
            <li key={l.key} className="grid gap-3 px-4 py-3 md:grid-cols-[minmax(0,1fr)_6rem_8rem_6rem_8rem_2.5rem] md:items-start">
              <div>
                <ProductCombobox
                  currency={currency}
                  value={l.productName}
                  label={`Product for line ${i + 1}`}
                  onPick={(p) => {
                    const price = p.prices.find((x) => x.currency === currency)?.unitPrice ?? ''
                    // A fixed price belongs to its product: kept when the same product is chosen again.
                    const keep = p.id === l.productId && l.priceOverride !== undefined
                    update(l.key, {
                      productId: p.id ?? '', productName: p.name, description: p.description || p.name, productStatus: p.active ? 'active' : 'inactive',
                      unitPrice: keep ? (l.priceOverride ?? price) : price, priceOverride: keep ? l.priceOverride : undefined,
                    })
                  }}
                />
                {l.description && l.description !== l.productName && <p className="mt-1 text-xs text-slate-500">{l.description}</p>}
                {unavailable(l) && <p role="alert" className="mt-1 text-xs font-medium text-red-700">{l.productStatus === 'inactive' ? `${l.productName}: product` : 'Product'} no longer available — choose another.</p>}
                {l.productId && !unavailable(l) && !l.unitPrice && <p role="alert" className="mt-1 text-xs font-medium text-red-700">No {currency} price for this product — choose another product.</p>}
              </div>
              <label className="text-xs text-slate-500 md:text-right">
                <span className="md:sr-only">Qty</span>
                <input className="input text-right" inputMode="decimal" aria-label={`Quantity for line ${i + 1}`} value={l.quantity} onChange={(e) => update(l.key, { quantity: e.target.value })} />
              </label>
              <div className="text-sm md:pt-2.5 md:text-right">
                <span className="text-xs text-slate-500 md:hidden">Unit price </span>
                <span className="tabular-nums">{l.unitPrice ? `${l.unitPrice}` : '—'}</span>
                {l.priceOverride !== undefined && <span className="block text-xs text-slate-500">fixed price</span>}
              </div>
              <label className="text-xs text-slate-500">
                <span className="md:sr-only">Disc %</span>
                <input className="input text-right" inputMode="decimal" placeholder="0" aria-label={`Discount for line ${i + 1}`} value={l.discountPercent} onChange={(e) => update(l.key, { discountPercent: e.target.value })} />
              </label>
              <div className="text-sm font-semibold tabular-nums md:pt-2.5 md:text-right">{amount !== null ? fromMinor(amount, minorUnits) : '—'}</div>
              <button type="button" aria-label={`Remove line ${i + 1}`} className="btn-secondary h-10 w-10 px-0 md:h-[44px]" onClick={() => onChange(lines.filter((x) => x.key !== l.key))} disabled={lines.length === 1}>
                ✕
              </button>
            </li>
          )
        })}
      </ul>
      <div className="flex items-center justify-between rounded-b-2xl border-t border-cream-200 px-4 py-3">
        <button type="button" className="btn-secondary" onClick={() => onChange([...lines, newLine()])}>
          + Add line
        </button>
        <div className="text-right">
          <div className="text-xs text-slate-500">Preview — the server computes the final total</div>
          <div className="text-lg font-bold tabular-nums text-brand-900">{problem || `${currency} ${fromMinor(total, minorUnits)}`}</div>
        </div>
      </div>
    </div>
  )
}

/** ProductCombobox searches every product; only those priced in currency can be chosen. */
export function ProductCombobox({ currency, value, label, onPick }: { currency: string; value: string; label: string; onPick: (p: Product) => void }) {
  const [q, setQ] = useState(value)
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<Product[]>([])
  const [active, setActive] = useState(0)
  const listId = useId()
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => setQ(value), [value])
  useEffect(() => {
    if (!open) return
    const t = setTimeout(() => {
      unwrap(api.GET('/products', { params: { query: { q: q || undefined, limit: 25 } } }))
        .then((r) => {
          setItems(r.items)
          setActive(0)
        })
        .catch(() => setItems([]))
    }, 150)
    return () => clearTimeout(t)
  }, [q, open])
  useEffect(() => {
    const close = (e: MouseEvent) => box.current && !box.current.contains(e.target as Node) && setOpen(false)
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [])

  const priced = (p: Product) => p.prices.find((x) => x.currency === currency)
  const choose = (p: Product) => {
    if (!priced(p)) return
    onPick(p)
    setQ(p.name)
    setOpen(false)
  }
  return (
    <div ref={box} className="relative">
      <input
        role="combobox"
        aria-label={label}
        aria-expanded={open}
        aria-controls={listId}
        aria-autocomplete="list"
        className="input"
        placeholder="Search products by name or SKU…"
        value={q}
        onFocus={() => setOpen(true)}
        onChange={(e) => {
          setQ(e.target.value)
          setOpen(true)
        }}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown') {
            e.preventDefault()
            setOpen(true)
            setActive((a) => Math.min(a + 1, items.length - 1))
          } else if (e.key === 'ArrowUp') {
            e.preventDefault()
            setActive((a) => Math.max(a - 1, 0))
          } else if (e.key === 'Enter' && open && items[active]) {
            e.preventDefault()
            choose(items[active]!)
          } else if (e.key === 'Escape') {
            setOpen(false)
            setQ(value)
          }
        }}
      />
      {open && (
        <div className="absolute z-40 mt-1 w-full min-w-[min(36rem,calc(100vw-2rem))] overflow-hidden rounded-xl bg-white shadow-lg ring-1 ring-cream-300">
          <div className="grid grid-cols-[minmax(0,1fr)_7rem_4.5rem_7.5rem] gap-3 px-3 py-2 table-head" aria-hidden="true">
            <span>Product</span>
            <span>SKU</span>
            <span>Unit</span>
            <span className="text-right">Unit price</span>
          </div>
          <ul id={listId} role="listbox" aria-label="Products" className="max-h-72 divide-y divide-cream-100 overflow-y-auto">
            {items.length === 0 && <li className="px-3 py-3 text-sm text-slate-500">No products found. Add one under Products.</li>}
            {items.map((p, i) => {
              const price = priced(p)
              return (
                <li
                  key={p.id}
                  role="option"
                  aria-selected={i === active}
                  aria-disabled={!price}
                  onMouseEnter={() => setActive(i)}
                  onMouseDown={(e) => {
                    e.preventDefault()
                    choose(p)
                  }}
                  className={`grid cursor-pointer grid-cols-[minmax(0,1fr)_7rem_4.5rem_7.5rem] items-center gap-3 px-3 py-2.5 text-sm ${i === active ? 'bg-brand-50' : ''} ${price ? '' : 'cursor-not-allowed opacity-50'}`}
                >
                  <span className="min-w-0">
                    <span className="block break-words font-medium text-brand-900">{p.name}</span>
                    {p.description && p.description !== p.name && <span className="block break-words text-xs text-slate-500">{p.description}</span>}
                  </span>
                  <span className="break-all text-xs text-slate-600">{p.sku}</span>
                  <span className="break-words text-xs text-slate-600">{p.unit || '—'}</span>
                  <span className="text-right tabular-nums">{price ? `${currency} ${price.unitPrice}` : <span className="text-xs">No {currency} price</span>}</span>
                </li>
              )
            })}
          </ul>
        </div>
      )}
    </div>
  )
}
