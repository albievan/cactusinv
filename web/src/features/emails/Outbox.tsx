import { useState } from 'react'
import { MaybeLink } from '../../components/Access'
import { useCan } from '../../components/auth'
import { api, unwrap } from '../../lib/api/client'
import { formatDateTime } from '../../lib/format'
import { useAsync } from '../../lib/useAsync'
import { Badge, Empty, ErrorBanner, Page, Spinner } from '../../components/ui'

const statuses = ['pending', 'failed', 'sent', 'cancelled'] as const

export function Outbox() {
  const can = useCan()
  const [status, setStatus] = useState<(typeof statuses)[number]>('pending')
  const { data, error, loading, reload } = useAsync(() => unwrap(api.GET('/emails', { params: { query: { status, limit: 100 } } })), [status])
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
      <div className="mb-3 flex gap-2 overflow-x-auto" role="tablist">
        {statuses.map((s) => (
          <button key={s} role="tab" aria-selected={status === s} onClick={() => setStatus(s)} className={`chip min-h-[36px] px-3 capitalize ${status === s ? 'bg-brand-500 text-white' : 'bg-white text-brand-900 ring-1 ring-cream-300 hover:bg-cream-50'}`}>{s}</button>
        ))}
      </div>
      <ErrorBanner error={actionError ?? error} />
      {loading && !data ? <Spinner /> : data?.items.length === 0 ? <Empty>Nothing {status}.</Empty> : (
        <ul className="space-y-2">
          {data?.items.map((m) => (
            <li key={m.id} className="card text-sm">
              <div className="flex items-center justify-between gap-2">
                {m.kind === 'statement' ? (
                  <MaybeLink need="customers.view" to={`/customers/${m.customerId}`} className="truncate font-semibold text-brand-700">{m.subject} <span className="chip ml-1 bg-cream-200 text-brand-800">statement</span></MaybeLink>
                ) : (
                  <MaybeLink need="invoices.view" to={`/invoices/${m.invoiceId}`} className="truncate font-semibold text-brand-700">{m.subject}</MaybeLink>
                )}
                <Badge status={m.status} />
              </div>
              <div className="truncate text-slate-500">To {m.to.join(', ')}{m.cc.length ? ` · CC ${m.cc.join(', ')}` : ''}</div>
              <div className="text-slate-500">{m.status === 'sent' ? `Sent ${formatDateTime(m.sentAt)}` : `Due ${formatDateTime(m.nextAttemptAt)}`}{m.attempts ? ` · ${m.attempts} attempt(s)` : ''}</div>
              {m.lastError && <div className="text-red-600">{m.lastError}</div>}
              <div className="mt-2 flex gap-2">
                {can('emails.cancel') && (m.status === 'pending' || m.status === 'failed') && <button className="btn-secondary" onClick={() => act(m.id, 'cancel')}>Cancel</button>}
                {can('emails.retry') && (m.status === 'failed' || m.status === 'cancelled') && <button className="btn-primary" onClick={() => act(m.id, 'retry')}>Retry now</button>}
              </div>
            </li>
          ))}
        </ul>
      )}
    </Page>
  )
}
