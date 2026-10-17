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
          <button className="btn-secondary" onClick={runVerify} disabled={verifying}>
            <IconShield width={16} height={16} /> {verifying ? 'Verifying…' : 'Verify integrity'}
          </button>
        </Can>
      }
    >
      <p className="mb-4 text-sm text-slate-600">
        Every change and every sign-in is recorded here with who did it. Entries can never be edited or deleted — the database refuses it — and each entry is
        chained to the previous one by a SHA-256 hash, so any tampering is detected by <em>Verify integrity</em>.
      </p>
      {verify && (
        <div role="status" className={`mb-4 rounded-xl border-l-4 bg-white p-3 text-sm font-semibold shadow-sm ${verify.ok ? 'border-green-600 text-green-800' : 'border-red-600 text-red-800'}`}>
          {verify.ok
            ? `Intact: all ${verify.entries} entries verify — nothing has been changed or removed.`
            : `Tampering detected at entry #${verify.brokenAt}: ${verify.reason} (${verify.entries} entries before it verify).`}
        </div>
      )}
      <ErrorBanner error={verifyError ?? error} />
      <div className="mb-4 grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
        <select aria-label="Area" className="input" value={filter.entityType} onChange={(e) => setFilter('entityType', e.target.value)}>
          {entityTypes.map((t) => <option key={t} value={t}>{t ? t.replace('_', ' ') : 'All areas'}</option>)}
        </select>
        <input aria-label="Action" className="input" placeholder="Action, e.g. invoice.voided" value={filter.action} onChange={(e) => setFilter('action', e.target.value)} />
        <input aria-label="User" className="input" placeholder="User" value={filter.actor} onChange={(e) => setFilter('actor', e.target.value)} />
        <input aria-label="Record id" className="input" placeholder="Record id" value={filter.entityId} onChange={(e) => setFilter('entityId', e.target.value)} />
      </div>
      {loading && !data ? <Spinner /> : data && data.items.length === 0 ? <Empty>No entries match.</Empty> : (
        <div className="card overflow-x-auto p-0">
          <table className="w-full min-w-[820px] text-sm">
            <thead className="table-head">
              <tr>
                <th className="px-4 py-3">#</th>
                <th className="px-4 py-3">When</th>
                <th className="px-4 py-3">User</th>
                <th className="px-4 py-3">Action</th>
                <th className="px-4 py-3">Record</th>
                <th className="px-4 py-3">Details</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-cream-200">
              {data?.items.map((e) => {
                const link = entityLink(e)
                return (
                  <tr key={e.seq} className="align-top hover:bg-cream-50">
                    <td className="px-4 py-2.5 tabular-nums text-slate-500" title={`hash ${e.hash}`}>{e.seq}</td>
                    <td className="whitespace-nowrap px-4 py-2.5">{formatDateTime(e.at)}</td>
                    <td className="px-4 py-2.5 font-medium">{e.actor}</td>
                    <td className="px-4 py-2.5">
                      <span className="font-semibold text-brand-900">{actionLabel(e.action)}</span>
                      <div className="text-xs text-slate-500">{e.action}</div>
                    </td>
                    <td className="px-4 py-2.5">
                      <span className="text-xs uppercase tracking-wide text-slate-500">{e.entityType.replace('_', ' ')}</span>
                      <div>{link ? <Link to={link} className="text-brand-700 hover:underline">open</Link> : <span className="text-xs text-slate-400">{e.entityId ? e.entityId.slice(-8) : '—'}</span>}</div>
                    </td>
                    <td className="break-words px-4 py-2.5 text-slate-600">{e.summary}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
      {data && data.total > pageSize && (
        <div className="mt-3 flex items-center justify-between text-sm">
          <span className="text-slate-500">{offset + 1}–{Math.min(offset + pageSize, data.total)} of {data.total}</span>
          <div className="flex gap-2">
            <button className="btn-secondary" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - pageSize))}>Newer</button>
            <button className="btn-secondary" disabled={offset + pageSize >= data.total} onClick={() => setOffset(offset + pageSize)}>Older</button>
          </div>
        </div>
      )}
    </Page>
  )
}
