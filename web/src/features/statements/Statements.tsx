import { useMemo, useState } from 'react'
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

const pdfHref = (customerId: string) => `/api/v1/customers/${customerId}/statement/pdf`

const lastSent = (s: Summary) => {
  if (!s.lastEmail) return 'Never'
  if (s.lastEmail.sentAt) return formatDate(s.lastEmail.sentAt)
  return s.lastEmail.status === 'failed' ? 'Failed' : 'Queued'
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
  return (
    <Page
      title="Statements"
      actions={
        <Can need="statements.send">
          <button type="button" className="btn-primary" disabled={picked.size === 0} onClick={() => setConfirm(true)}>
            <IconSend width={16} height={16} /> E-mail {picked.size > 0 ? `${picked.size} statement${picked.size === 1 ? '' : 's'}` : 'statements'}
          </button>
        </Can>
      }
    >
      <ErrorBanner error={error} />
      {msg && <p role="status" className="mb-3 rounded-xl bg-green-50 p-3 text-sm font-semibold text-green-800 ring-1 ring-green-200">{msg}</p>}
      {data && (
        <div className="space-y-4">
          <p className="text-sm text-slate-600">Every customer's unpaid invoices as of {formatDate(data.asOf)}. Pick customers to e-mail their statements, or open one to see it in full.</p>
          {totals.length > 0 && (
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
              {totals.map(([cur, t]) => (
                <section key={cur} className="card">
                  <h2 className="text-sm font-semibold text-slate-600">Owed to you · {cur}</h2>
                  <p className="mt-1 text-2xl font-bold tabular-nums text-brand-900">{cur} {groupAmount(t.owed)}</p>
                  <p className="mt-1 text-sm text-slate-600">
                    {t.customers} customer{t.customers === 1 ? '' : 's'}
                    {t.overdue > 0 && <span className="font-semibold text-amber-800"> · {t.overdue} overdue</span>}
                  </p>
                </section>
              ))}
            </div>
          )}
          <div className="flex flex-col gap-3 md:flex-row md:items-center">
            <div className="flex flex-wrap gap-2" role="group" aria-label="Show">
              {([['owing', 'Owing'], ['overdue', 'Overdue'], ['all', 'All customers']] as [Filter, string][]).map(([k, label]) => (
                <button key={k} type="button" aria-pressed={filter === k} onClick={() => setFilter(k)} className={`chip min-h-[36px] px-4 ${filter === k ? 'bg-brand-600 text-white' : 'bg-white text-brand-900 ring-1 ring-cream-300'}`}>
                  {label}
                </button>
              ))}
            </div>
            <div className="md:ml-auto md:w-80 [&>input]:mb-0"><SearchBox value={q} onChange={setQ} placeholder="Search customers" /></div>
          </div>
          {rows.length === 0 ? (
            <Empty>{filter === 'all' ? 'No customers yet.' : filter === 'overdue' ? 'Nobody is overdue.' : 'Nobody owes you anything right now.'}</Empty>
          ) : (
            <div className="card overflow-x-auto p-0">
              <table className="w-full text-sm">
                <thead className="table-head">
                  <tr>
                    <th className="w-10 py-3 pl-3 pr-1 sm:px-4"><input type="checkbox" className="h-4 w-4" aria-label="Select all shown" checked={allPicked} onChange={toggleAll} /></th>
                    <th className="px-2 py-3 text-left sm:px-4">Customer</th>
                    <th className="px-2 py-3 text-right sm:px-4">Balance</th>
                    <th className="hidden px-4 py-3 text-right lg:table-cell">Current</th>
                    <th className="hidden px-4 py-3 text-right lg:table-cell">1–30</th>
                    <th className="hidden px-4 py-3 text-right lg:table-cell">31–60</th>
                    <th className="hidden px-4 py-3 text-right lg:table-cell">61–90</th>
                    <th className="hidden px-4 py-3 text-right lg:table-cell">90+</th>
                    <th className="hidden px-4 py-3 text-left md:table-cell">Last sent</th>
                    <th className="px-2 py-3 text-right sm:px-4"><span className="sr-only sm:not-sr-only">Actions</span></th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-cream-200">
                  {rows.map((s) => (
                    <tr key={s.customerId} className={picked.has(s.customerId) ? 'bg-brand-50/60' : ''}>
                      <td className="py-3 pl-3 pr-1 sm:px-4"><input type="checkbox" className="h-4 w-4" aria-label={`Select ${s.name}`} checked={picked.has(s.customerId)} onChange={() => toggle(s.customerId)} /></td>
                      <td className="px-2 py-3 sm:px-4">
                        <Link to={`/statements/${s.customerId}`} className="font-semibold text-brand-800 hover:underline">{s.name}</Link>
                        <div className="text-xs text-slate-500">
                          {s.code} · {s.openCount} unpaid
                          {s.overdueCount > 0 && <span className="font-semibold text-amber-800"> · {s.overdueCount} overdue ({s.maxDaysOverdue} days)</span>}
                        </div>
                      </td>
                      <td className="whitespace-nowrap px-2 py-3 text-right font-semibold tabular-nums sm:px-4">{s.currency} {groupAmount(s.total)}</td>
                      {buckets(s.aging).map(([label, v], i) => (
                        <td key={label} className={`hidden whitespace-nowrap px-4 py-3 text-right tabular-nums lg:table-cell ${Number(v) === 0 ? 'text-slate-400' : i >= 2 ? 'font-semibold text-amber-800' : ''}`}>
                          {Number(v) === 0 ? '—' : groupAmount(v)}
                        </td>
                      ))}
                      <td className="hidden whitespace-nowrap px-4 py-3 md:table-cell">
                        <span className={s.lastEmail?.status === 'failed' ? 'font-semibold text-red-700' : 'text-slate-600'}>{lastSent(s)}</span>
                      </td>
                      <td className="py-3 pl-1 pr-3 sm:px-4">
                        <div className="flex justify-end gap-1">
                          <a href={pdfHref(s.customerId)} target="_blank" rel="noreferrer" title="Statement PDF" className="hidden h-8 w-8 sm:inline-flex items-center justify-center rounded-lg text-brand-800 ring-1 ring-cream-300 hover:bg-cream-50">
                            <IconFilePdf width={15} height={15} /><span className="sr-only">Statement PDF for {s.name}</span>
                          </a>
                          <Link to={`/statements/${s.customerId}`} title="Open statement" className="inline-flex h-8 items-center rounded-lg px-2.5 text-xs font-semibold text-brand-800 ring-1 ring-cream-300 hover:bg-cream-50">
                            Open
                          </Link>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
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
      <div className="space-y-4">
        <p className="text-sm text-slate-600">Each customer gets their statement PDF at their invoice e-mail address (and CC list). The PDF is built when the e-mail goes out, so it is current.</p>
        <ul className="max-h-56 divide-y divide-cream-200 overflow-y-auto rounded-xl ring-1 ring-cream-200">
          {chosen.map((s) => (
            <li key={s.customerId} className="flex justify-between gap-3 px-3 py-2 text-sm">
              <span className="truncate">{s.name}<span className="block truncate text-xs text-slate-500">{s.email}</span></span>
              <span className={`shrink-0 tabular-nums ${Number(s.total) === 0 ? 'text-slate-400' : 'font-semibold'}`}>{s.currency} {groupAmount(s.total)}</span>
            </li>
          ))}
        </ul>
        {zero > 0 && (
          <label className="flex items-center gap-3 text-sm">
            <input type="checkbox" className="h-5 w-5" checked={includeZero} onChange={(e) => setIncludeZero(e.target.checked)} />
            Also e-mail the {zero} customer{zero === 1 ? '' : 's'} who owe nothing
          </label>
        )}
        <ErrorBanner error={err} />
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>Cancel</button>
          <button type="button" className="btn-primary" onClick={send} disabled={busy}>
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
          <div className="flex flex-wrap gap-2">
            <a className="btn-secondary" href={pdfHref(customerId)} target="_blank" rel="noreferrer"><IconFilePdf width={16} height={16} /> PDF</a>
            <Can need="statements.send"><button type="button" className="btn-primary" onClick={send} disabled={busy}><IconSend width={16} height={16} /> {busy ? 'Queuing…' : 'E-mail statement'}</button></Can>
          </div>
        )
      }
    >
      <ErrorBanner error={st.error ?? customer.error ?? err} />
      {msg && <p role="status" className="mb-3 rounded-xl bg-green-50 p-3 text-sm font-semibold text-green-800 ring-1 ring-green-200">{msg}</p>}
      {s && c && (
        <div className="space-y-5">
          <section className="card grid gap-4 md:grid-cols-[1fr_auto] md:items-end">
            <div className="text-sm text-slate-600">
              <div className="text-xs font-semibold uppercase tracking-wide text-slate-500">Statement of account</div>
              <div className="mt-1">{c.code} · {c.email}{c.cc.length > 0 && ` · cc ${c.cc.join(', ')}`}</div>
              <div>As of {formatDate(s.asOf)} · {c.paymentTerms.text}</div>
              <MaybeLink need="customers.view" to={`/customers/${customerId}`} className="text-brand-700 underline">Customer details</MaybeLink>
            </div>
            <div className="md:text-right">
              <div className="text-sm font-semibold text-slate-600">Balance due</div>
              <div className="text-4xl font-bold tabular-nums tracking-tight text-brand-900">{s.currency} {groupAmount(s.total)}</div>
            </div>
          </section>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-5">
            {buckets(s.aging).map(([label, v], i) => (
              <div key={label} className="card p-4">
                <div className="text-xs font-semibold uppercase tracking-wide text-slate-500">{label}</div>
                <div className={`mt-1 text-lg font-bold tabular-nums ${Number(v) > 0 && i >= 2 ? 'text-amber-800' : 'text-brand-900'}`}>{groupAmount(v)}</div>
              </div>
            ))}
          </div>
          <section className="card overflow-x-auto p-0">
            <h2 className="px-4 pt-4 text-lg font-semibold">Unpaid invoices</h2>
            {s.lines.length === 0 ? (
              <p className="p-4 text-sm text-slate-500">Nothing owed — every invoice is paid.</p>
            ) : (
              <table className="mt-3 w-full text-sm">
                <thead className="table-head">
                  <tr>
                    <th className="px-4 py-3 text-left">Invoice</th>
                    <th className="px-4 py-3 text-left">Issued</th>
                    <th className="px-4 py-3 text-left">Due</th>
                    <th className="hidden px-4 py-3 text-left sm:table-cell">Status</th>
                    <th className="px-4 py-3 text-right">Amount</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-cream-200">
                  {s.lines.map((l) => (
                    <tr key={l.invoiceId}>
                      <td className="whitespace-nowrap px-4 py-3 font-semibold"><MaybeLink need="invoices.view" to={`/invoices/${l.invoiceId}`} className="text-brand-800 hover:underline">{l.number}</MaybeLink></td>
                      <td className="whitespace-nowrap px-4 py-3">{formatDate(l.issueDate)}</td>
                      <td className="whitespace-nowrap px-4 py-3">
                        {formatDate(l.dueDate)}
                        {l.daysOverdue > 0 && (
                          <span className="ml-2 inline-flex items-center gap-1 text-xs font-semibold text-amber-800"><IconClockAlert width={13} height={13} />{l.daysOverdue} days late</span>
                        )}
                      </td>
                      <td className="hidden px-4 py-3 sm:table-cell"><Badge status={l.status} /></td>
                      <td className="whitespace-nowrap px-4 py-3 text-right font-semibold tabular-nums">{groupAmount(l.amount)}</td>
                    </tr>
                  ))}
                </tbody>
                <tfoot>
                  <tr className="border-t-2 border-cream-300">
                    <td colSpan={3} className="px-4 py-3 font-semibold sm:hidden">Total</td>
                    <td colSpan={4} className="hidden px-4 py-3 font-semibold sm:table-cell">Total</td>
                    <td className="whitespace-nowrap px-4 py-3 text-right font-bold tabular-nums">{s.currency} {groupAmount(s.total)}</td>
                  </tr>
                </tfoot>
              </table>
            )}
          </section>
          <section className="card">
            <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
              <h2 className="text-lg font-semibold">Statements e-mailed</h2>
              <Can need="schedules.create"><Link to={`/schedules/new?customer=${customerId}&kind=statement`} className="btn-secondary"><IconCalendar width={16} height={16} /> Schedule monthly statements</Link></Can>
            </div>
            {history.data && history.data.items.length === 0 ? (
              <p className="text-sm text-slate-500">No statement has been e-mailed to this customer yet.</p>
            ) : (
              <ul className="divide-y divide-cream-200 text-sm">
                {history.data?.items.map((m) => (
                  <li key={m.id} className="flex flex-wrap items-center justify-between gap-2 py-2">
                    <span>
                      <span className="font-medium">{m.to.join(', ')}</span>
                      <span className="block text-xs text-slate-500">Queued {formatDateTime(m.createdAt)}{m.sentAt && ` · sent ${formatDateTime(m.sentAt)}`}</span>
                      {m.lastError && m.status === 'failed' && <span className="block text-xs text-red-700">{m.lastError}</span>}
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
