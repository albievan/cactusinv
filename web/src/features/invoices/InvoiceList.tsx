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
    <Page title="Invoices" actions={<Can need="invoices.create"><Link to="/invoices/new" className="btn-primary"><IconPlus width={16} height={16} /> New invoice</Link></Can>}>
      <SearchBox value={q} onChange={setQ} placeholder="Search number or customer" />
      <div className="-mx-4 mb-4 flex gap-2 overflow-x-auto px-4 pb-1 md:mx-0 md:px-0" role="tablist" aria-label="Stage filter">
        {filters.map((f) => (
          <button key={f.key} role="tab" aria-selected={filter === f.key} onClick={() => setFilter(f.key)} className={`chip min-h-[34px] px-3.5 ${filter === f.key ? 'bg-brand-600 text-white' : 'bg-white text-brand-900 ring-1 ring-cream-300'}`}>
            {f.label}
          </button>
        ))}
      </div>
      <ErrorBanner error={actionError ?? error} />
      {notice && <div role="status" className="mb-3 rounded-xl border-l-4 border-brand-500 bg-white p-3 text-sm font-medium text-brand-900 shadow-sm">{notice}</div>}
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
            {onChanged && onError && <div className="mt-1 flex justify-end"><InvoiceActions inv={inv} onChanged={onChanged} onError={onError} /></div>}
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
              {onChanged && <th className="px-4 py-3 text-right">Actions</th>}
            </tr>
          </thead>
          <tbody className="divide-y divide-cream-200">
            {items.map((inv) => (
              <tr key={inv.id} className="hover:bg-cream-50">
                <td className="whitespace-nowrap px-4 py-3 font-semibold">
                  <Link to={`/invoices/${inv.id}`} className="text-brand-700 hover:underline">{inv.number || 'Draft'}</Link>
                </td>
                <td className="px-4 py-3">{inv.billTo.name}</td>
                <td className="whitespace-nowrap px-4 py-3">{formatDate(inv.issueDate)}</td>
                <td className="whitespace-nowrap px-4 py-3">{formatDate(inv.dueDate)}</td>
                <td className="px-4 py-3"><Badge status={inv.stage} /></td>
                <td className="whitespace-nowrap px-4 py-3 text-right font-semibold tabular-nums">{formatMoney(inv.totals.total, inv.currency)}</td>
                {onChanged && onError && (
                  <td className="px-4 py-2">
                    <div className="flex justify-end"><InvoiceActions inv={inv} onChanged={onChanged} onError={onError} /></div>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  )
}
