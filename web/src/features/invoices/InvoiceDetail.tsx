import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, idempotencyKey, unwrap } from '../../lib/api/client'
import { formatDate, formatDateTime } from '../../lib/format'
import { formatMoney } from '../../lib/money'
import { useAsync } from '../../lib/useAsync'
import { Badge, ErrorBanner, Field, Page, Sheet, Spinner } from '../../components/ui'

export function InvoiceDetail() {
  const { id = '' } = useParams()
  const nav = useNavigate()
  const { data, error, loading, reload } = useAsync(() => unwrap(api.GET('/invoices/{id}', { params: { path: { id } } })), [id])
  const [actionError, setActionError] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const [sendOpen, setSendOpen] = useState(false)
  const [voidOpen, setVoidOpen] = useState(false)

  if (loading && !data) return <Spinner />
  if (!data) return <Page title="Invoice" back="/invoices"><ErrorBanner error={error} /></Page>
  const inv = data.invoice
  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    setActionError(null)
    try {
      await fn()
      reload()
    } catch (e) {
      setActionError(e as Error)
    } finally {
      setBusy(false)
    }
  }
  const path = { params: { path: { id } } }
  return (
    <Page title={inv.number || 'Draft invoice'} back="/invoices">
      <ErrorBanner error={actionError ?? error} />
      <div className="card mb-4">
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div>
            <div className="text-sm text-slate-500">Bill to</div>
            <div className="font-semibold">{inv.billTo.name}</div>
            {inv.billTo.attention && <div className="text-sm">Att: {inv.billTo.attention}</div>}
          </div>
          <div className="text-right">
            <Badge status={inv.overdue ? 'overdue' : inv.status} />
            <div className="mt-1 text-2xl font-bold tabular-nums">{formatMoney(inv.totals.total, inv.currency)}</div>
          </div>
        </div>
        <dl className="mt-3 grid grid-cols-2 gap-2 text-sm md:grid-cols-4">
          <div><dt className="text-slate-500">Issued</dt><dd>{formatDate(inv.issueDate)}</dd></div>
          <div><dt className="text-slate-500">Due</dt><dd>{formatDate(inv.dueDate)}</dd></div>
          <div className="col-span-2"><dt className="text-slate-500">Terms</dt><dd>{inv.paymentTerms.text}</dd></div>
        </dl>
      </div>

      <div className="mb-4 grid grid-cols-2 gap-2 md:flex md:flex-wrap">
        {inv.status === 'draft' && (
          <>
            <Link to={`/invoices/${id}/edit`} className="btn-secondary">Edit</Link>
            <button className="btn-primary" disabled={busy} onClick={() => act(() => unwrap(api.POST('/invoices/{id}/issue', { ...path, body: {} })))}>Issue</button>
          </>
        )}
        {inv.status !== 'void' && <button className="btn-primary" disabled={busy} onClick={() => setSendOpen(true)}>{inv.status === 'draft' ? 'Issue & send' : 'Send'}</button>}
        <a className="btn-secondary" href={`/api/v1/invoices/${id}/pdf`} target="_blank" rel="noreferrer">PDF</a>
        {(inv.status === 'issued' || inv.status === 'sent') && (
          <>
            <button className="btn-secondary" disabled={busy} onClick={() => act(() => unwrap(api.POST('/invoices/{id}/mark-paid', path)))}>Mark paid</button>
            <button className="btn-danger" disabled={busy} onClick={() => setVoidOpen(true)}>Void</button>
          </>
        )}
        {inv.status === 'draft' && (
          <button className="btn-danger" disabled={busy} onClick={() => act(async () => { await unwrap(api.DELETE('/invoices/{id}', path)); nav('/invoices') })}>Delete</button>
        )}
      </div>

      <section className="card mb-4 p-0">
        <h2 className="p-4 pb-2 font-semibold">Lines</h2>
        <ul className="divide-y divide-slate-100 dark:divide-slate-800">
          {inv.lines.map((l, i) => (
            <li key={i} className="flex gap-3 px-4 py-3 text-sm">
              <div className="flex-1">
                <div>{l.description}</div>
                <div className="text-slate-500">{l.quantity} × {l.unitPrice}{l.discountPercent !== '0' ? ` − ${l.discountPercent}%` : ''}</div>
              </div>
              <div className="tabular-nums">{l.amount}</div>
            </li>
          ))}
        </ul>
        <dl className="space-y-1 border-t border-slate-100 p-4 text-sm dark:border-slate-800">
          <Row label="Subtotal" value={inv.totals.subtotal} />
          <Row label="Discount" value={inv.totals.discount} />
          {inv.vatPercent !== '0' && <Row label={`VAT ${inv.vatPercent}%`} value={inv.totals.vat} />}
          <div className="flex justify-between text-base font-bold"><dt>Total {inv.currency}</dt><dd className="tabular-nums">{inv.totals.total}</dd></div>
        </dl>
      </section>

      {inv.bank.accountNumber && (
        <section className="card mb-4 text-sm">
          <h2 className="mb-2 font-semibold">Pay into</h2>
          <p>{inv.bank.bankName} · {inv.bank.branch} · {inv.bank.accountType} {inv.bank.accountNumber} · Code {inv.bank.branchCode}</p>
        </section>
      )}

      <section className="card mb-4 text-sm">
        <h2 className="mb-2 font-semibold">E-mails</h2>
        {data.emails.length === 0 ? <p className="text-slate-500">Not sent yet.</p> : (
          <ul className="space-y-2">
            {data.emails.map((m) => (
              <li key={m.id} className="flex items-center justify-between gap-2">
                <span className="truncate">{m.to.join(', ')} · {formatDateTime(m.sentAt ?? m.nextAttemptAt)}</span>
                <Badge status={m.status} />
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="card text-sm">
        <h2 className="mb-2 font-semibold">History</h2>
        <ol className="space-y-1">
          {data.events.map((e, i) => (
            <li key={i} className="flex justify-between gap-2"><span>{e.type.replace(/_/g, ' ')} {e.detail && <span className="text-slate-500">· {e.detail}</span>}</span><span className="text-slate-500">{formatDateTime(e.at)}</span></li>
          ))}
        </ol>
      </section>

      <SendSheet open={sendOpen} onClose={() => setSendOpen(false)} onSend={(body) => act(async () => { await unwrap(api.POST('/invoices/{id}/send', { ...path, body, params: { path: { id }, header: { 'Idempotency-Key': idempotencyKey() } } })); setSendOpen(false) })} busy={busy} />
      <Sheet title="Void invoice" open={voidOpen} onClose={() => setVoidOpen(false)}>
        <VoidForm onVoid={(reason) => act(async () => { await unwrap(api.POST('/invoices/{id}/void', { ...path, body: { reason } })); setVoidOpen(false) })} />
      </Sheet>
    </Page>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return <div className="flex justify-between"><dt className="text-slate-500">{label}</dt><dd className="tabular-nums">{value}</dd></div>
}

function SendSheet({ open, onClose, onSend, busy }: { open: boolean; onClose: () => void; busy: boolean; onSend: (b: { to?: string[]; sendAt?: string }) => void }) {
  const [when, setWhen] = useState<'now' | 'later'>('now')
  const [at, setAt] = useState('')
  const [to, setTo] = useState('')
  return (
    <Sheet title="Send invoice" open={open} onClose={onClose}>
      <div className="space-y-4">
        <div className="grid grid-cols-2 gap-2" role="radiogroup">
          {(['now', 'later'] as const).map((w) => (
            <button key={w} role="radio" aria-checked={when === w} className={when === w ? 'btn-primary' : 'btn-secondary'} onClick={() => setWhen(w)}>
              {w === 'now' ? 'Send now' : 'Schedule'}
            </button>
          ))}
        </div>
        {when === 'later' && (
          <Field label="Send at">{(id) => <input id={id} className="input" type="datetime-local" value={at} onChange={(e) => setAt(e.target.value)} />}</Field>
        )}
        <Field label="To (optional)" hint="Leave empty to use the customer's e-mail and CC addresses.">
          {(id) => <input id={id} className="input" type="email" multiple inputMode="email" value={to} onChange={(e) => setTo(e.target.value)} />}
        </Field>
        <button
          className="btn-primary w-full"
          disabled={busy || (when === 'later' && !at)}
          onClick={() => onSend({ to: to ? to.split(',').map((s) => s.trim()).filter(Boolean) : undefined, sendAt: when === 'later' && at ? new Date(at).toISOString() : undefined })}
        >
          {when === 'now' ? 'Send now' : 'Schedule e-mail'}
        </button>
      </div>
    </Sheet>
  )
}

function VoidForm({ onVoid }: { onVoid: (reason: string) => void }) {
  const [reason, setReason] = useState('')
  return (
    <div className="space-y-4">
      <p className="text-sm text-slate-500">The number stays used and scheduled e-mails are cancelled. This cannot be undone.</p>
      <Field label="Reason">{(id) => <input id={id} className="input" value={reason} onChange={(e) => setReason(e.target.value)} />}</Field>
      <button className="btn-danger w-full" disabled={!reason.trim()} onClick={() => onVoid(reason)}>Void invoice</button>
    </div>
  )
}
