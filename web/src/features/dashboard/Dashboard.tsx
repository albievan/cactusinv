import { useMemo, useState, type ReactNode } from 'react'
import { Can, MaybeLink } from '../../components/Access'
import { Link } from 'react-router-dom'
import { api, unwrap, type Dashboard as DashboardData } from '../../lib/api/client'
import { formatDate, formatDateTime, formatInZone } from '../../lib/format'
import { groupAmount, zero } from '../../lib/money'
import { useCurrencies } from '../../lib/lookups'
import { useAsync } from '../../lib/useAsync'
import { ColumnChart } from '../../components/charts/ColumnChart'
import { Empty, ErrorBanner, Page, Spinner } from '../../components/ui'
import { IconClockAlert, IconPlus } from '../../components/icons'
import { actionLabel } from '../invoices/InvoiceDetail'

// Categorical slots validated with the dataviz validator (light surface): blue, orange.
const INVOICED = '#2a78d6'
const PAID = '#eb6834'

const monthLabel = (m: string) => new Date(`${m}-01T00:00:00`).toLocaleDateString(undefined, { month: 'short' })

export function Dashboard() {
  const { data, error, loading } = useAsync(() => unwrap(api.GET('/dashboard')), [])
  const currencies = useMemo(() => {
    if (!data) return []
    const set = new Set<string>([
      ...data.totals.map((t) => t.currency), ...data.invoiced.map((m) => m.currency), ...data.paid.map((m) => m.currency),
      ...data.quoteStats.map((q) => q.currency),
    ])
    return [...set].sort()
  }, [data])
  const [picked, setPicked] = useState<string>('')
  const currency = picked || currencies[0] || ''

  if (loading && !data) return <Spinner />
  return (
    <Page title="Dashboard" actions={<Can need="invoices.create"><Link to="/invoices/new" className="btn btn-primary"><IconPlus width={16} /> New invoice</Link></Can>}>
      <ErrorBanner error={error} />
      {data && (
        <div className="vstack gap-4">
          {data.failedEmailCount > 0 && (
            <MaybeLink need="emails.view" to="/emails" className="d-flex align-items-center gap-2 rounded-3 border-start border-4 border-danger bg-white p-3 small fw-semibold text-danger-emphasis shadow-sm">
              <i className="bi bi-exclamation-triangle-fill" aria-hidden="true" /> {data.failedEmailCount} e-mail(s) failed to send — review the outbox.
            </MaybeLink>
          )}
          {currencies.length > 1 && (
            <div className="d-flex flex-wrap align-items-center gap-2" role="group" aria-label="Currency">
              <span className="small fw-semibold text-primary-emphasis">Currency</span>
              {currencies.map((c) => (
                <button key={c} type="button" onClick={() => setPicked(c)} aria-pressed={c === currency} className={`btn btn-sm rounded-pill px-3 ${c === currency ? 'btn-primary' : 'btn-outline-primary'}`}>
                  {c}
                </button>
              ))}
            </div>
          )}
          {currency ? <CurrencyView data={data} currency={currency} /> : <Empty>No invoices yet — create your first one to see figures here.</Empty>}
          <ComingUp data={data} />
          <div className="row row-cols-1 row-cols-xl-3 g-4">
            <div><TopDebtors data={data} /></div>
            <div><Upcoming data={data} /></div>
            <div><Activity data={data} /></div>
          </div>
        </div>
      )}
    </Page>
  )
}

function CurrencyView({ data, currency }: { data: DashboardData; currency: string }) {
  const digits = useCurrencies().minor(currency)
  const none = zero(digits)
  const t = data.totals.find((x) => x.currency === currency)
  const thisMonth = data.months[data.months.length - 1] ?? ''
  const pick = (list: DashboardData['invoiced'], m: string) => list.find((x) => x.currency === currency && x.month === m)
  const invoicedNow = pick(data.invoiced, thisMonth)
  const paidNow = pick(data.paid, thisMonth)
  const series = [
    { key: 'invoiced', label: 'Invoiced', color: INVOICED, values: data.months.map((m) => Number(pick(data.invoiced, m)?.total ?? 0)) },
    { key: 'paid', label: 'Paid', color: PAID, values: data.months.map((m) => Number(pick(data.paid, m)?.total ?? 0)) },
  ]
  return (
    <>
      <div className="row row-cols-1 row-cols-sm-2 row-cols-xl-4 g-3">
        <div className="col-sm-12">
          <section className="card card-body h-100">
            <h2 className="small fw-semibold text-body-secondary mb-0">Outstanding</h2>
            <p className="mt-1 mb-0 fw-bold text-primary-emphasis lh-1 text-nowrap" style={{ fontSize: 'clamp(1.75rem, 2.4vw, 3rem)' }}>
              <span className="me-1 fs-4 fw-semibold text-body-secondary">{currency}</span>
              {groupAmount(t?.outstanding ?? none)}
            </p>
            <p className="mt-1 mb-0 small text-body-secondary">{t?.openCount ?? 0} unpaid invoice{t?.openCount === 1 ? '' : 's'}</p>
          </section>
        </div>
        <div>
          <section className="card card-body h-100">
            <h2 className="small fw-semibold text-body-secondary mb-0">Overdue</h2>
            <p className="mt-1 mb-0 fs-4 fw-bold text-primary-emphasis">{currency} {groupAmount(t?.overdue ?? none)}</p>
            <p className={`mt-1 mb-0 d-flex align-items-center gap-1 small fw-semibold ${t && t.overdueCount > 0 ? 'text-warning-emphasis' : 'text-body-secondary'}`}>
              {t && t.overdueCount > 0 && <IconClockAlert width={16} />}
              {t && t.overdueCount > 0 ? <Link to="/invoices" className="text-reset">{t.overdueCount} overdue</Link> : 'Nothing overdue'}
            </p>
          </section>
        </div>
        <div>
          <section className="card card-body h-100">
            <h2 className="small fw-semibold text-body-secondary mb-0">Invoiced this month</h2>
            <p className="mt-1 mb-0 fs-4 fw-bold text-primary-emphasis">{currency} {groupAmount(invoicedNow?.total ?? none)}</p>
            <p className="mt-1 mb-0 small text-body-secondary">{invoicedNow?.count ?? 0} invoice{invoicedNow?.count === 1 ? '' : 's'}</p>
          </section>
        </div>
        <div>
          <section className="card card-body h-100">
            <h2 className="small fw-semibold text-body-secondary mb-0">Paid this month</h2>
            <p className="mt-1 mb-0 fs-4 fw-bold text-primary-emphasis">{currency} {groupAmount(paidNow?.total ?? none)}</p>
            <p className="mt-1 mb-0 small text-body-secondary">{paidNow?.count ?? 0} invoice{paidNow?.count === 1 ? '' : 's'}</p>
          </section>
        </div>
      </div>
      <section className="card card-body">
        <h2 className="fs-5 fw-semibold mb-1">Invoiced and paid, last 12 months</h2>
        <p className="mb-3 small text-body-secondary">{currency} · invoiced by issue month, paid by payment month</p>
        <ColumnChart categories={data.months.map(monthLabel)} series={series} unit={currency} decimals={digits} />
      </section>
      <Can need="quotes.view"><QuoteFigures data={data} currency={currency} /></Can>
    </>
  )
}

/** Stat is one figure in a card: a label, the value and a line under it. */
function Stat({ label, value, note, warn }: { label: string; value: ReactNode; note: ReactNode; warn?: boolean }) {
  return (
    <div>
      <h3 className="small fw-semibold text-body-secondary mb-0">{label}</h3>
      <p className="mt-1 mb-0 fs-5 fw-bold text-primary-emphasis text-nowrap" style={{ fontVariantNumeric: 'tabular-nums' }}>{value}</p>
      <p className={`mt-1 mb-0 small ${warn ? 'fw-semibold text-warning-emphasis' : 'text-body-secondary'}`}>{note}</p>
    </div>
  )
}

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`

/** QuoteFigures is the quotes in the selected currency: open, accepted, won and expiring (M39). */
function QuoteFigures({ data, currency }: { data: DashboardData; currency: string }) {
  const q = data.quoteStats.find((x) => x.currency === currency)
  const rate = q && q.decided > 0 ? Math.round((q.won / q.decided) * 100) : null
  return (
    <section className="card card-body" aria-labelledby="quote-figures">
      <div className="mb-3 d-flex align-items-center justify-content-between gap-2">
        <h2 id="quote-figures" className="fs-5 fw-semibold mb-0">Quotes</h2>
        <Link to="/quotes" className="fs-8 fw-semibold">All quotes</Link>
      </div>
      {!q ? <p className="small text-body-secondary mb-0">No quotes in {currency} yet.</p> : (
        <>
          <div className="row row-cols-2 row-cols-lg-4 g-3">
            <Stat label="Open" value={`${currency} ${groupAmount(q.openValue)}`} note={`${plural(q.open, 'quote')} awaiting an answer`} />
            <Stat label="Accepted, to invoice" value={`${currency} ${groupAmount(q.acceptedValue)}`} note={plural(q.accepted, 'quote')} />
            <div>
              <Stat label="Win rate" value={rate === null ? '—' : `${rate}%`} note={q.decided > 0 ? `${q.won} won of ${q.decided} answered` : 'No answers yet'} />
              {rate !== null && (
                <div className="progress mt-2" style={{ height: 6 }} role="progressbar" aria-label="Win rate" aria-valuenow={rate} aria-valuemin={0} aria-valuemax={100}>
                  <div className="progress-bar" style={{ width: `${rate}%` }} />
                </div>
              )}
            </div>
            <Stat
              label={`Expiring in ${data.soonDays} days`}
              value={q.expiringSoon}
              warn={q.expired > 0}
              note={q.expired > 0 ? `${plural(q.expired, 'quote')} already expired` : 'None expired'}
            />
          </div>
          <p className="mt-3 mb-0 pt-3 border-top small text-body-secondary">
            This month: {plural(q.issuedThisMonth, 'quote')} worth {currency} {groupAmount(q.thisMonthValue)} · {q.converted} turned into invoices · {q.declined} declined
          </p>
        </>
      )}
    </section>
  )
}

/** ListCard is a titled list with a link to the full page and a line for when it is empty. */
function ListCard({ title, link, linkLabel, empty, children }: { title: string; link: string; linkLabel: string; empty: ReactNode; children: ReactNode[] }) {
  return (
    <section className="card card-body h-100">
      <div className="mb-3 d-flex align-items-center justify-content-between gap-2">
        <h2 className="fs-5 fw-semibold mb-0">{title}</h2>
        <Link to={link} className="fs-8 fw-semibold text-nowrap">{linkLabel}</Link>
      </div>
      {children.length === 0 ? <p className="small text-body-secondary mb-0">{empty}</p> : <ul className="list-unstyled vstack gap-3 mb-0">{children}</ul>}
    </section>
  )
}

/** Row is one entry of a ListCard: a linked title and amount, and a line of detail. */
function Row({ to, title, amount, detail, warn }: { to: string; title: string; amount: string; detail: ReactNode; warn?: boolean }) {
  return (
    <li>
      <div className="d-flex align-items-baseline justify-content-between gap-2 small">
        <Link to={to} className="text-truncate min-w-0 fw-medium text-primary-emphasis">{title}</Link>
        <span className="flex-shrink-0 fw-semibold" style={{ fontVariantNumeric: 'tabular-nums' }}>{amount}</span>
      </div>
      <div className={`fs-8 ${warn ? 'fw-semibold text-warning-emphasis' : 'text-body-secondary'}`}>{detail}</div>
    </li>
  )
}

const daysUntil = (iso: string, today: string) => Math.round((Date.parse(`${iso}T00:00:00Z`) - Date.parse(`${today}T00:00:00Z`)) / 86_400_000)
const inDays = (n: number) => (n <= 0 ? 'today' : n === 1 ? 'tomorrow' : `in ${n} days`)

/** ComingUp lists, across currencies, the next scheduled invoices, invoices falling due and quotes running out (M39). */
function ComingUp({ data }: { data: DashboardData }) {
  return (
    <div className="row row-cols-1 row-cols-lg-2 row-cols-xxl-3 g-4">
      <Can need="schedules.view">
        <div>
          <ListCard title="Upcoming invoices" link="/invoices" linkLabel="Invoices → Scheduled" empty="No invoices scheduled.">
            {data.upcomingInvoices.map((u) => (
              <Row
                key={u.scheduleId}
                to={`/schedules/${u.scheduleId}`}
                title={u.customerName}
                amount={u.totals ? `${u.currency} ${groupAmount(u.totals.total)}` : u.currency}
                warn={!!u.problem || u.scheduleStatus === 'paused'}
                detail={u.problem ? `Cannot be raised: ${u.problem}` : `${u.scheduleName} · ${u.runAt ? formatDateTime(u.runAt) : 'paused'}${u.autoSend ? ' · e-mailed' : ''}`}
              />
            ))}
          </ListCard>
        </div>
      </Can>
      <Can need="invoices.view">
        <div>
          <ListCard title={`Due in the next ${data.soonDays} days`} link="/invoices" linkLabel="All invoices" empty="Nothing falls due this week.">
            {data.dueSoon.map((inv) => {
              const n = inv.dueDate ? daysUntil(inv.dueDate, data.today) : 0
              return (
                <Row key={inv.id} to={`/invoices/${inv.id}`} title={`${inv.number ?? ''} · ${inv.billTo.name}`} amount={`${inv.currency} ${groupAmount(inv.totals.total)}`}
                  warn={n <= 1} detail={`Due ${inDays(n)} (${formatDate(inv.dueDate)})`} />
              )
            })}
          </ListCard>
        </div>
      </Can>
      <Can need="quotes.view">
        <div>
          <ListCard title="Quotes expiring soon" link="/quotes" linkLabel="All quotes" empty={`No open quote runs out in the next ${data.soonDays} days.`}>
            {data.expiringQuotes.map((q) => {
              const n = daysUntil(q.validUntil, data.today)
              return (
                <Row key={q.id} to={`/quotes/${q.id}`} title={`${q.number} · ${q.billTo.name}`} amount={`${q.currency} ${groupAmount(q.totals.total)}`}
                  warn={n <= 1} detail={`${q.stage === 'sent' ? 'Sent' : 'Draft'} · valid until ${formatDate(q.validUntil)} (${n <= 0 ? 'last day' : inDays(n)})`} />
              )
            })}
          </ListCard>
        </div>
      </Can>
    </div>
  )
}

function TopDebtors({ data }: { data: DashboardData }) {
  // Bars compare debtors within a currency only: a large JPY amount must not flatten USD.
  const max = (cur: string) => Math.max(1, ...data.topDebtors.filter((d) => d.currency === cur).map((d) => Number(d.outstanding)))
  return (
    <section className="card card-body h-100">
      <h2 className="fs-5 fw-semibold mb-3">Who owes the most</h2>
      {data.topDebtors.length === 0 ? <p className="small text-body-secondary mb-0">Nothing outstanding.</p> : (
        <ul className="list-unstyled vstack gap-3 mb-0">
          {data.topDebtors.map((d) => (
            <li key={d.customerId + d.currency}>
              <div className="d-flex align-items-baseline justify-content-between gap-2 small">
                <MaybeLink need="customers.view" to={`/customers/${d.customerId}`} className="text-truncate min-w-0 fw-medium text-primary-emphasis">{d.name}</MaybeLink>
                <span className="flex-shrink-0 fw-semibold" style={{ fontVariantNumeric: 'tabular-nums' }}>{d.currency} {groupAmount(d.outstanding)}</span>
              </div>
              <div className="progress mt-1" style={{ height: 8 }} aria-hidden="true">
                <div className="progress-bar" style={{ width: `${(Number(d.outstanding) / max(d.currency)) * 100}%` }} />
              </div>
              <div className="fs-8 text-body-secondary">{d.openCount} unpaid</div>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function Upcoming({ data }: { data: DashboardData }) {
  return (
    <section className="card card-body h-100 vstack gap-3">
      <div>
        <h2 className="fs-5 fw-semibold mb-2">Next scheduled</h2>
        {data.upcomingRuns.length === 0 ? <p className="small text-body-secondary mb-0"><MaybeLink need="schedules.create" to="/schedules/new" className="text-decoration-underline">Create a schedule</MaybeLink> to raise invoices automatically.</p> : (
          <ul className="list-unstyled vstack gap-1 small mb-0">
            {data.upcomingRuns.map((s) => (
              <li key={s.id} className="d-flex justify-content-between gap-2">
                <Link to={`/schedules/${s.id}`} className="text-truncate min-w-0">{s.name}</Link>
                <span className="flex-shrink-0 text-body-secondary">{formatInZone(s.nextRunAt, s.recurrence.timeZone)}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
      <div>
        <h2 className="fs-5 fw-semibold mb-2">E-mails queued</h2>
        {data.upcomingEmails.length === 0 ? <p className="small text-body-secondary mb-0">Nothing waiting to be sent.</p> : (
          <ul className="list-unstyled vstack gap-1 small mb-0">
            {data.upcomingEmails.map((m) => (
              <li key={m.id} className="d-flex justify-content-between gap-2">
                <span className="text-truncate min-w-0">{m.subject}</span>
                <span className="flex-shrink-0 text-body-secondary">{formatDateTime(m.nextAttemptAt)}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  )
}

function Activity({ data }: { data: DashboardData }) {
  return (
    <section className="card card-body h-100">
      <div className="mb-3 d-flex align-items-center justify-content-between">
        <h2 className="fs-5 fw-semibold mb-0">Recent activity</h2>
        <MaybeLink need="audit.view" to="/audit" className="fs-8 fw-semibold">Audit log</MaybeLink>
      </div>
      {data.activity.length === 0 ? <p className="small text-body-secondary mb-0">No activity yet.</p> : (
        <ol className="list-unstyled vstack gap-2 small mb-0">
          {data.activity.map((e) => (
            <li key={e.seq}>
              <span className="fw-semibold text-primary-emphasis">{actionLabel(e.action)}</span>
              <span className="text-body-secondary"> · {e.summary || e.entityType}</span>
              <div className="fs-8 text-body-secondary">{e.actor} · {formatDateTime(e.at)}</div>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}
