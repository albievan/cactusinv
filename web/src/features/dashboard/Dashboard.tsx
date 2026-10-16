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
    <Page title="Dashboard" actions={<Can need="invoices.edit"><Link to="/invoices/new" className="btn-primary"><IconPlus width={16} height={16} /> New invoice</Link></Can>}>
      <ErrorBanner error={error} />
      {data && (
        <div className="space-y-5">
          {data.failedEmailCount > 0 && (
            <MaybeLink need="emails.view" to="/emails" className="flex items-center gap-2 rounded-xl border-l-4 border-red-600 bg-white p-3 text-sm font-semibold text-red-800 shadow-sm">
              ⚠ {data.failedEmailCount} e-mail(s) failed to send — review the outbox.
            </MaybeLink>
          )}
          {currencies.length > 1 && (
            <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Currency">
              <span className="text-sm font-semibold text-brand-900">Currency</span>
              {currencies.map((c) => (
                <button key={c} type="button" onClick={() => setPicked(c)} aria-pressed={c === currency} className={`chip min-h-[32px] px-3 ${c === currency ? 'bg-brand-600 text-white' : 'bg-white text-brand-900 ring-1 ring-cream-300'}`}>
                  {c}
                </button>
              ))}
            </div>
          )}
          {currency ? <CurrencyView data={data} currency={currency} /> : <Empty>No invoices yet — create your first one to see figures here.</Empty>}
          <div className="grid gap-5 xl:grid-cols-3">
            <TopDebtors data={data} />
            <Upcoming data={data} />
            <Activity data={data} />
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
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <section className="card sm:col-span-2 xl:col-span-1">
          <h2 className="text-sm font-semibold text-slate-600">Outstanding</h2>
          <p className="mt-1 text-5xl font-bold tracking-tight text-brand-900">
            <span className="mr-1 text-2xl font-semibold text-slate-500">{currency}</span>
            {groupAmount(t?.outstanding ?? none)}
          </p>
          <p className="mt-1 text-sm text-slate-600">{t?.openCount ?? 0} unpaid invoice{t?.openCount === 1 ? '' : 's'}</p>
        </section>
        <section className="card">
          <h2 className="text-sm font-semibold text-slate-600">Overdue</h2>
          <p className="mt-1 text-2xl font-bold text-brand-900">{currency} {groupAmount(t?.overdue ?? none)}</p>
          <p className={`mt-1 flex items-center gap-1 text-sm font-semibold ${t && t.overdueCount > 0 ? 'text-amber-800' : 'text-slate-500'}`}>
            {t && t.overdueCount > 0 && <IconClockAlert width={16} height={16} />}
            {t && t.overdueCount > 0 ? <Link to="/invoices" className="hover:underline">{t.overdueCount} overdue</Link> : 'Nothing overdue'}
          </p>
        </section>
        <section className="card">
          <h2 className="text-sm font-semibold text-slate-600">Invoiced this month</h2>
          <p className="mt-1 text-2xl font-bold text-brand-900">{currency} {groupAmount(invoicedNow?.total ?? none)}</p>
          <p className="mt-1 text-sm text-slate-600">{invoicedNow?.count ?? 0} invoice{invoicedNow?.count === 1 ? '' : 's'}</p>
        </section>
        <section className="card">
          <h2 className="text-sm font-semibold text-slate-600">Paid this month</h2>
          <p className="mt-1 text-2xl font-bold text-brand-900">{currency} {groupAmount(paidNow?.total ?? none)}</p>
          <p className="mt-1 text-sm text-slate-600">{paidNow?.count ?? 0} invoice{paidNow?.count === 1 ? '' : 's'}</p>
        </section>
      </div>
      <section className="card">
        <h2 className="mb-1 text-lg font-semibold">Invoiced and paid, last 12 months</h2>
        <p className="mb-3 text-sm text-slate-500">{currency} · invoiced by issue month, paid by payment month</p>
        <ColumnChart categories={data.months.map(monthLabel)} series={series} unit={currency} decimals={digits} />
      </section>
    </>
  )
}

function TopDebtors({ data }: { data: DashboardData }) {
  const max = Math.max(1, ...data.topDebtors.map((d) => Number(d.outstanding)))
  return (
    <section className="card">
      <h2 className="mb-3 text-lg font-semibold">Who owes the most</h2>
      {data.topDebtors.length === 0 ? <p className="text-sm text-slate-500">Nothing outstanding.</p> : (
        <ul className="space-y-3">
          {data.topDebtors.map((d) => (
            <li key={d.customerId + d.currency}>
              <div className="flex items-baseline justify-between gap-2 text-sm">
                <MaybeLink need="customers.view" to={`/customers/${d.customerId}`} className="truncate font-medium text-brand-800 hover:underline">{d.name}</MaybeLink>
                <span className="shrink-0 font-semibold tabular-nums">{d.currency} {groupAmount(d.outstanding)}</span>
              </div>
              <div className="mt-1 h-2 rounded-full bg-cream-200" aria-hidden="true">
                <div className="h-2 rounded-full bg-brand-500" style={{ width: `${(Number(d.outstanding) / max) * 100}%` }} />
              </div>
              <div className="text-xs text-slate-500">{d.openCount} unpaid</div>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function Upcoming({ data }: { data: DashboardData }) {
  return (
    <section className="card space-y-4">
      <div>
        <h2 className="mb-2 text-lg font-semibold">Next scheduled</h2>
        {data.upcomingRuns.length === 0 ? <p className="text-sm text-slate-500"><MaybeLink need="schedules.edit" to="/schedules/new" className="text-brand-700 underline">Create a schedule</MaybeLink> to raise invoices automatically.</p> : (
          <ul className="space-y-1.5 text-sm">
            {data.upcomingRuns.map((s) => (
              <li key={s.id} className="flex justify-between gap-2">
                <Link to={`/schedules/${s.id}`} className="truncate text-brand-700 hover:underline">{s.name}</Link>
                <span className="shrink-0 text-slate-500">{formatInZone(s.nextRunAt, s.recurrence.timeZone)}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
      <div>
        <h2 className="mb-2 text-lg font-semibold">E-mails queued</h2>
        {data.upcomingEmails.length === 0 ? <p className="text-sm text-slate-500">Nothing waiting to be sent.</p> : (
          <ul className="space-y-1.5 text-sm">
            {data.upcomingEmails.map((m) => (
              <li key={m.id} className="flex justify-between gap-2">
                <span className="truncate">{m.subject}</span>
                <span className="shrink-0 text-slate-500">{formatDateTime(m.nextAttemptAt)}</span>
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
    <section className="card">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-lg font-semibold">Recent activity</h2>
        <MaybeLink need="audit.view" to="/audit" className="text-xs font-semibold text-brand-700 hover:underline">Audit log</MaybeLink>
      </div>
      {data.activity.length === 0 ? <p className="text-sm text-slate-500">No activity yet.</p> : (
        <ol className="space-y-2 text-sm">
          {data.activity.map((e) => (
            <li key={e.seq}>
              <span className="font-semibold text-brand-900">{actionLabel(e.action)}</span>
              <span className="text-slate-600"> · {e.summary || e.entityType}</span>
              <div className="text-xs text-slate-500">{e.actor} · {formatDateTime(e.at)}</div>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}
