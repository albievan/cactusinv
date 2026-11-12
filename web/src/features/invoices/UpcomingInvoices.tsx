import { useNavigate } from 'react-router-dom'
import { useCan } from '../../components/auth'
import { ActionButton } from '../../components/InvoiceActions'
import { IconEdit, IconFilePdf } from '../../components/icons'
import { Badge, Empty, ErrorBanner, Spinner } from '../../components/ui'
import { api, unwrap, type Schemas } from '../../lib/api/client'
import { formatDate } from '../../lib/format'
import { formatMoney } from '../../lib/money'
import { useAsync } from '../../lib/useAsync'

type Upcoming = Schemas['UpcomingInvoice']

const tnum = { fontVariantNumeric: 'tabular-nums' } as const

/** "August 2026": the month an upcoming invoice is for. */
function forMonth(iso: string): string {
  return new Date(iso + 'T00:00:00').toLocaleDateString(undefined, { month: 'long', year: 'numeric' })
}

/** When it is raised: its date, or that it waits for the schedule to be resumed. */
function raised(u: Upcoming): string {
  return u.scheduleStatus === 'paused' ? 'When resumed' : formatDate(u.runAt ?? u.issueDate)
}

/**
 * UpcomingInvoices is the Invoices → Scheduled tab: for every invoice schedule, active or
 * paused, the next invoice it will raise and has not raised yet. Preview opens a sample PDF
 * (watermarked "SAMPLE - DO NOT EMAIL TO CUSTOMER"; nothing is stored or sent).
 */
export function UpcomingInvoices({ q }: { q: string }) {
  const { data, error, loading } = useAsync(() => unwrap(api.GET('/schedules/upcoming')), [])
  const needle = q.trim().toLowerCase()
  const items = (data?.items ?? []).filter((u) => !needle || u.customerName.toLowerCase().includes(needle) || u.scheduleName.toLowerCase().includes(needle))
  if (loading && !data) return <Spinner />
  return (
    <>
      <ErrorBanner error={error} />
      {data && items.length === 0 ? <Empty>No scheduled invoices to raise.</Empty> : (
        <>
          <ul className="list-unstyled vstack gap-2 mb-0 d-md-none">
            {items.map((u) => (
              <li key={u.scheduleId} className="card card-body">
                <div className="d-flex align-items-center gap-3">
                  <div className="min-w-0 flex-grow-1">
                    <div className="d-flex align-items-center gap-2">
                      <span className="fw-semibold text-primary-emphasis text-truncate">{u.scheduleName}</span>
                      <Badge status={u.scheduleStatus} label={u.scheduleStatus === 'paused' ? 'Paused' : 'Scheduled'} />
                    </div>
                    <div className="text-truncate small text-body-secondary">{u.customerName} · {forMonth(u.period)}</div>
                  </div>
                  <div className="text-end">
                    <div className="fw-semibold" style={tnum}>{u.totals ? formatMoney(u.totals.total, u.currency) : '—'}</div>
                    <div className="fs-8 text-body-secondary">{raised(u)}</div>
                  </div>
                </div>
                {u.problem && <div className="small text-danger mt-1">{u.problem}</div>}
                <div className="mt-1 d-flex justify-content-end"><UpcomingActions u={u} /></div>
              </li>
            ))}
          </ul>
          <div className="card overflow-hidden d-none d-md-block">
            <div className="table-responsive">
              <table className="table table-hover align-middle mb-0 small">
                <thead>
                  <tr>
                    <th>Schedule</th>
                    <th>Customer</th>
                    <th>For</th>
                    <th>Raised</th>
                    <th>Due</th>
                    <th>Stage</th>
                    <th className="text-end">Total</th>
                    <th className="text-end">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((u) => (
                    <tr key={u.scheduleId}>
                      <td className="fw-semibold">
                        {u.scheduleName}
                        {u.problem && <div className="fw-normal text-danger">{u.problem}</div>}
                      </td>
                      <td>{u.customerName}</td>
                      <td className="text-nowrap">{forMonth(u.period)}</td>
                      <td className="text-nowrap">{raised(u)}</td>
                      <td className="text-nowrap">{formatDate(u.dueDate)}</td>
                      <td><Badge status={u.scheduleStatus} label={u.scheduleStatus === 'paused' ? 'Paused' : 'Scheduled'} /></td>
                      <td className="text-nowrap text-end fw-semibold" style={tnum}>{u.totals ? formatMoney(u.totals.total, u.currency) : '—'}</td>
                      <td><div className="d-flex justify-content-end"><UpcomingActions u={u} /></div></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}
    </>
  )
}

/** UpcomingActions: preview the sample PDF, or open the schedule (its lines and notes). */
function UpcomingActions({ u }: { u: Upcoming }) {
  const can = useCan()
  const nav = useNavigate()
  return (
    <div className="d-flex align-items-center gap-1 flex-nowrap" role="group" aria-label={`Actions for ${u.scheduleName}`}>
      <ActionButton showLabel={false} icon={IconFilePdf} label="Preview sample invoice" href={`/api/v1/schedules/${u.scheduleId}/sample-pdf`} disabled={!!u.problem} />
      {can('schedules.edit') && <ActionButton showLabel={false} icon={IconEdit} label="Edit schedule" onClick={() => nav(`/schedules/${u.scheduleId}/edit`)} />}
    </div>
  )
}
