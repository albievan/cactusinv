import { useState } from 'react'
import { MaybeLink } from '../../components/Access'
import { useCan } from '../../components/auth'
import { api, unwrap } from '../../lib/api/client'
import { formatDateTime } from '../../lib/format'
import { useAsync } from '../../lib/useAsync'
import { Badge, Empty, ErrorBanner, Page, Spinner } from '../../components/ui'

const statuses = ['pending', 'sending', 'failed', 'sent', 'cancelled'] as const
const PAGE = 100

export function Outbox() {
  const can = useCan()
  const [status, setStatus] = useState<(typeof statuses)[number]>('pending')
  // Older messages page in with "Show more" (the list was capped at 100).
  const [shown, setShown] = useState(PAGE)
  const { data, error, loading, reload } = useAsync(async () => {
    const pages = await Promise.all(
      Array.from({ length: Math.ceil(shown / PAGE) }, (_, i) => unwrap(api.GET('/emails', { params: { query: { status, limit: PAGE, offset: i * PAGE } } }))),
    )
    return { items: pages.flatMap((p) => p.items), total: pages[0]?.total ?? 0 }
  }, [status, shown])
  const [actionError, setActionError] = useState<Error | null>(null)
  const act = async (id: string, what: 'cancel' | 'retry') => {
    setActionError(null)
    try {
      await unwrap(what === 'cancel' ? api.POST('/emails/{id}/cancel', { params: { path: { id } } }) : api.POST('/emails/{id}/retry', { params: { path: { id } } }))
      reload()
    } catch (e) {
      setActionError(e as Error)
    }
  }
  return (
    <Page title="Outbox">
      <div className="nav nav-pills flex-nowrap gap-2 mb-3 overflow-x-auto" role="tablist">
        {statuses.map((s) => (
          <button key={s} role="tab" aria-selected={status === s} onClick={() => { setStatus(s); setShown(PAGE) }} className={`nav-link border text-capitalize text-nowrap ${status === s ? 'active' : 'bg-white'}`}>{s}</button>
        ))}
      </div>
      <ErrorBanner error={actionError ?? error} />
      {loading && !data ? <Spinner /> : data?.items.length === 0 ? <Empty>Nothing {status}.</Empty> : (
        <ul className="list-unstyled vstack gap-2 mb-0">
          {data?.items.map((m) => (
            <li key={m.id} className="card card-body small">
              <div className="d-flex align-items-center justify-content-between gap-2">
                {m.kind === 'statement' ? (
                  <MaybeLink need="customers.view" to={`/customers/${m.customerId}`} className="text-truncate min-w-0 fw-semibold">{m.subject} <span className="badge rounded-pill ms-1 bg-body-secondary text-primary-emphasis border">statement</span></MaybeLink>
                ) : (
                  <MaybeLink need="invoices.view" to={`/invoices/${m.invoiceId}`} className="text-truncate min-w-0 fw-semibold">{m.subject}</MaybeLink>
                )}
                <Badge status={m.status} />
              </div>
              <div className="text-truncate text-body-secondary">To {m.to.join(', ')}{m.cc.length ? ` · CC ${m.cc.join(', ')}` : ''}</div>
              <div className="text-body-secondary">{m.status === 'sent' ? `Sent ${formatDateTime(m.sentAt)}` : `Due ${formatDateTime(m.nextAttemptAt)}`}{m.attempts ? ` · ${m.attempts} attempt(s)` : ''}</div>
              {m.lastError && <div className="text-danger">{m.lastError}</div>}
              <div className="mt-2 d-flex gap-2">
                {can('emails.cancel') && (m.status === 'pending' || m.status === 'failed') && <button className="btn btn-outline-primary" onClick={() => act(m.id, 'cancel')}>Cancel</button>}
                {can('emails.retry') && (m.status === 'failed' || m.status === 'cancelled') && <button className="btn btn-primary" onClick={() => act(m.id, 'retry')}>Retry now</button>}
              </div>
            </li>
          ))}
        </ul>
      )}
      {data && data.items.length < data.total && (
        <button className="btn btn-outline-primary mt-3 w-100" onClick={() => setShown(shown + PAGE)} disabled={loading}>
          Show more ({data.total - data.items.length} older)
        </button>
      )}
    </Page>
  )
}
