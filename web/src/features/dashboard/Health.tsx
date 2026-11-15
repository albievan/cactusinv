import { useEffect, useRef, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { api, unwrap } from '../../lib/api/client'
import { useAsync } from '../../lib/useAsync'
import { ErrorBanner } from '../../components/ui'
import { formatBytes, formatCount, formatMs, formatUptime, stateBadge } from '../settings/system/systemFormat'

/** How often the health card reads again. */
const REFRESH_MS = 30_000

/**
 * HealthCard is the application's health and performance on the dashboard (M57), for who
 * may see Settings → Database & containers: the checks (database, mail server, outbox,
 * backups, disk, automatic sending) and the figures, read again every 30 seconds.
 */
export function HealthCard() {
  const health = useAsync(() => unwrap(api.GET('/system/health')), [])
  const reload = useRef(health.reload)
  reload.current = health.reload
  useEffect(() => {
    const t = window.setInterval(() => reload.current(), REFRESH_MS)
    return () => window.clearInterval(t)
  }, [])
  const h = health.data
  const overall = stateBadge(h?.state ?? '')
  return (
    <section className="card card-body vstack gap-3" aria-labelledby="health-heading">
      <div className="d-flex flex-wrap align-items-center justify-content-between gap-2">
        <h2 id="health-heading" className="fs-5 fw-semibold mb-0">Application health</h2>
        <div className="d-flex align-items-center gap-2">
          {h && (
            <span className={`badge rounded-pill border d-inline-flex align-items-center gap-1 ${overall.cls}`}>
              <i className={`bi ${overall.icon}`} aria-hidden="true" /> {overall.label}
            </span>
          )}
          <Link to="/settings/database" className="small">Details</Link>
        </div>
      </div>
      <ErrorBanner error={health.error} />
      {h && (
        <>
          <div className="row row-cols-2 row-cols-md-4 g-3">
            <Figure label="Up for" value={formatUptime(h.uptimeSeconds)} note={h.runtime.hostname || '—'} />
            <Figure label="Requests" value={formatCount(h.requests.total)} note={`${formatCount(h.requests.lastMinute)} in the last minute`} />
            <Figure label="Response time" value={formatMs(h.requests.averageMs)} note={`95% within ${formatMs(h.requests.p95Ms)}`} />
            <Figure label="Server errors" value={formatCount(h.requests.serverErrors)} note={`${formatCount(h.requests.clientErrors)} refused requests`} warn={h.requests.serverErrors > 0} />
            <Figure label="Database" value={formatMs(h.dbPingMs)} note={`${h.pool.inUse} of ${h.pool.open} connections busy`} />
            <Figure label="Memory" value={formatBytes(h.runtime.heapBytes)} note={`${formatBytes(h.runtime.sysBytes)} from the system`} />
            <Figure label="Goroutines" value={formatCount(h.runtime.goroutines)} note={`${h.runtime.cpus} CPUs`} />
            <Figure label="Outbox" value={formatCount(h.outboxPending)} note={h.outboxFailed > 0 ? `${h.outboxFailed} failed` : 'waiting to send'} warn={h.outboxFailed > 0} />
          </div>
          <ul className="list-unstyled vstack gap-1 mb-0" aria-label="Health checks">
            {h.checks.map((c) => {
              const b = stateBadge(c.state)
              return (
                <li key={c.name} className="d-flex flex-wrap align-items-start gap-2 border-top pt-2 small">
                  <span className={`badge rounded-pill border d-inline-flex align-items-center gap-1 ${b.cls}`} style={{ minWidth: '6.5rem' }}>
                    <i className={`bi ${b.icon}`} aria-hidden="true" /> {b.label}
                  </span>
                  <span className="fw-semibold">{c.name}</span>
                  <span className="text-body-secondary flex-grow-1" style={{ minWidth: 0 }}>{c.detail}</span>
                </li>
              )
            })}
          </ul>
        </>
      )}
    </section>
  )
}

function Figure({ label, value, note, warn }: { label: string; value: ReactNode; note: ReactNode; warn?: boolean }) {
  return (
    <div>
      <h3 className="small fw-semibold text-body-secondary mb-0">{label}</h3>
      <p className="mt-1 mb-0 fs-5 fw-bold text-primary-emphasis text-nowrap" style={{ fontVariantNumeric: 'tabular-nums' }}>{value}</p>
      <p className={`mt-1 mb-0 small text-truncate ${warn ? 'fw-semibold text-warning-emphasis' : 'text-body-secondary'}`}>{note}</p>
    </div>
  )
}
