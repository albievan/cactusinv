import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, unwrap, type Invoice } from '../../lib/api/client'
import { formatDate } from '../../lib/format'
import { formatMoney } from '../../lib/money'
import { useAsync } from '../../lib/useAsync'
import { Badge, Empty, ErrorBanner, Page, SearchBox, Spinner } from '../../components/ui'

const filters = ['all', 'draft', 'issued', 'sent', 'overdue', 'paid', 'void'] as const
type Filter = (typeof filters)[number]

export function InvoiceList() {
  const [filter, setFilter] = useState<Filter>('all')
  const [q, setQ] = useState('')
  const { data, error, loading } = useAsync(
    () =>
      unwrap(
        api.GET('/invoices', {
          params: {
            query: {
              q: q || undefined,
              status: filter === 'all' || filter === 'overdue' ? undefined : filter,
              overdue: filter === 'overdue' ? true : undefined,
              limit: 100,
            },
          },
        }),
      ),
    [filter, q],
  )
  return (
    <Page title="Invoices" actions={<Link to="/invoices/new" className="btn-primary">+ New</Link>}>
      <SearchBox value={q} onChange={setQ} placeholder="Search number or customer" />
      <div className="-mx-4 mb-3 flex gap-2 overflow-x-auto px-4 pb-1" role="tablist" aria-label="Status filter">
        {filters.map((f) => (
          <button key={f} role="tab" aria-selected={filter === f} onClick={() => setFilter(f)} className={`chip min-h-[36px] px-3 capitalize ${filter === f ? 'bg-brand-500 text-white' : 'bg-white ring-1 ring-slate-200 dark:bg-slate-900 dark:ring-slate-700'}`}>
            {f}
          </button>
        ))}
      </div>
      <ErrorBanner error={error} />
      {loading && !data ? <Spinner /> : data && data.items.length === 0 ? <Empty>No invoices yet.</Empty> : <InvoiceCards items={data?.items ?? []} />}
    </Page>
  )
}

export function InvoiceCards({ items }: { items: Invoice[] }) {
  return (
    <>
      <ul className="space-y-2 md:hidden">
        {items.map((inv) => (
          <li key={inv.id}>
            <Link to={`/invoices/${inv.id}`} className="card flex items-center gap-3 active:bg-slate-50">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="font-semibold">{inv.number || 'Draft'}</span>
                  <Badge status={inv.overdue ? 'overdue' : inv.status} />
                </div>
                <div className="truncate text-sm text-slate-500">{inv.billTo.name}</div>
              </div>
              <div className="text-right">
                <div className="font-semibold tabular-nums">{formatMoney(inv.totals.total, inv.currency)}</div>
                <div className="text-xs text-slate-500">{inv.dueDate ? `Due ${formatDate(inv.dueDate)}` : formatDate(inv.issueDate)}</div>
              </div>
            </Link>
          </li>
        ))}
      </ul>
      <table className="card hidden w-full p-0 text-sm md:table">
        <thead className="text-left text-slate-500">
          <tr>
            <th className="p-3">Number</th>
            <th className="p-3">Customer</th>
            <th className="p-3">Issued</th>
            <th className="p-3">Due</th>
            <th className="p-3">Status</th>
            <th className="p-3 text-right">Total</th>
          </tr>
        </thead>
        <tbody>
          {items.map((inv) => (
            <tr key={inv.id} className="border-t border-slate-100 hover:bg-slate-50 dark:border-slate-800 dark:hover:bg-slate-800">
              <td className="p-3 font-medium">
                <Link to={`/invoices/${inv.id}`} className="text-brand-600 hover:underline">
                  {inv.number || 'Draft'}
                </Link>
              </td>
              <td className="p-3">{inv.billTo.name}</td>
              <td className="p-3">{formatDate(inv.issueDate)}</td>
              <td className="p-3">{formatDate(inv.dueDate)}</td>
              <td className="p-3">
                <Badge status={inv.overdue ? 'overdue' : inv.status} />
              </td>
              <td className="p-3 text-right tabular-nums">{formatMoney(inv.totals.total, inv.currency)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  )
}
