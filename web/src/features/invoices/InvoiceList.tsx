import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, unwrap, type Invoice } from '../../lib/api/client'
import { formatDate } from '../../lib/format'
import { formatMoney } from '../../lib/money'
import { useAsync } from '../../lib/useAsync'
import { Badge, Empty, ErrorBanner, Page, SearchBox, Spinner } from '../../components/ui'
import { IconPlus } from '../../components/icons'

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
  const { data, error, loading } = useAsync(
    () =>
      unwrap(
        api.GET('/invoices', {
          params: {
            query: {
              q: q || undefined,
              status: filter === 'all' || filter === 'overdue' ? undefined : filter,
              overdue: filter === 'overdue' ? true : undefined,
              limit: 200,
            },
          },
        }),
      ),
    [filter, q],
  )
  return (
    <Page title="Invoices" actions={<Link to="/invoices/new" className="btn-primary"><IconPlus width={16} height={16} /> New invoice</Link>}>
      <SearchBox value={q} onChange={setQ} placeholder="Search number or customer" />
      <div className="-mx-4 mb-4 flex gap-2 overflow-x-auto px-4 pb-1 md:mx-0 md:px-0" role="tablist" aria-label="Stage filter">
        {filters.map((f) => (
          <button key={f.key} role="tab" aria-selected={filter === f.key} onClick={() => setFilter(f.key)} className={`chip min-h-[34px] px-3.5 ${filter === f.key ? 'bg-brand-600 text-white' : 'bg-white text-brand-900 ring-1 ring-cream-300'}`}>
            {f.label}
          </button>
        ))}
      </div>
      <ErrorBanner error={error} />
      {loading && !data ? <Spinner /> : data && data.items.length === 0 ? <Empty>No invoices match.</Empty> : <InvoiceTable items={data?.items ?? []} />}
    </Page>
  )
}

export function InvoiceTable({ items }: { items: Invoice[] }) {
  return (
    <>
      <ul className="space-y-2 md:hidden">
        {items.map((inv) => (
          <li key={inv.id}>
            <Link to={`/invoices/${inv.id}`} className="card flex items-center gap-3 p-4">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="font-semibold text-brand-900">{inv.number || 'Draft'}</span>
                  <Badge status={inv.stage} />
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
      <div className="card hidden overflow-hidden p-0 md:block">
        <table className="w-full text-sm">
          <thead className="table-head">
            <tr>
              <th className="px-4 py-3">Number</th>
              <th className="px-4 py-3">Customer</th>
              <th className="px-4 py-3">Issued</th>
              <th className="px-4 py-3">Due</th>
              <th className="px-4 py-3">Stage</th>
              <th className="px-4 py-3 text-right">Total</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-cream-200">
            {items.map((inv) => (
              <tr key={inv.id} className="hover:bg-cream-50">
                <td className="px-4 py-3 font-semibold">
                  <Link to={`/invoices/${inv.id}`} className="text-brand-700 hover:underline">{inv.number || 'Draft'}</Link>
                </td>
                <td className="px-4 py-3">{inv.billTo.name}</td>
                <td className="px-4 py-3">{formatDate(inv.issueDate)}</td>
                <td className="px-4 py-3">{formatDate(inv.dueDate)}</td>
                <td className="px-4 py-3"><Badge status={inv.stage} /></td>
                <td className="px-4 py-3 text-right font-semibold tabular-nums">{formatMoney(inv.totals.total, inv.currency)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  )
}
