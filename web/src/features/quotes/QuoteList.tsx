import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { Can } from '../../components/Access'
import { api, unwrap } from '../../lib/api/client'
import { formatDate } from '../../lib/format'
import { formatMoney } from '../../lib/money'
import { usePaged } from '../../lib/usePaged'
import { Badge, Empty, ErrorBanner, Page, SearchBox, ShowMore, Spinner, quoteStageLabels } from '../../components/ui'
import { IconPersonPlus, IconPlus } from '../../components/icons'

const tnum = { fontVariantNumeric: 'tabular-nums' } as const

const filters = [
  { key: 'all', label: 'All' },
  { key: 'draft', label: 'Draft' },
  { key: 'sent', label: 'Sent' },
  { key: 'accepted', label: 'Accepted' },
  { key: 'converted', label: 'Invoiced' },
  { key: 'declined', label: 'Declined' },
  { key: 'expired', label: 'Expired' },
  // How customers answered through the links in the quote e-mail (M41).
  { key: 'customer-accepted', label: 'Accepted by customer' },
  { key: 'customer-declined', label: 'Declined by customer' },
  { key: 'customer', label: 'All customer answers' },
] as const
type Filter = (typeof filters)[number]['key']

/** The list query for a filter tab. */
function filterQuery(f: Filter) {
  switch (f) {
    case 'all': return {}
    case 'expired': return { expired: true }
    case 'customer': return { answeredBy: 'customer' as const }
    case 'customer-accepted': return { answeredBy: 'customer' as const, status: 'accepted' as const }
    case 'customer-declined': return { answeredBy: 'customer' as const, status: 'declined' as const }
    default: return { status: f }
  }
}

/** ByCustomer marks a quote the customer answered through the e-mail link. */
function ByCustomer({ show }: { show: boolean }) {
  return show ? <span className="fs-8 fw-semibold text-success-emphasis text-nowrap">by customer</span> : null
}

export function QuoteList() {
  const [params, setParams] = useSearchParams()
  const [filter, setFilter] = useState<Filter>('all')
  const q = params.get('q') ?? ''
  const setQ = (v: string) => setParams(v ? { q: v } : {}, { replace: true })
  const list = usePaged(
    (offset, limit) =>
      unwrap(api.GET('/quotes', {
        params: {
          query: {
            q: q || undefined,
            ...filterQuery(filter),
            limit,
            offset,
          },
        },
      })),
    [filter, q],
  )
  const { data, error, loading } = list
  return (
    <Page
      title="Quotes"
      actions={
        <div className="d-flex gap-2">
          <Link to="/quotes/adhoc-customers" className="btn btn-outline-primary" title="Ad-hoc customers"><IconPersonPlus width={16} /> <span className="d-none d-sm-inline">Ad-hoc customers</span></Link>
          <Can need="quotes.create"><Link to="/quotes/new" className="btn btn-primary"><IconPlus width={16} height={16} /> New quote</Link></Can>
        </div>
      }
    >
      <SearchBox value={q} onChange={setQ} placeholder="Search number or customer" />
      <div className="nav nav-pills flex-nowrap gap-2 mb-3 pb-1 overflow-x-auto" role="tablist" aria-label="Stage filter">
        {filters.map((f) => (
          <button key={f.key} role="tab" aria-selected={filter === f.key} onClick={() => setFilter(f.key)} className={`nav-link rounded-pill text-nowrap small fw-semibold px-3 py-1 ${filter === f.key ? 'active' : 'bg-white border'}`}>
            {f.label}
          </button>
        ))}
      </div>
      <ErrorBanner error={error} />
      {loading && !data ? <Spinner /> : data && data.items.length === 0 ? <Empty>No quotes match.</Empty> : (
        <>
          <ul className="list-unstyled vstack gap-2 mb-0 d-md-none">
            {data?.items.map((qt) => (
              <li key={qt.id}>
                <Link to={`/quotes/${qt.id}`} className="card card-body d-flex flex-row align-items-center gap-3 text-reset text-decoration-none">
                  <div className="min-w-0 flex-grow-1">
                    <div className="d-flex align-items-center gap-2">
                      <span className="fw-semibold text-primary-emphasis">{qt.number}</span>
                      <Badge status={qt.stage} label={quoteStageLabels[qt.stage]} />
                      <ByCustomer show={qt.answeredBy === 'customer'} />
                    </div>
                    <div className="text-truncate small text-body-secondary">{qt.billTo.name}{qt.customerId ? '' : ' · ad-hoc'}</div>
                  </div>
                  <div className="text-end">
                    <div className="fw-semibold" style={tnum}>{formatMoney(qt.totals.total, qt.currency)}</div>
                    <div className="fs-8 text-body-secondary">Valid to {formatDate(qt.validUntil)}</div>
                  </div>
                </Link>
              </li>
            ))}
          </ul>
          <div className="card overflow-hidden d-none d-md-block">
            <div className="table-responsive">
              <table className="table table-hover align-middle mb-0 small">
                <thead>
                  <tr>
                    <th>Number</th>
                    <th>For</th>
                    <th>Date</th>
                    <th>Valid until</th>
                    <th>Stage</th>
                    <th className="text-end">Total</th>
                  </tr>
                </thead>
                <tbody>
                  {data?.items.map((qt) => (
                    <tr key={qt.id}>
                      <td className="text-nowrap fw-semibold"><Link to={`/quotes/${qt.id}`}>{qt.number}</Link></td>
                      <td>{qt.billTo.name}{!qt.customerId && <span className="badge rounded-pill ms-2 bg-body-secondary text-primary-emphasis border">ad-hoc</span>}</td>
                      <td className="text-nowrap">{formatDate(qt.issueDate)}</td>
                      <td className="text-nowrap">{formatDate(qt.validUntil)}</td>
                      <td><div className="d-flex align-items-center gap-2"><Badge status={qt.stage} label={quoteStageLabels[qt.stage]} /><ByCustomer show={qt.answeredBy === 'customer'} /></div></td>
                      <td className="text-nowrap text-end fw-semibold" style={tnum}>{formatMoney(qt.totals.total, qt.currency)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}
      <ShowMore list={list} />
    </Page>
  )
}
