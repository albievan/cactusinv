import { useState } from 'react'
import { Can } from '../../components/Access'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, idempotencyKey, unwrap, type InvoiceDetail } from '../../lib/api/client'
import { InvoiceActions } from '../../components/InvoiceActions'
import { formatDate, formatDateTime, todayISO } from '../../lib/format'
import { formatMoney, groupAmount } from '../../lib/money'
import { useAsync } from '../../lib/useAsync'
import { Badge, ConfirmButton, ErrorBanner, Field, Page, Sheet, Spinner, StageTracker } from '../../components/ui'
import { IconEdit, IconTrash } from '../../components/icons'

const tnum = { fontVariantNumeric: 'tabular-nums' } as const

export function InvoiceDetail() {
  const { id = '' } = useParams()
  const nav = useNavigate()
  const { data, error, loading, reload } = useAsync(() => unwrap(api.GET('/invoices/{id}', { params: { path: { id } } })), [id])
  const [actionError, setActionError] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const [sendOpen, setSendOpen] = useState(false)
  const [notice, setNotice] = useState('')
  // A draft dated in the past is issued with a date the user picks: it used to be issued
  // backdated silently (perhaps overdue at once, out of number order).
  const [askDate, setAskDate] = useState(false)
  // thenSend: the date was asked for "Send later / other address…", which opens once issued.
  const [thenSend, setThenSend] = useState(false)

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
      <div className="card card-body mb-3">
        <div className="mb-4">
          <StageTracker stage={inv.stage} issuedAt={inv.issuedAt} sentAt={inv.sentAt} paidAt={inv.paidAt ?? inv.overdueAt ?? inv.voidedAt} />
        </div>
        <div className="d-flex flex-wrap align-items-start justify-content-between gap-2">
          <div>
            <div className="small text-body-secondary">Bill to</div>
            <div className="fw-semibold">{inv.billTo.name}</div>
            {inv.billTo.attention && <div className="small">Att: {inv.billTo.attention}</div>}
          </div>
          <div className="text-end">
            <Badge status={inv.stage} />
            <div className="mt-1 fs-4 fw-bold" style={tnum}>{formatMoney(inv.totals.total, inv.currency)}</div>
          </div>
        </div>
        <dl className="row row-cols-2 row-cols-md-4 g-2 mt-2 mb-0 small">
          <div><dt className="fw-normal text-body-secondary">Issued</dt><dd className="mb-0">{formatDate(inv.issueDate)}</dd></div>
          <div><dt className="fw-normal text-body-secondary">Due</dt><dd className="mb-0">{formatDate(inv.dueDate)}</dd></div>
          <div className="col-12 col-md-6"><dt className="fw-normal text-body-secondary">Terms</dt><dd className="mb-0">{inv.paymentTerms.text}</dd></div>
        </dl>
      </div>

      <div className="mb-3 d-flex flex-wrap align-items-center gap-2">
        <InvoiceActions inv={inv} showLabels withNotes onChanged={(m) => { setActionError(null); setNotice(m); reload() }} onError={setActionError} />
        {inv.status === 'draft' && (
          <>
            <Can need="invoices.edit"><Link to={`/invoices/${id}/edit`} className="btn btn-sm btn-outline-primary"><IconEdit width={15} /> Edit</Link></Can>
            <Can need="invoices.issue"><button className="btn btn-sm btn-outline-primary" disabled={busy} onClick={() => (inv.issueDate < todayISO() ? setAskDate(true) : act(() => unwrap(api.POST('/invoices/{id}/issue', { ...path, body: {} }))))}>Issue</button></Can>
            <Sheet title="Issue with which date?" open={askDate} onClose={() => { setAskDate(false); setThenSend(false) }}>
              <p className="small text-body-secondary mb-0">This draft is dated {formatDate(inv.issueDate)}. The due date follows from the date it is issued with.</p>
              <div className="mt-3 d-flex flex-wrap justify-content-end gap-2">
                <button className="btn btn-outline-primary" disabled={busy} onClick={() => { setAskDate(false); setThenSend(false); void act(async () => { await unwrap(api.POST('/invoices/{id}/issue', { ...path, body: {} })); if (thenSend) setSendOpen(true) }) }}>
                  Keep {formatDate(inv.issueDate)}
                </button>
                <button className="btn btn-primary" disabled={busy} onClick={() => { setAskDate(false); setThenSend(false); void act(async () => { await unwrap(api.POST('/invoices/{id}/issue', { ...path, body: { issueDate: todayISO() } })); if (thenSend) setSendOpen(true) }) }}>
                  Issue dated today
                </button>
              </div>
            </Sheet>
            <Can need="invoices.delete"><ConfirmButton
              label={<><IconTrash width={15} /> Delete draft</>}
              className="btn btn-sm btn-outline-danger"
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
            <button className="btn btn-sm btn-outline-primary" disabled={busy} onClick={() => (inv.status === 'draft' && inv.issueDate < todayISO() ? (setThenSend(true), setAskDate(true)) : setSendOpen(true))}>Send later / other address…</button>
          </Can>
          </Can>
        )}
      </div>
      {notice && <div role="status" className="mb-3 rounded-3 border-start border-4 border-primary bg-white p-3 small fw-medium text-primary-emphasis shadow-sm">{notice}</div>}
      <WhoDidWhat events={data.events} />

      <section className="card mb-3 overflow-hidden">
        <h2 className="fs-6 fw-semibold p-3 pb-2 mb-0">Lines</h2>
        <ul className="list-group list-group-flush">
          {inv.lines.map((l, i) => (
            <li key={i} className="list-group-item d-flex gap-3 px-3 py-3 small">
              <div className="flex-grow-1 min-w-0">
                <div>{l.description}</div>
                <div className="text-body-secondary">{l.quantity} × {groupAmount(l.unitPrice)}{l.discountPercent !== '0' ? ` − ${l.discountPercent}%` : ''}</div>
              </div>
              <div style={tnum}>{groupAmount(l.amount)}</div>
            </li>
          ))}
        </ul>
        <dl className="vstack gap-1 border-top p-3 mb-0 small">
          <Row label="Subtotal" value={groupAmount(inv.totals.subtotal)} />
          {Number(inv.totals.discount) !== 0 && <Row label="Discount" value={`-${groupAmount(inv.totals.discount)}`} />}
          {inv.vatPercent !== '0' && <Row label={`VAT ${inv.vatPercent}%`} value={groupAmount(inv.totals.vat)} />}
          <div className="d-flex justify-content-between fs-6 fw-bold"><dt>Total {inv.currency}</dt><dd className="mb-0" style={tnum}>{groupAmount(inv.totals.total)}</dd></div>
        </dl>
      </section>

      {inv.bank.accountNumber && (
        <section className="card card-body mb-3 small">
          <h2 className="fs-6 fw-semibold mb-2">Pay into</h2>
          <dl className="row row-cols-2 row-cols-md-4 gx-3 gy-1 mb-0">
            {[
              ['Bank', inv.bank.bankName], ['Branch name', inv.bank.branch], ['Sort / branch code', inv.bank.branchCode],
              ['Account number', inv.bank.accountNumber], ['SWIFT', inv.bank.swiftBic], ['IBAN', inv.bank.iban], ['Country', inv.bank.country],
            ].filter(([, v]) => v).map(([k, v]) => (
              <div key={k}><dt className="fs-8 fw-normal text-body-secondary">{k}</dt><dd className="fw-medium mb-0">{v}</dd></div>
            ))}
          </dl>
        </section>
      )}

      <section className="card card-body mb-3 small">
        <h2 className="fs-6 fw-semibold mb-2">E-mails</h2>
        {data.emails.length === 0 ? <p className="text-body-secondary mb-0">Not sent yet.</p> : (
          <ul className="list-unstyled vstack gap-2 mb-0">
            {data.emails.map((m) => (
              <li key={m.id} className="d-flex align-items-center justify-content-between gap-2">
                <span className="min-w-0 text-truncate">{m.to.join(', ')} · {formatDateTime(m.sentAt ?? m.nextAttemptAt)}</span>
                <Badge status={m.status} />
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="card card-body small">
        <div className="mb-3 d-flex align-items-center justify-content-between">
          <h2 className="fs-6 fw-semibold mb-0">Audit trail</h2>
          <Link to={`/audit?entityType=invoice&entityId=${id}`} className="fs-8 fw-semibold">Open in audit log</Link>
        </div>
        <ol className="position-relative list-unstyled vstack gap-3 border-start ps-3 mb-0">
          {data.events.map((e, i) => (
            <li key={i}>
              <span className="position-absolute mt-1 rounded-circle bg-primary" style={{ left: -5, width: 10, height: 10 }} />
              <div className="d-flex flex-wrap align-items-baseline justify-content-between column-gap-3">
                <span><span className="fw-semibold text-primary-emphasis">{actionLabel(e.action)}</span> by <span className="fw-medium">{e.actor}</span></span>
                <span className="fs-8 text-body-secondary">{formatDateTime(e.at)}</span>
              </div>
              {e.detail && <div className="fs-8 text-body-secondary">{e.detail}</div>}
            </li>
          ))}
        </ol>
      </section>

      <SendSheet open={sendOpen} onClose={() => setSendOpen(false)} onSend={(body) => act(async () => { await unwrap(api.POST('/invoices/{id}/send', { ...path, body, params: { path: { id }, header: { 'Idempotency-Key': idempotencyKey() } } })); setSendOpen(false) })} busy={busy} />
    </Page>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return <div className="d-flex justify-content-between"><dt className="fw-normal text-body-secondary">{label}</dt><dd className="mb-0" style={tnum}>{value}</dd></div>
}

function SendSheet({ open, onClose, onSend, busy }: { open: boolean; onClose: () => void; busy: boolean; onSend: (b: { to?: string[]; sendAt?: string }) => void }) {
  const [when, setWhen] = useState<'now' | 'later'>('now')
  const [at, setAt] = useState('')
  const [to, setTo] = useState('')
  return (
    <Sheet title="Send invoice" open={open} onClose={onClose}>
      <div className="vstack gap-3">
        <div className="btn-group w-100" role="radiogroup">
          {(['now', 'later'] as const).map((w) => (
            <button key={w} role="radio" aria-checked={when === w} className={`btn btn-outline-primary${when === w ? ' active' : ''}`} onClick={() => setWhen(w)}>
              {w === 'now' ? 'Send now' : 'Schedule'}
            </button>
          ))}
        </div>
        {when === 'later' && (
          <Field label="Send at">{(id) => <input id={id} className="form-control" type="datetime-local" value={at} onChange={(e) => setAt(e.target.value)} />}</Field>
        )}
        <Field label="To (optional)" hint="Leave empty to use the customer's e-mail and CC addresses.">
          {(id) => <input id={id} className="form-control" type="email" multiple inputMode="email" value={to} onChange={(e) => setTo(e.target.value)} />}
        </Field>
        <button
          className="btn btn-primary w-100"
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
  'invoice.voided': 'Voided', 'invoice.deleted': 'Deleted', 'invoice.pdf_viewed': 'PDF viewed', 'invoice.notes_updated': 'Notes edited',
  'quote.created': 'Created', 'quote.updated': 'Edited', 'quote.email_queued': 'E-mail queued', 'quote.email_sent': 'E-mailed',
  'quote.email_failed': 'E-mail failed', 'quote.accepted': 'Marked accepted', 'quote.declined': 'Marked declined',
  'quote.converted': 'Turned into an invoice', 'quote.invoice_deleted': 'Its draft invoice was deleted', 'quote.pdf_viewed': 'PDF viewed',
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
    <dl className="d-grid gap-2 mb-3" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(9rem, 1fr))' }}>
      {shown.map(([label, e]) => (
        <div key={label} className="min-w-0 rounded-3 bg-white p-3 border">
          <dt className="fs-8 fw-semibold text-uppercase text-body-secondary">{label} by</dt>
          <dd className="text-truncate fw-semibold text-primary-emphasis mb-0" title={e!.actor}>{e!.actor}</dd>
          <dd className="fs-8 text-body-secondary mb-0">{formatDateTime(e!.at)}</dd>
        </div>
      ))}
    </dl>
  )
}
