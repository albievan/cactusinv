import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Can } from '../../components/Access'
import { useCan } from '../../components/auth'
import { api, idempotencyKey, unwrap } from '../../lib/api/client'
import { formatDate, formatDateTime } from '../../lib/format'
import { formatMoney, groupAmount } from '../../lib/money'
import { useAsync } from '../../lib/useAsync'
import { Badge, ConfirmButton, ErrorBanner, Field, Page, Sheet, Spinner, quoteStageLabels } from '../../components/ui'
import { IconCheck, IconEdit, IconFilePdf, IconInvoice, IconPersonPlus, IconSend, IconTrash, IconBan } from '../../components/icons'
import { actionLabel } from '../invoices/InvoiceDetail'
import { ConvertSheet } from './AdhocCustomers'

const tnum = { fontVariantNumeric: 'tabular-nums' } as const

export function QuoteDetail() {
  const { id = '' } = useParams()
  const nav = useNavigate()
  const can = useCan()
  const { data, error, loading, reload } = useAsync(() => unwrap(api.GET('/quotes/{id}', { params: { path: { id } } })), [id])
  const [actionError, setActionError] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [sendOpen, setSendOpen] = useState(false)
  const [declineOpen, setDeclineOpen] = useState(false)
  const [converting, setConverting] = useState(false)

  if (loading && !data) return <Spinner />
  if (!data) return <Page title="Quote" back="/quotes"><ErrorBanner error={error} /></Page>
  const qt = data.quote
  const path = { params: { path: { id } } }
  const act = async (fn: () => Promise<unknown>, msg: string) => {
    setBusy(true)
    setActionError(null)
    try {
      await fn()
      setNotice(msg)
      reload()
    } catch (e) {
      setActionError(e as Error)
    } finally {
      setBusy(false)
    }
  }
  const open = qt.status === 'draft' || qt.status === 'sent'
  const canConvert = open || qt.status === 'accepted'
  const adhocOnly = !qt.customerId
  return (
    <Page title={qt.number} back="/quotes">
      <ErrorBanner error={actionError ?? error} />
      <div className="card card-body mb-3">
        <div className="d-flex flex-wrap align-items-start justify-content-between gap-2">
          <div>
            <div className="small text-body-secondary">Quotation for{adhocOnly && ' (ad-hoc customer)'}</div>
            <div className="fw-semibold">{qt.billTo.name}</div>
            {qt.billTo.attention && <div className="small">Att: {qt.billTo.attention}</div>}
          </div>
          <div className="text-end">
            <Badge status={qt.stage} label={quoteStageLabels[qt.stage]} />
            <div className="mt-1 fs-4 fw-bold" style={tnum}>{formatMoney(qt.totals.total, qt.currency)}</div>
          </div>
        </div>
        <dl className="row row-cols-2 row-cols-md-4 g-2 mt-2 mb-0 small">
          <div><dt className="fw-normal text-body-secondary">Date</dt><dd className="mb-0">{formatDate(qt.issueDate)}</dd></div>
          <div><dt className="fw-normal text-body-secondary">Valid until</dt><dd className="mb-0">{formatDate(qt.validUntil)}</dd></div>
          {qt.reference && <div><dt className="fw-normal text-body-secondary">Reference</dt><dd className="mb-0">{qt.reference}</dd></div>}
          <div><dt className="fw-normal text-body-secondary" title="Printed on the PDF; it changes if anything printed changes.">Verification code</dt><dd className="mb-0 font-monospace">{qt.fingerprint}</dd></div>
        </dl>
        {qt.status === 'declined' && qt.declineReason && <div className="small text-danger mt-2">Declined: {qt.declineReason}</div>}
        {qt.invoiceId && (
          <div className="small mt-2">
            Invoiced: <Can need="invoices.view"><Link to={`/invoices/${qt.invoiceId}`}>{data.invoiceNumber || 'draft invoice'}</Link></Can>
          </div>
        )}
      </div>

      <div className="mb-3 d-flex flex-wrap align-items-center gap-2">
        <a className="btn btn-sm btn-outline-primary" href={`/api/v1/quotes/${id}/pdf`} target="_blank" rel="noreferrer"><IconFilePdf width={15} /> PDF</a>
        {qt.status !== 'declined' && qt.status !== 'converted' && (
          <Can need="quotes.send"><button className="btn btn-sm btn-outline-primary" disabled={busy} onClick={() => setSendOpen(true)}><IconSend width={15} /> E-mail</button></Can>
        )}
        {open && <Can need="quotes.edit"><Link to={`/quotes/${id}/edit`} className="btn btn-sm btn-outline-primary"><IconEdit width={15} /> Edit</Link></Can>}
        {open && (
          <Can need="quotes.outcome">
            <button className="btn btn-sm btn-outline-success" disabled={busy} onClick={() => act(() => unwrap(api.POST('/quotes/{id}/accept', path)), `${qt.number} marked accepted.`)}><IconCheck width={15} /> Accepted</button>
          </Can>
        )}
        {canConvert && (
          <Can need="quotes.outcome"><button className="btn btn-sm btn-outline-danger" disabled={busy} onClick={() => setDeclineOpen(true)}><IconBan width={15} /> Declined</button></Can>
        )}
        {canConvert && adhocOnly && data.adhocCustomer && (
          <Can need="customers.create"><button className="btn btn-sm btn-primary" disabled={busy} onClick={() => setConverting(true)}><IconPersonPlus width={15} /> Make customer</button></Can>
        )}
        {canConvert && !adhocOnly && can(['quotes.convert']) && can('invoices.create') && (
          <ConfirmButton
            danger={false}
            className="btn btn-sm btn-primary"
            disabled={busy}
            label={<><IconInvoice width={15} /> Convert to invoice</>}
            title={`Turn ${qt.number} into an invoice?`}
            message={<>A draft invoice is made for {qt.billTo.name} with the quoted products, prices, VAT and notes. Check it and issue it from the invoice page.</>}
            confirmLabel="Make draft invoice"
            onConfirm={async () => {
              try {
                const inv = await unwrap(api.POST('/quotes/{id}/convert', path))
                nav(`/invoices/${inv.id}`)
              } catch (e) {
                setActionError(e as Error)
              }
            }}
          />
        )}
        {qt.status === 'draft' && !qt.sentAt && (
          <Can need="quotes.delete"><ConfirmButton
            label={<><IconTrash width={15} /> Delete</>}
            className="btn btn-sm btn-outline-danger"
            disabled={busy}
            title="Delete this quote?"
            message="The quote is deleted; its number is not used again. This cannot be undone."
            confirmLabel="Delete quote"
            onConfirm={async () => { try { await unwrap(api.DELETE('/quotes/{id}', path)); nav('/quotes') } catch (e) { setActionError(e as Error) } }}
          /></Can>
        )}
      </div>
      {adhocOnly && canConvert && (
        <p className="small text-body-secondary">To turn this quote into an invoice, first make {qt.billTo.name} a customer.</p>
      )}
      {notice && <div role="status" className="mb-3 rounded-3 border-start border-4 border-primary bg-white p-3 small fw-medium text-primary-emphasis shadow-sm">{notice}</div>}

      <section className="card mb-3 overflow-hidden">
        <h2 className="fs-6 fw-semibold p-3 pb-2 mb-0">Lines</h2>
        <ul className="list-group list-group-flush">
          {qt.lines.map((l, i) => (
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
          <Row label="Subtotal" value={groupAmount(qt.totals.subtotal)} />
          {Number(qt.totals.discount) !== 0 && <Row label="Discount" value={`-${groupAmount(qt.totals.discount)}`} />}
          {qt.vatPercent !== '0' && <Row label={`VAT ${qt.vatPercent}%`} value={groupAmount(qt.totals.vat)} />}
          <div className="d-flex justify-content-between fs-6 fw-bold"><dt>Total {qt.currency}</dt><dd className="mb-0" style={tnum}>{groupAmount(qt.totals.total)}</dd></div>
        </dl>
      </section>

      {qt.terms && (
        <section className="card card-body mb-3 small">
          <h2 className="fs-6 fw-semibold mb-2">Terms</h2>
          <p className="mb-0" style={{ whiteSpace: 'pre-wrap' }}>{qt.terms}</p>
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
          <Link to={`/audit?entityType=quote&entityId=${id}`} className="fs-8 fw-semibold">Open in audit log</Link>
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

      <SendQuoteSheet
        open={sendOpen}
        busy={busy}
        needsAddress={adhocOnly && !data.adhocCustomer?.email}
        onClose={() => setSendOpen(false)}
        onSend={(body) => act(async () => {
          await unwrap(api.POST('/quotes/{id}/send', { ...path, body, params: { path: { id }, header: { 'Idempotency-Key': idempotencyKey() } } }))
          setSendOpen(false)
        }, `${qt.number} queued to be e-mailed.`)}
      />
      <DeclineSheet open={declineOpen} busy={busy} onClose={() => setDeclineOpen(false)} onDecline={(reason) => act(async () => {
        await unwrap(api.POST('/quotes/{id}/decline', { ...path, body: { reason } }))
        setDeclineOpen(false)
      }, `${qt.number} marked declined.`)} />
      {converting && data.adhocCustomer && (
        <ConvertSheet adhoc={data.adhocCustomer} onClose={() => setConverting(false)} onDone={(c) => { setConverting(false); setNotice(`${c.name} is now customer ${c.code}: this quote can be turned into an invoice.`); reload() }} />
      )}
    </Page>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return <div className="d-flex justify-content-between"><dt className="fw-normal text-body-secondary">{label}</dt><dd className="mb-0" style={tnum}>{value}</dd></div>
}

function SendQuoteSheet({ open, onClose, onSend, busy, needsAddress }: { open: boolean; onClose: () => void; busy: boolean; needsAddress: boolean; onSend: (b: { to?: string[]; sendAt?: string }) => void }) {
  const [when, setWhen] = useState<'now' | 'later'>('now')
  const [at, setAt] = useState('')
  const [to, setTo] = useState('')
  return (
    <Sheet title="E-mail quote" open={open} onClose={onClose}>
      <div className="vstack gap-3">
        <p className="small text-body-secondary mb-0">Sent with the quote e-mail from Settings → Quotes; the PDF is attached and can be printed but not changed.</p>
        <div className="btn-group w-100" role="radiogroup">
          {(['now', 'later'] as const).map((w) => (
            <button key={w} role="radio" aria-checked={when === w} className={`btn btn-outline-primary${when === w ? ' active' : ''}`} onClick={() => setWhen(w)}>
              {w === 'now' ? 'Send now' : 'Schedule'}
            </button>
          ))}
        </div>
        {when === 'later' && <Field label="Send at">{(fid) => <input id={fid} className="form-control" type="datetime-local" value={at} onChange={(e) => setAt(e.target.value)} />}</Field>}
        <Field label={needsAddress ? 'To' : 'To (optional)'} hint={needsAddress ? 'The ad-hoc customer has no e-mail address yet.' : "Leave empty to use the customer's e-mail address."}>
          {(fid) => <input id={fid} className="form-control" type="email" multiple inputMode="email" value={to} onChange={(e) => setTo(e.target.value)} required={needsAddress} />}
        </Field>
        <button
          className="btn btn-primary w-100"
          disabled={busy || (when === 'later' && !at) || (needsAddress && !to)}
          onClick={() => onSend({ to: to ? to.split(',').map((s) => s.trim()).filter(Boolean) : undefined, sendAt: when === 'later' && at ? new Date(at).toISOString() : undefined })}
        >
          {when === 'now' ? 'Send now' : 'Schedule e-mail'}
        </button>
      </div>
    </Sheet>
  )
}

function DeclineSheet({ open, onClose, onDecline, busy }: { open: boolean; onClose: () => void; busy: boolean; onDecline: (reason: string) => void }) {
  const [reason, setReason] = useState('')
  return (
    <Sheet title="Mark declined" open={open} onClose={onClose}>
      <div className="vstack gap-3">
        <Field label="Reason (optional)">{(fid) => <input id={fid} className="form-control" value={reason} onChange={(e) => setReason(e.target.value)} maxLength={500} />}</Field>
        <button className="btn btn-danger w-100" disabled={busy} onClick={() => onDecline(reason)}>Mark declined</button>
      </div>
    </Sheet>
  )
}
