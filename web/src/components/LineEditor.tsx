import { useEffect, useId, useRef, useState } from 'react'
import { api, unwrap, type Product } from '../lib/api/client'
import { fromMinor, lineAmount } from '../lib/money'
import { uuid } from '../lib/uuid'
import { IconClose, IconPlus } from './icons'

const tnum = { fontVariantNumeric: 'tabular-nums' } as const
const lineColumns = 'minmax(0,1fr) 6rem 8rem 6rem 8rem 2.5rem'
const productColumns = 'minmax(0,1fr) 7rem 4.5rem 7.5rem'

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
    <div className="card">
      <div className="d-none d-md-grid gap-3 rounded-top px-3 py-2 bg-body-secondary fs-8 fw-semibold text-uppercase text-primary-emphasis" style={{ gridTemplateColumns: lineColumns }}>
        <span>Product</span>
        <span className="text-end">Qty</span>
        <span className="text-end">Unit price</span>
        <span className="text-end">Disc %</span>
        <span className="text-end">Amount</span>
        <span />
      </div>
      <ul className="list-group list-group-flush">
        {lines.map((l, i) => {
          const amount = l.productId && l.productStatus !== 'missing' ? lineAmount(l, minorUnits) : null
          return (
            <li key={l.key} className="list-group-item d-flex flex-column d-md-grid gap-3 align-items-md-start px-3 py-3" style={{ gridTemplateColumns: lineColumns }}>
              <div className="min-w-0">
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
                {l.description && l.description !== l.productName && <p className="mt-1 mb-0 fs-8 text-body-secondary">{l.description}</p>}
                {unavailable(l) && <p role="alert" className="mt-1 mb-0 fs-8 fw-medium text-danger">{l.productStatus === 'inactive' ? `${l.productName}: product` : 'Product'} no longer available — choose another.</p>}
                {l.productId && !unavailable(l) && !l.unitPrice && <p role="alert" className="mt-1 mb-0 fs-8 fw-medium text-danger">No {currency} price for this product — choose another product.</p>}
              </div>
              <label className="fs-8 text-body-secondary text-md-end">
                <span className="d-md-none">Qty</span>
                <input className="form-control text-end" inputMode="decimal" aria-label={`Quantity for line ${i + 1}`} value={l.quantity} onChange={(e) => update(l.key, { quantity: e.target.value })} />
              </label>
              <div className="small pt-md-2 text-md-end">
                <span className="fs-8 text-body-secondary d-md-none">Unit price </span>
                <span style={tnum}>{l.unitPrice ? `${l.unitPrice}` : '—'}</span>
                {l.priceOverride !== undefined && <span className="d-block fs-8 text-body-secondary">fixed price</span>}
              </div>
              <label className="fs-8 text-body-secondary">
                <span className="d-md-none">Disc %</span>
                <input className="form-control text-end" inputMode="decimal" placeholder="0" aria-label={`Discount for line ${i + 1}`} value={l.discountPercent} onChange={(e) => update(l.key, { discountPercent: e.target.value })} />
              </label>
              <div className="small fw-semibold pt-md-2 text-md-end" style={tnum}>{amount !== null ? fromMinor(amount, minorUnits) : '—'}</div>
              <button type="button" aria-label={`Remove line ${i + 1}`} title={`Remove line ${i + 1}`} className="btn btn-outline-primary btn-icon" onClick={() => onChange(lines.filter((x) => x.key !== l.key))} disabled={lines.length === 1}>
                <IconClose width={14} />
              </button>
            </li>
          )
        })}
      </ul>
      <div className="d-flex align-items-center justify-content-between gap-2 border-top px-3 py-3">
        <button type="button" className="btn btn-outline-primary" onClick={() => onChange([...lines, newLine()])}>
          <IconPlus width={16} /> Add line
        </button>
        <div className="text-end">
          <div className="fs-8 text-body-secondary">Preview — the server computes the final total</div>
          <div className="fs-5 fw-bold text-primary-emphasis" style={tnum}>{problem || `${currency} ${fromMinor(total, minorUnits)}`}</div>
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
  const [failed, setFailed] = useState(false)
  const seq = useRef(0)
  const [active, setActive] = useState(0)
  const listId = useId()
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => setQ(value), [value])
  useEffect(() => {
    if (!open) return
    const t = setTimeout(() => {
      // Only the newest search may fill the list: a slow older answer is dropped.
      const mine = ++seq.current
      unwrap(api.GET('/products', { params: { query: { q: q || undefined, limit: 25 } } }))
        .then((r) => {
          if (mine !== seq.current) return
          setItems(r.items)
          setFailed(false)
          setActive(0)
        })
        .catch(() => {
          if (mine !== seq.current) return
          setItems([])
          setFailed(true)
        })
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
    <div ref={box} className="position-relative">
      <input
        role="combobox"
        aria-label={label}
        aria-expanded={open}
        aria-controls={listId}
        aria-autocomplete="list"
        className="form-control"
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
        <div className="position-absolute mt-1 w-100 overflow-hidden rounded-3 bg-white shadow border" style={{ zIndex: 1000, minWidth: 'min(36rem, calc(100vw - 2rem))' }}>
          <div className="d-none d-sm-grid gap-3 px-3 py-2 bg-body-secondary fs-8 fw-semibold text-uppercase text-primary-emphasis" style={{ gridTemplateColumns: productColumns }} aria-hidden="true">
            <span>Product</span>
            <span>SKU</span>
            <span>Unit</span>
            <span className="text-end">Unit price</span>
          </div>
          <ul id={listId} role="listbox" aria-label="Products" className="list-group list-group-flush overflow-y-auto" style={{ maxHeight: '18rem' }}>
            {items.length === 0 && (
              <li className="list-group-item px-3 py-3 small text-body-secondary">{failed ? 'The products could not be loaded. Check your connection and try again.' : 'No products found. Add one under Products.'}</li>
            )}
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
                  className={`list-group-item d-flex d-sm-grid align-items-center gap-3 px-3 py-2 small ${i === active ? 'bg-primary-subtle' : ''} ${price ? '' : 'opacity-50'}`}
                  style={{ gridTemplateColumns: productColumns, cursor: price ? 'pointer' : 'not-allowed' }}
                >
                  <span className="min-w-0 flex-grow-1">
                    <span className="d-block text-break fw-medium text-primary-emphasis">{p.name}</span>
                    {p.description && p.description !== p.name && <span className="d-block text-break fs-8 text-body-secondary">{p.description}</span>}
                    {/* On a phone the SKU and unit sit under the name; the columns need ~22rem. */}
                    <span className="d-block d-sm-none text-break fs-8 text-body-secondary">{[p.sku, p.unit].filter(Boolean).join(' · ')}</span>
                  </span>
                  <span className="d-none d-sm-block text-break fs-8 text-body-secondary">{p.sku}</span>
                  <span className="d-none d-sm-block text-break fs-8 text-body-secondary">{p.unit || '—'}</span>
                  <span className="text-end flex-shrink-0" style={tnum}>{price ? `${currency} ${price.unitPrice}` : <span className="fs-8">No {currency} price</span>}</span>
                </li>
              )
            })}
          </ul>
        </div>
      )}
    </div>
  )
}
