import { useMemo, useState } from 'react'
import { Can, MaybeLink } from '../../components/Access'
import { Link } from 'react-router-dom'
import { api, unwrap, type Dashboard as DashboardData } from '../../lib/api/client'
import { formatDateTime, formatInZone } from '../../lib/format'
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
    const set = new Set<string>([...data.totals.map((t) => t.currency), ...data.invoiced.map((m) => m.currency), ...data.paid.map((m) => m.currency)])
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
    </>
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
