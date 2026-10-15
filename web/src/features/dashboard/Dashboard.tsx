import { Link } from 'react-router-dom'
import { api, unwrap } from '../../lib/api/client'
import { formatDateTime } from '../../lib/format'
import { formatMoney } from '../../lib/money'
import { useAsync } from '../../lib/useAsync'
import { Empty, ErrorBanner, Page, Spinner } from '../../components/ui'
import { InvoiceCards } from '../invoices/InvoiceList'

export function Dashboard() {
  const { data, error, loading } = useAsync(() => unwrap(api.GET('/dashboard')), [])
  if (loading && !data) return <Spinner />
  return (
    <Page title="Home" actions={<Link to="/invoices/new" className="btn-primary">+ Invoice</Link>}>
      <ErrorBanner error={error} />
      {data && (
        <div className="space-y-6">
          {data.failedEmailCount > 0 && (
            <Link to="/emails" className="block rounded-xl bg-red-50 p-3 text-sm font-medium text-red-800 ring-1 ring-red-200">
              {data.failedEmailCount} e-mail(s) failed to send — review the outbox.
            </Link>
          )}
          <section>
            <h2 className="mb-2 text-sm font-semibold uppercase text-slate-500">Outstanding</h2>
            {data.totals.length === 0 ? <Empty>Nothing outstanding.</Empty> : (
              <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {data.totals.map((t) => (
                  <Link to="/invoices" key={t.currency} className="card block">
                    <div className="text-sm text-slate-500">{t.currency} · {t.openCount} open</div>
                    <div className="text-2xl font-bold tabular-nums">{formatMoney(t.outstanding, t.currency)}</div>
                    {t.overdueCount > 0 && <div className="mt-1 text-sm font-medium text-amber-700">{t.overdueCount} overdue · {formatMoney(t.overdue, t.currency)}</div>}
                  </Link>
                ))}
              </div>
            )}
          </section>
          <section className="grid gap-4 md:grid-cols-2">
            <div className="card">
              <h2 className="mb-2 font-semibold">Upcoming e-mails</h2>
              {data.upcomingEmails.length === 0 ? <p className="text-sm text-slate-500">None scheduled.</p> : (
                <ul className="space-y-1 text-sm">{data.upcomingEmails.map((m) => <li key={m.id} className="flex justify-between gap-2"><span className="truncate">{m.subject}</span><span className="shrink-0 text-slate-500">{formatDateTime(m.nextAttemptAt)}</span></li>)}</ul>
              )}
            </div>
            <div className="card">
              <h2 className="mb-2 font-semibold">Next scheduled invoices</h2>
              {data.upcomingRuns.length === 0 ? <p className="text-sm text-slate-500"><Link to="/schedules/new" className="text-brand-600 underline">Create a schedule</Link> to invoice automatically.</p> : (
                <ul className="space-y-1 text-sm">{data.upcomingRuns.map((s) => <li key={s.id} className="flex justify-between gap-2"><Link to={`/schedules/${s.id}`} className="truncate text-brand-600">{s.name}</Link><span className="shrink-0 text-slate-500">{formatDateTime(s.nextRunAt)}</span></li>)}</ul>
              )}
            </div>
          </section>
          <section>
            <h2 className="mb-2 text-sm font-semibold uppercase text-slate-500">Recent invoices</h2>
            {data.recentInvoices.length === 0 ? <Empty>No invoices yet.</Empty> : <InvoiceCards items={data.recentInvoices} />}
          </section>
        </div>
      )}
    </Page>
  )
}
