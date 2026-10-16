import { useState } from 'react'
import { Can } from '../../components/Access'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, idempotencyKey, unwrap, type InvoiceDetail } from '../../lib/api/client'
import { InvoiceActions } from '../../components/InvoiceActions'
import { formatDate, formatDateTime } from '../../lib/format'
import { formatMoney, groupAmount } from '../../lib/money'
import { useAsync } from '../../lib/useAsync'
import { Badge, ConfirmButton, ErrorBanner, Field, Page, Sheet, Spinner, StageTracker } from '../../components/ui'

export function InvoiceDetail() {
  const { id = '' } = useParams()
  const nav = useNavigate()
  const { data, error, loading, reload } = useAsync(() => unwrap(api.GET('/invoices/{id}', { params: { path: { id } } })), [id])
  const [actionError, setActionError] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const [sendOpen, setSendOpen] = useState(false)
  const [notice, setNotice] = useState('')

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
        <div className="mb-5">
          <StageTracker stage={inv.stage} issuedAt={inv.issuedAt} sentAt={inv.sentAt} paidAt={inv.paidAt ?? inv.overdueAt ?? inv.voidedAt} />
        </div>
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div>
            <div className="text-sm text-slate-500">Bill to</div>
            <div className="font-semibold">{inv.billTo.name}</div>
            {inv.billTo.attention && <div className="text-sm">Att: {inv.billTo.attention}</div>}
          </div>
          <div className="text-right">
            <Badge status={inv.stage} />
            <div className="mt-1 text-2xl font-bold tabular-nums">{formatMoney(inv.totals.total, inv.currency)}</div>
          </div>
        </div>
        <dl className="mt-3 grid grid-cols-2 gap-2 text-sm md:grid-cols-4">
          <div><dt className="text-slate-500">Issued</dt><dd>{formatDate(inv.issueDate)}</dd></div>
          <div><dt className="text-slate-500">Due</dt><dd>{formatDate(inv.dueDate)}</dd></div>
          <div className="col-span-2"><dt className="text-slate-500">Terms</dt><dd>{inv.paymentTerms.text}</dd></div>
        </dl>
      </div>

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <InvoiceActions inv={inv} showLabels onChanged={(m) => { setActionError(null); setNotice(m); reload() }} onError={setActionError} />
        {inv.status === 'draft' && (
          <>
            <Can need="invoices.edit"><Link to={`/invoices/${id}/edit`} className="inline-flex h-8 items-center rounded-lg px-3 text-xs font-semibold text-brand-800 ring-1 ring-cream-300 hover:bg-cream-50">Edit</Link></Can>
            <Can need="invoices.issue"><button className="inline-flex h-8 items-center rounded-lg px-3 text-xs font-semibold text-brand-800 ring-1 ring-cream-300 hover:bg-cream-50" disabled={busy} onClick={() => act(() => unwrap(api.POST('/invoices/{id}/issue', { ...path, body: {} })))}>Issue</button></Can>
            <Can need="invoices.delete"><ConfirmButton
              label="Delete draft"
              className="inline-flex h-8 items-center rounded-lg px-3 text-xs font-semibold text-red-700 ring-1 ring-red-200 hover:bg-red-50"
              disabled={busy}
              title="Delete this draft?"
              message="The draft and its lines are deleted. It has no invoice number yet, so no number is lost. This cannot be undone."
              confirmLabel="Delete draft"
              onConfirm={() => act(async () => { await unwrap(api.DELETE('/invoices/{id}', path)); nav('/invoices') })}
            /></Can>
          </>
        )}
        {inv.status !== 'void' && (
          // Sending a draft issues it, which also needs invoices.issue (as "Issue & send").
          <Can need={inv.status === 'draft' ? 'invoices.issue' : 'invoices.send'}>
          <Can need="invoices.send">
            <button className="inline-flex h-8 items-center rounded-lg px-3 text-xs font-semibold text-brand-800 ring-1 ring-cream-300 hover:bg-cream-50" disabled={busy} onClick={() => setSendOpen(true)}>Send later / other address…</button>
          </Can>
          </Can>
        )}
      </div>
      {notice && <div role="status" className="mb-4 rounded-xl border-l-4 border-brand-500 bg-white p-3 text-sm font-medium text-brand-900 shadow-sm">{notice}</div>}
      <WhoDidWhat events={data.events} />

      <section className="card mb-4 p-0">
        <h2 className="p-4 pb-2 font-semibold">Lines</h2>
        <ul className="divide-y divide-slate-100">
          {inv.lines.map((l, i) => (
            <li key={i} className="flex gap-3 px-4 py-3 text-sm">
              <div className="flex-1">
                <div>{l.description}</div>
                <div className="text-slate-500">{l.quantity} × {groupAmount(l.unitPrice)}{l.discountPercent !== '0' ? ` − ${l.discountPercent}%` : ''}</div>
              </div>
              <div className="tabular-nums">{groupAmount(l.amount)}</div>
            </li>
          ))}
        </ul>
        <dl className="space-y-1 border-t border-slate-100 p-4 text-sm">
          <Row label="Subtotal" value={groupAmount(inv.totals.subtotal)} />
          {Number(inv.totals.discount) !== 0 && <Row label="Discount" value={`-${groupAmount(inv.totals.discount)}`} />}
          {inv.vatPercent !== '0' && <Row label={`VAT ${inv.vatPercent}%`} value={groupAmount(inv.totals.vat)} />}
          <div className="flex justify-between text-base font-bold"><dt>Total {inv.currency}</dt><dd className="tabular-nums">{groupAmount(inv.totals.total)}</dd></div>
        </dl>
      </section>

      {inv.bank.accountNumber && (
        <section className="card mb-4 text-sm">
          <h2 className="mb-2 font-semibold">Pay into</h2>
          <dl className="grid grid-cols-2 gap-x-4 gap-y-1 md:grid-cols-4">
            {[
              ['Bank', inv.bank.bankName], ['Branch name', inv.bank.branch], ['Sort / branch code', inv.bank.branchCode],
              ['Account number', inv.bank.accountNumber], ['SWIFT', inv.bank.swiftBic], ['IBAN', inv.bank.iban], ['Country', inv.bank.country],
            ].filter(([, v]) => v).map(([k, v]) => (
              <div key={k}><dt className="text-xs text-slate-500">{k}</dt><dd className="font-medium">{v}</dd></div>
            ))}
          </dl>
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
        <div className="mb-3 flex items-center justify-between">
          <h2 className="font-semibold">Audit trail</h2>
          <Link to={`/audit?entityType=invoice&entityId=${id}`} className="text-xs font-semibold text-brand-700 hover:underline">Open in audit log</Link>
        </div>
        <ol className="relative space-y-3 border-l border-cream-300 pl-4">
          {data.events.map((e, i) => (
            <li key={i}>
              <span className="absolute -left-[5px] mt-1.5 h-2.5 w-2.5 rounded-full bg-brand-500" />
              <div className="flex flex-wrap items-baseline justify-between gap-x-3">
                <span><span className="font-semibold text-brand-900">{actionLabel(e.action)}</span> by <span className="font-medium">{e.actor}</span></span>
                <span className="text-xs text-slate-500">{formatDateTime(e.at)}</span>
              </div>
              {e.detail && <div className="text-xs text-slate-500">{e.detail}</div>}
            </li>
          ))}
        </ol>
      </section>

      <SendSheet open={sendOpen} onClose={() => setSendOpen(false)} onSend={(body) => act(async () => { await unwrap(api.POST('/invoices/{id}/send', { ...path, body, params: { path: { id }, header: { 'Idempotency-Key': idempotencyKey() } } })); setSendOpen(false) })} busy={busy} />
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

const actionLabels: Record<string, string> = {
  'invoice.created': 'Created', 'invoice.updated': 'Edited', 'invoice.issued': 'Issued', 'invoice.email_queued': 'E-mail queued',
  'invoice.email_sent': 'E-mailed', 'invoice.email_failed': 'E-mail failed', 'invoice.email_cancelled': 'E-mail cancelled',
  'invoice.email_retried': 'E-mail retried', 'invoice.overdue': 'Marked overdue', 'invoice.paid': 'Marked paid',
  'invoice.voided': 'Voided', 'invoice.deleted': 'Deleted', 'invoice.pdf_viewed': 'PDF viewed',
}

export function actionLabel(a: string): string {
  return actionLabels[a] ?? a.replace(/^[a-z_]+\./, '').replace(/_/g, ' ').replace(/^./, (c) => c.toUpperCase())
}

/** WhoDidWhat summarises the key actions on an invoice and who performed them. */
function WhoDidWhat({ events }: { events: InvoiceDetail['events'] }) {
  const last = (action: string) => [...events].reverse().find((e) => e.action === action)
  const rows: [string, ReturnType<typeof last>][] = [
    ['Created', last('invoice.created')], ['Issued', last('invoice.issued')], ['E-mailed', last('invoice.email_queued')],
    ['Marked overdue', last('invoice.overdue')], ['Paid', last('invoice.paid')], ['Voided', last('invoice.voided')],
  ]
  const shown = rows.filter(([, e]) => e)
  if (shown.length === 0) return null
  return (
    <dl className="mb-4 grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
      {shown.map(([label, e]) => (
        <div key={label} className="rounded-xl bg-white p-3 ring-1 ring-cream-300">
          <dt className="text-[11px] font-semibold uppercase tracking-wide text-slate-500">{label} by</dt>
          <dd className="truncate font-semibold text-brand-900" title={e!.actor}>{e!.actor}</dd>
          <dd className="text-xs text-slate-500">{formatDateTime(e!.at)}</dd>
        </div>
      ))}
    </dl>
  )
}
