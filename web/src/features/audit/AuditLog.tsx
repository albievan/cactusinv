import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, unwrap, type Schemas } from '../../lib/api/client'
import { formatDateTime } from '../../lib/format'
import { useAsync } from '../../lib/useAsync'
import { Empty, ErrorBanner, Page, Spinner } from '../../components/ui'
import { IconShield } from '../../components/icons'
import { Can } from '../../components/Access'
import { actionLabel } from '../invoices/InvoiceDetail'

const entityTypes = ['', 'invoice', 'customer', 'product', 'schedule', 'bank_account', 'currency', 'settings', 'user']
const pageSize = 100

function entityLink(e: Schemas['AuditEntry']): string | null {
  switch (e.entityType) {
    case 'invoice': return e.action === 'invoice.deleted' ? null : `/invoices/${e.entityId}`
    case 'customer': return `/customers/${e.entityId}`
    case 'product': return `/products/${e.entityId}`
    case 'schedule': return `/schedules/${e.entityId}`
    default: return null
  }
}

export function AuditLog() {
  const [params, setParams] = useSearchParams()
  const [offset, setOffset] = useState(0)
  const filter = {
    entityType: params.get('entityType') ?? '',
    entityId: params.get('entityId') ?? '',
    actor: params.get('actor') ?? '',
    action: params.get('action') ?? '',
  }
  const setFilter = (k: keyof typeof filter, v: string) => {
    const next = new URLSearchParams(params)
    if (v) next.set(k, v)
    else next.delete(k)
    setOffset(0)
    setParams(next, { replace: true })
  }
  const { data, error, loading } = useAsync(
    () => unwrap(api.GET('/audit', { params: { query: { ...Object.fromEntries(Object.entries(filter).filter(([, v]) => v)), limit: pageSize, offset } } })),
    [params.toString(), offset],
  )
  const [verify, setVerify] = useState<Schemas['AuditVerification'] | null>(null)
  const [verifying, setVerifying] = useState(false)
  const [verifyError, setVerifyError] = useState<Error | null>(null)
  const runVerify = async () => {
    setVerifying(true)
    setVerifyError(null)
    try {
      setVerify(await unwrap(api.GET('/audit/verify')))
    } catch (e) {
      setVerifyError(e as Error)
    } finally {
      setVerifying(false)
    }
  }
  return (
    <Page
      title="Audit log"
      actions={
        <Can need="audit.verify">
          <button className="btn btn-outline-primary" onClick={runVerify} disabled={verifying}>
            <IconShield width={16} /> {verifying ? 'Verifying…' : 'Verify integrity'}
          </button>
        </Can>
      }
    >
      <p className="mb-3 small text-body-secondary">
        Every change and every sign-in is recorded here with who did it. Entries can never be edited or deleted — the database refuses it — and each entry is
        chained to the previous one by a SHA-256 hash, so any tampering is detected by <em>Verify integrity</em>.
      </p>
      {verify && (
        <div role="status" className={`mb-3 rounded-3 border-start border-4 bg-white p-3 small fw-semibold shadow-sm ${verify.ok ? 'border-success text-success-emphasis' : 'border-danger text-danger-emphasis'}`}>
          {verify.ok
            ? `Intact: all ${verify.entries} entries verify — nothing has been changed or removed.`
            : `Tampering detected at entry #${verify.brokenAt}: ${verify.reason} (${verify.entries} entries before it verify).`}
        </div>
      )}
      <ErrorBanner error={verifyError ?? error} />
      <div className="row row-cols-1 row-cols-sm-2 row-cols-lg-4 g-2 mb-3">
        <div>
          <select aria-label="Area" className="form-select" value={filter.entityType} onChange={(e) => setFilter('entityType', e.target.value)}>
            {entityTypes.map((t) => <option key={t} value={t}>{t ? t.replace('_', ' ') : 'All areas'}</option>)}
          </select>
        </div>
        <div>
          <input aria-label="Action" className="form-control" placeholder="Action, e.g. invoice.voided" value={filter.action} onChange={(e) => setFilter('action', e.target.value)} />
        </div>
        <div>
          <input aria-label="User" className="form-control" placeholder="User" value={filter.actor} onChange={(e) => setFilter('actor', e.target.value)} />
        </div>
        <div>
          <input aria-label="Record id" className="form-control" placeholder="Record id" value={filter.entityId} onChange={(e) => setFilter('entityId', e.target.value)} />
        </div>
      </div>
      {loading && !data ? <Spinner /> : data && data.items.length === 0 ? <Empty>No entries match.</Empty> : (
        <div className="card table-responsive">
          <table className="table table-hover align-top mb-0 small" style={{ minWidth: 820 }}>
            <thead>
              <tr>
                <th>#</th>
                <th>When</th>
                <th>User</th>
                <th>Action</th>
                <th>Record</th>
                <th>Details</th>
              </tr>
            </thead>
            <tbody>
              {data?.items.map((e) => {
                const link = entityLink(e)
                return (
                  <tr key={e.seq}>
                    <td className="text-body-secondary" style={{ fontVariantNumeric: 'tabular-nums' }} title={`hash ${e.hash}`}>{e.seq}</td>
                    <td className="text-nowrap">{formatDateTime(e.at)}</td>
                    <td className="fw-medium">{e.actor}</td>
                    <td>
                      <span className="fw-semibold text-primary-emphasis">{actionLabel(e.action)}</span>
                      <div className="fs-8 text-body-secondary">{e.action}</div>
                    </td>
                    <td>
                      <span className="fs-8 text-uppercase text-body-secondary">{e.entityType.replace('_', ' ')}</span>
                      <div>{link ? <Link to={link}>open</Link> : <span className="fs-8 text-body-secondary">{e.entityId ? e.entityId.slice(-8) : '—'}</span>}</div>
                    </td>
                    <td className="text-break text-body-secondary">{e.summary}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
      {data && data.total > pageSize && (
        <div className="mt-3 d-flex align-items-center justify-content-between small">
          <span className="text-body-secondary">{offset + 1}–{Math.min(offset + pageSize, data.total)} of {data.total}</span>
          <div className="d-flex gap-2">
            <button className="btn btn-outline-primary" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - pageSize))}>Newer</button>
            <button className="btn btn-outline-primary" disabled={offset + pageSize >= data.total} onClick={() => setOffset(offset + pageSize)}>Older</button>
          </div>
        </div>
      )}
    </Page>
  )
}
