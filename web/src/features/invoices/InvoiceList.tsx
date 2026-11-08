import { useState } from 'react'
import { Can } from '../../components/Access'
import { Link, useSearchParams } from 'react-router-dom'
import { api, unwrap, type Invoice } from '../../lib/api/client'
import { formatDate } from '../../lib/format'
import { formatMoney } from '../../lib/money'
import { usePaged } from '../../lib/usePaged'
import { Badge, Empty, ErrorBanner, ShowMore, Page, SearchBox, Spinner } from '../../components/ui'
import { IconPlus } from '../../components/icons'
import { InvoiceActions } from '../../components/InvoiceActions'

const tnum = { fontVariantNumeric: 'tabular-nums' } as const

const filters = [
  { key: 'all', label: 'All' },
  { key: 'draft', label: 'Draft' },
  { key: 'issued', label: 'Issued' },
  { key: 'sent', label: 'Emailed' },
  { key: 'overdue', label: 'Overdue' },
  { key: 'paid', label: 'Paid' },
  { key: 'void', label: 'Void' },
] as const
type Filter = (typeof filters)[number]['key']

export function InvoiceList() {
  const [params, setParams] = useSearchParams()
  const [filter, setFilter] = useState<Filter>('all')
  const q = params.get('q') ?? ''
  const setQ = (v: string) => setParams(v ? { q: v } : {}, { replace: true })
  const [notice, setNotice] = useState('')
  const [actionError, setActionError] = useState<Error | null>(null)
  const list = usePaged(
    (offset, limit) =>
      unwrap(
        api.GET('/invoices', {
          params: {
            query: {
              q: q || undefined,
              status: filter === 'all' || filter === 'overdue' ? undefined : filter,
              overdue: filter === 'overdue' ? true : undefined,
              limit,
              offset,
            },
          },
        }),
      ),
    [filter, q],
  )
  const { data, error, loading, reload } = list
  return (
    <Page title="Invoices" actions={<Can need="invoices.create"><Link to="/invoices/new" className="btn btn-primary"><IconPlus width={16} height={16} /> New invoice</Link></Can>}>
      <SearchBox value={q} onChange={setQ} placeholder="Search number or customer" />
      <div className="nav nav-pills flex-nowrap gap-2 mb-3 pb-1 overflow-x-auto" role="tablist" aria-label="Stage filter">
        {filters.map((f) => (
          <button key={f.key} role="tab" aria-selected={filter === f.key} onClick={() => setFilter(f.key)} className={`nav-link rounded-pill text-nowrap small fw-semibold px-3 py-1 ${filter === f.key ? 'active' : 'bg-white border'}`}>
            {f.label}
          </button>
        ))}
      </div>
      <ErrorBanner error={actionError ?? error} />
      {notice && <div role="status" className="mb-3 rounded-3 border-start border-4 border-primary bg-white p-3 small fw-medium text-primary-emphasis shadow-sm">{notice}</div>}
      {loading && !data ? <Spinner /> : data && data.items.length === 0 ? <Empty>No invoices match.</Empty> : (
        <InvoiceTable
          items={data?.items ?? []}
          onChanged={(m) => { setActionError(null); setNotice(m); reload() }}
          onError={(e) => { setNotice(''); setActionError(e) }}
        />
      )}
    <ShowMore list={list} />
    </Page>
  )
}

export function InvoiceTable({ items, onChanged, onError }: { items: Invoice[]; onChanged?: (msg: string) => void; onError?: (e: Error) => void }) {
  return (
    <>
      <ul className="list-unstyled vstack gap-2 mb-0 d-md-none">
        {items.map((inv) => (
          <li key={inv.id}>
            <Link to={`/invoices/${inv.id}`} className="card card-body d-flex flex-row align-items-center gap-3 text-reset text-decoration-none">
              <div className="min-w-0 flex-grow-1">
                <div className="d-flex align-items-center gap-2">
                  <span className="fw-semibold text-primary-emphasis">{inv.number || 'Draft'}</span>
                  <Badge status={inv.stage} />
                </div>
                <div className="text-truncate small text-body-secondary">{inv.billTo.name}</div>
              </div>
              <div className="text-end">
                <div className="fw-semibold" style={tnum}>{formatMoney(inv.totals.total, inv.currency)}</div>
                <div className="fs-8 text-body-secondary">{inv.dueDate ? `Due ${formatDate(inv.dueDate)}` : formatDate(inv.issueDate)}</div>
              </div>
            </Link>
            {onChanged && onError && <div className="mt-1 d-flex justify-content-end"><InvoiceActions inv={inv} onChanged={onChanged} onError={onError} /></div>}
          </li>
        ))}
      </ul>
      <div className="card overflow-hidden d-none d-md-block">
        <div className="table-responsive">
        <table className="table table-hover align-middle mb-0 small">
          <thead>
            <tr>
              <th>Number</th>
              <th>Customer</th>
              <th>Issued</th>
              <th>Due</th>
              <th>Stage</th>
              <th className="text-end">Total</th>
              {onChanged && <th className="text-end">Actions</th>}
            </tr>
          </thead>
          <tbody>
            {items.map((inv) => (
              <tr key={inv.id}>
                <td className="text-nowrap fw-semibold">
                  <Link to={`/invoices/${inv.id}`}>{inv.number || 'Draft'}</Link>
                </td>
                <td>{inv.billTo.name}</td>
                <td className="text-nowrap">{formatDate(inv.issueDate)}</td>
                <td className="text-nowrap">{formatDate(inv.dueDate)}</td>
                <td><Badge status={inv.stage} /></td>
                <td className="text-nowrap text-end fw-semibold" style={tnum}>{formatMoney(inv.totals.total, inv.currency)}</td>
                {onChanged && onError && (
                  <td>
                    <div className="d-flex justify-content-end"><InvoiceActions inv={inv} onChanged={onChanged} onError={onError} /></div>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
        </div>
      </div>
    </>
  )
}
