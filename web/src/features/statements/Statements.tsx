import { useId, useMemo, useState } from 'react'
import { Can, MaybeLink } from '../../components/Access'
import { Link, useParams } from 'react-router-dom'
import { api, unwrap, type Schemas } from '../../lib/api/client'
import { formatDate, formatDateTime } from '../../lib/format'
import { groupAmount, sumAmounts, toMinor } from '../../lib/money'
import { useCurrencies } from '../../lib/lookups'
import { useAsync } from '../../lib/useAsync'
import { Badge, Empty, ErrorBanner, Page, SearchBox, Sheet, Spinner } from '../../components/ui'
import { IconCalendar, IconClockAlert, IconFilePdf, IconSend } from '../../components/icons'
import { uuid } from '../../lib/uuid'

type Summary = Schemas['StatementSummary']
type Aging = Schemas['Aging']

const buckets = (a: Aging): [string, string][] => [
  ['Current', a.current],
  ['1–30 days', a.days1to30],
  ['31–60 days', a.days31to60],
  ['61–90 days', a.days61to90],
  ['90+ days', a.over90],
]

const tabular = { fontVariantNumeric: 'tabular-nums' } as const

const pdfHref = (customerId: string) => `/api/v1/customers/${customerId}/statement/pdf`

const sendFailed = (s: Summary) => s.lastEmail?.status === 'failed'

const lastSent = (s: Summary) => {
  if (!s.lastEmail) return 'Never'
  if (s.lastEmail.sentAt) return formatDate(s.lastEmail.sentAt)
  return sendFailed(s) ? 'Failed' : 'Queued'
}

type Filter = 'owing' | 'overdue' | 'all'

/** Statements lists every customer's statement of account, with bulk e-mailing. */
export function Statements() {
  const { data, error, loading, reload } = useAsync(() => unwrap(api.GET('/statements')), [])
  const { minor } = useCurrencies()
  const [filter, setFilter] = useState<Filter>('owing')
  const [q, setQ] = useState('')
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [confirm, setConfirm] = useState(false)
  const [msg, setMsg] = useState('')

  const rows = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return (data?.items ?? []).filter((s) => {
      if (filter === 'owing' && toMinor(s.total, 3) === 0n) return false
      if (filter === 'overdue' && s.overdueCount === 0) return false
      return !needle || s.name.toLowerCase().includes(needle) || s.code.toLowerCase().includes(needle)
    })
  }, [data, filter, q])

  const totals = useMemo(() => {
    // Summed exactly in each currency's minor units (never as floats).
    const by = new Map<string, { amounts: string[]; customers: number; overdue: number }>()
    for (const s of data?.items ?? []) {
      if (toMinor(s.total, minor(s.currency)) === 0n) continue
      const t = by.get(s.currency) ?? { amounts: [], customers: 0, overdue: 0 }
      t.amounts.push(s.total)
      t.customers++
      if (s.overdueCount > 0) t.overdue++
      by.set(s.currency, t)
    }
    return [...by.entries()]
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([cur, t]) => [cur, { ...t, owed: sumAmounts(t.amounts, minor(cur)) }] as const)
  }, [data, minor])

  const toggle = (id: string) => {
    const next = new Set(picked)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setPicked(next)
  }
  const allPicked = rows.length > 0 && rows.every((r) => picked.has(r.customerId))
  const toggleAll = () => setPicked(allPicked ? new Set() : new Set(rows.map((r) => r.customerId)))

  if (loading && !data) return <Spinner />
  if (loading && !data) return <Spinner />
  return (
    <Page
      title="Statements"
      actions={
        <Can need="statements.send">
          <button type="button" className="btn btn-primary" disabled={picked.size === 0} onClick={() => setConfirm(true)}>
            <IconSend width={16} height={16} /> E-mail {picked.size > 0 ? `${picked.size} statement${picked.size === 1 ? '' : 's'}` : 'statements'}
          </button>
        </Can>
      }
    >
      <ErrorBanner error={error} />
      {msg && <p role="status" className="alert alert-success mb-3 small fw-semibold">{msg}</p>}
      {data && (
        <div className="vstack gap-3">
          <p className="small text-body-secondary mb-0">Every customer's unpaid invoices as of {formatDate(data.asOf)}. Pick customers to e-mail their statements, or open one to see it in full.</p>
          {totals.length > 0 && (
            <div className="row row-cols-1 row-cols-sm-2 row-cols-xl-4 g-3">
              {totals.map(([cur, t]) => (
                <div key={cur}>
                  <section className="card card-body h-100">
                    <h2 className="small fw-semibold text-body-secondary mb-0">Owed to you · {cur}</h2>
                    <p className="mt-1 mb-0 fs-4 fw-bold text-primary-emphasis" style={tabular}>{cur} {groupAmount(t.owed)}</p>
                    <p className="mt-1 mb-0 small text-body-secondary">
                      {t.customers} customer{t.customers === 1 ? '' : 's'}
                      {t.overdue > 0 && <span className="fw-semibold text-warning-emphasis"> · {t.overdue} overdue</span>}
                    </p>
                  </section>
                </div>
              ))}
            </div>
          )}
          <div className="d-flex flex-column flex-md-row align-items-md-baseline gap-3">
            <div className="d-flex flex-wrap gap-2" role="group" aria-label="Show">
              {([['owing', 'Owing'], ['overdue', 'Overdue'], ['all', 'All customers']] as [Filter, string][]).map(([k, label]) => (
                <button key={k} type="button" aria-pressed={filter === k} onClick={() => setFilter(k)} className={`btn btn-sm btn-outline-primary rounded-pill px-3${filter === k ? ' active' : ''}`}>
                  {label}
                </button>
              ))}
            </div>
            <div className="ms-md-auto col-md-4"><SearchBox value={q} onChange={setQ} placeholder="Search customers" /></div>
          </div>
          {rows.length === 0 ? (
            <Empty>{filter === 'all' ? 'No customers yet.' : filter === 'overdue' ? 'Nobody is overdue.' : 'Nobody owes you anything right now.'}</Empty>
          ) : (
            <div className="card overflow-hidden">
              <div className="table-responsive">
                <table className="table table-hover align-middle mb-0 small">
                  <thead>
                    <tr>
                      <th style={{ width: '2.5rem' }}><input type="checkbox" className="form-check-input mt-0" aria-label="Select all shown" checked={allPicked} onChange={toggleAll} /></th>
                      <th className="text-start">Customer</th>
                      <th className="text-end">Balance</th>
                      <th className="d-none d-lg-table-cell text-end">Current</th>
                      <th className="d-none d-lg-table-cell text-end">1–30</th>
                      <th className="d-none d-lg-table-cell text-end">31–60</th>
                      <th className="d-none d-lg-table-cell text-end">61–90</th>
                      <th className="d-none d-lg-table-cell text-end">90+</th>
                      <th className="d-none d-md-table-cell text-start">Last sent</th>
                      <th className="text-end"><span className="d-none d-sm-inline">Actions</span><span className="visually-hidden d-sm-none">Actions</span></th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map((s) => (
                      <tr key={s.customerId} className={picked.has(s.customerId) ? 'table-primary' : ''}>
                        <td><input type="checkbox" className="form-check-input mt-0" aria-label={`Select ${s.name}`} checked={picked.has(s.customerId)} onChange={() => toggle(s.customerId)} /></td>
                        <td>
                          <Link to={`/statements/${s.customerId}`} className="fw-semibold text-primary-emphasis">{s.name}</Link>
                          <div className="fs-8 text-body-secondary">
                            {s.code} · {s.openCount} unpaid
                            {s.overdueCount > 0 && <span className="fw-semibold text-warning-emphasis"> · {s.overdueCount} overdue ({s.maxDaysOverdue} days)</span>}
                          </div>
                        </td>
                        <td className="text-nowrap text-end fw-semibold" style={tabular}>{s.currency} {groupAmount(s.total)}</td>
                        {buckets(s.aging).map(([label, v], i) => (
                          <td key={label} className={`d-none d-lg-table-cell text-nowrap text-end ${Number(v) === 0 ? 'text-body-secondary' : i >= 2 ? 'fw-semibold text-warning-emphasis' : ''}`} style={tabular}>
                            {Number(v) === 0 ? '—' : groupAmount(v)}
                          </td>
                        ))}
                        <td className="d-none d-md-table-cell text-nowrap">
                          <span className={sendFailed(s) ? 'fw-semibold text-danger' : 'text-body-secondary'}>{lastSent(s)}</span>
                        </td>
                        <td>
                          <div className="d-flex justify-content-end gap-1">
                            <a href={pdfHref(s.customerId)} target="_blank" rel="noreferrer" title="Statement PDF" className="btn btn-sm btn-outline-primary btn-icon d-none d-sm-inline-flex">
                              <IconFilePdf width={15} height={15} /><span className="visually-hidden">Statement PDF for {s.name}</span>
                            </a>
                            <Link to={`/statements/${s.customerId}`} title="Open statement" className="btn btn-sm btn-outline-primary">
                              Open
                            </Link>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
      )}
      <BulkSend
        open={confirm}
        ids={[...picked]}
        items={data?.items ?? []}
        onClose={() => setConfirm(false)}
        onDone={(text) => {
          setConfirm(false)
          setPicked(new Set())
          setMsg(text)
          reload()
        }}
      />
    </Page>
  )
}

function BulkSend({ open, ids, items, onClose, onDone }: { open: boolean; ids: string[]; items: Summary[]; onClose: () => void; onDone: (msg: string) => void }) {
  const [includeZero, setIncludeZero] = useState(false)
  const zeroId = useId()
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<Error | null>(null)
  const chosen = items.filter((s) => ids.includes(s.customerId))
  const zero = chosen.filter((s) => Number(s.total) === 0).length
  const send = async () => {
    setBusy(true)
    setErr(null)
    try {
      const res = await unwrap(api.POST('/statements/send', { body: { customerIds: ids, includeZero } }))
      const name = (id: string) => items.find((s) => s.customerId === id)?.name ?? id
      const skipped = res.skipped.map((s) => `${name(s.customerId)} (${s.reason})`)
      onDone(`${res.queued.length} statement${res.queued.length === 1 ? '' : 's'} queued for e-mail.${skipped.length ? ` Skipped: ${skipped.join(', ')}.` : ''}`)
    } catch (e) {
      setErr(e as Error)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Sheet title="E-mail statements" open={open} onClose={onClose}>
      <div className="vstack gap-3">
        <p className="small text-body-secondary mb-0">Each customer gets their statement PDF at their invoice e-mail address (and CC list). The PDF is built when the e-mail goes out, so it is current.</p>
        <ul className="list-group overflow-y-auto mb-0" style={{ maxHeight: '14rem' }}>
          {chosen.map((s) => (
            <li key={s.customerId} className="list-group-item d-flex justify-content-between gap-3 py-2 small">
              <span className="text-truncate min-w-0">{s.name}<span className="d-block text-truncate fs-8 text-body-secondary">{s.email}</span></span>
              <span className={`flex-shrink-0 ${Number(s.total) === 0 ? 'text-body-secondary' : 'fw-semibold'}`} style={tabular}>{s.currency} {groupAmount(s.total)}</span>
            </li>
          ))}
        </ul>
        {zero > 0 && (
          <div className="form-check small mb-0">
            <input id={zeroId} type="checkbox" className="form-check-input" checked={includeZero} onChange={(e) => setIncludeZero(e.target.checked)} />
            <label htmlFor={zeroId} className="form-check-label">
              Also e-mail the {zero} customer{zero === 1 ? '' : 's'} who owe nothing
            </label>
          </div>
        )}
        <ErrorBanner error={err} />
        <div className="d-flex justify-content-end gap-2">
          <button type="button" className="btn btn-outline-primary" onClick={onClose}>Cancel</button>
          <button type="button" className="btn btn-primary" onClick={send} disabled={busy}>
            <IconSend width={16} height={16} /> {busy ? 'Queuing…' : `E-mail ${ids.length}`}
          </button>
        </div>
      </div>
    </Sheet>
  )
}

/** StatementView is one customer's statement in full: aging, unpaid invoices, history. */
export function StatementView() {
  const { customerId = '' } = useParams()
  const customer = useAsync(() => unwrap(api.GET('/customers/{id}', { params: { path: { id: customerId } } })), [customerId])
  const st = useAsync(() => unwrap(api.GET('/customers/{id}/statement', { params: { path: { id: customerId } } })), [customerId])
  const history = useAsync(() => unwrap(api.GET('/emails', { params: { query: { customerId, limit: 20 } } })), [customerId])
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)

  const send = async () => {
    setBusy(true)
    setErr(null)
    setMsg('')
    try {
      await unwrap(api.POST('/customers/{id}/statement/send', { params: { path: { id: customerId }, header: { 'Idempotency-Key': uuid() } }, body: {} }))
      setMsg('Statement queued for e-mail.')
      history.reload()
    } catch (e) {
      setErr(e as Error)
    } finally {
      setBusy(false)
    }
  }

  if ((st.loading && !st.data) || (customer.loading && !customer.data)) return <Spinner />
  const c = customer.data
  const s = st.data
  return (
    <Page
      title={c ? c.name : 'Statement'}
      back="/statements"
      actions={
        s && (
          <div className="d-flex flex-wrap gap-2">
            <a className="btn btn-outline-primary" href={pdfHref(customerId)} target="_blank" rel="noreferrer"><IconFilePdf width={16} height={16} /> PDF</a>
            <Can need="statements.send"><button type="button" className="btn btn-primary" onClick={send} disabled={busy}><IconSend width={16} height={16} /> {busy ? 'Queuing…' : 'E-mail statement'}</button></Can>
          </div>
        )
      }
    >
      <ErrorBanner error={st.error ?? customer.error ?? err} />
      {msg && <p role="status" className="alert alert-success mb-3 small fw-semibold">{msg}</p>}
      {s && c && (
        <div className="vstack gap-4">
          <section className="card card-body flex-md-row justify-content-md-between align-items-md-end gap-3">
            <div className="small text-body-secondary">
              <div className="fs-8 fw-semibold text-uppercase">Statement of account</div>
              <div className="mt-1">{c.code} · {c.email}{c.cc.length > 0 && ` · cc ${c.cc.join(', ')}`}</div>
              <div>As of {formatDate(s.asOf)} · {c.paymentTerms.text}</div>
              <MaybeLink need="customers.view" to={`/customers/${customerId}`} className="text-decoration-underline">Customer details</MaybeLink>
            </div>
            <div className="text-md-end">
              <div className="small fw-semibold text-body-secondary">Balance due</div>
              <div className="fs-2 fw-bold text-primary-emphasis" style={tabular}>{s.currency} {groupAmount(s.total)}</div>
            </div>
          </section>
          <div className="row row-cols-2 row-cols-sm-5 g-3">
            {buckets(s.aging).map(([label, v], i) => (
              <div key={label}>
                <div className="card card-body h-100">
                  <div className="fs-8 fw-semibold text-uppercase text-body-secondary">{label}</div>
                  <div className={`mt-1 fs-5 fw-bold ${Number(v) > 0 && i >= 2 ? 'text-warning-emphasis' : 'text-primary-emphasis'}`} style={tabular}>{groupAmount(v)}</div>
                </div>
              </div>
            ))}
          </div>
          <section className="card overflow-hidden">
            <h2 className="h5 px-3 pt-3">Unpaid invoices</h2>
            {s.lines.length === 0 ? (
              <p className="p-3 pt-0 mb-0 small text-body-secondary">Nothing owed — every invoice is paid.</p>
            ) : (
              <div className="table-responsive">
                <table className="table align-middle mb-0 small">
                  <thead>
                    <tr>
                      <th className="text-start">Invoice</th>
                      <th className="text-start">Issued</th>
                      <th className="text-start">Due</th>
                      <th className="d-none d-sm-table-cell text-start">Status</th>
                      <th className="text-end">Amount</th>
                    </tr>
                  </thead>
                  <tbody>
                    {s.lines.map((l) => (
                      <tr key={l.invoiceId}>
                        <td className="text-nowrap fw-semibold"><MaybeLink need="invoices.view" to={`/invoices/${l.invoiceId}`} className="text-primary-emphasis">{l.number}</MaybeLink></td>
                        <td className="text-nowrap">{formatDate(l.issueDate)}</td>
                        <td className="text-nowrap">
                          {formatDate(l.dueDate)}
                          {l.daysOverdue > 0 && (
                            <span className="ms-2 d-inline-flex align-items-center gap-1 fs-8 fw-semibold text-warning-emphasis"><IconClockAlert width={13} height={13} />{l.daysOverdue} days late</span>
                          )}
                        </td>
                        <td className="d-none d-sm-table-cell"><Badge status={l.status} /></td>
                        <td className="text-nowrap text-end fw-semibold" style={tabular}>{groupAmount(l.amount)}</td>
                      </tr>
                    ))}
                  </tbody>
                  <tfoot className="table-group-divider">
                    <tr>
                      <td colSpan={3} className="fw-semibold d-sm-none">Total</td>
                      <td colSpan={4} className="d-none d-sm-table-cell fw-semibold">Total</td>
                      <td className="text-nowrap text-end fw-bold" style={tabular}>{s.currency} {groupAmount(s.total)}</td>
                    </tr>
                  </tfoot>
                </table>
              </div>
            )}
          </section>
          <section className="card card-body">
            <div className="mb-3 d-flex flex-wrap align-items-center justify-content-between gap-2">
              <h2 className="h5 mb-0">Statements e-mailed</h2>
              <Can need="schedules.create"><Link to={`/schedules/new?customer=${customerId}&kind=statement`} className="btn btn-outline-primary"><IconCalendar width={16} height={16} /> Schedule monthly statements</Link></Can>
            </div>
            {history.data && history.data.items.length === 0 ? (
              <p className="small text-body-secondary mb-0">No statement has been e-mailed to this customer yet.</p>
            ) : (
              <ul className="list-group list-group-flush small">
                {history.data?.items.map((m) => (
                  <li key={m.id} className="list-group-item d-flex flex-wrap align-items-center justify-content-between gap-2 px-0 py-2">
                    <span>
                      <span className="fw-medium">{m.to.join(', ')}</span>
                      <span className="d-block fs-8 text-body-secondary">Queued {formatDateTime(m.createdAt)}{m.sentAt && ` · sent ${formatDateTime(m.sentAt)}`}</span>
                      {m.lastError && m.status === 'failed' && <span className="d-block fs-8 text-danger">{m.lastError}</span>}
                    </span>
                    <Badge status={m.status} />
                  </li>
                ))}
              </ul>
            )}
          </section>
        </div>
      )}
    </Page>
  )
}
