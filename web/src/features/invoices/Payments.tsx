import { useState } from 'react'
import { Can } from '../../components/Access'
import { api, unwrap, type Invoice, type InvoiceDetail } from '../../lib/api/client'
import { formatDate, todayISO } from '../../lib/format'
import { formatMoney } from '../../lib/money'
import { ConfirmButton, Field, Sheet } from '../../components/ui'
import { IconTrash } from '../../components/icons'

const tnum = { fontVariantNumeric: 'tabular-nums' } as const

/**
 * Payments lists the money received against an issued invoice (M58), with what is still
 * owed. Whoever may mark invoices paid records a part-payment here, or removes one entered by
 * mistake (a paid invoice opens again). Drafts and void invoices have none.
 */
export function Payments({ inv, payments, busy, act }: {
  inv: Invoice
  payments: InvoiceDetail['payments']
  busy: boolean
  act: (fn: () => Promise<unknown>) => Promise<void>
}) {
  const [recording, setRecording] = useState(false)
  if (inv.status === 'draft' || (inv.status === 'void' && payments.length === 0)) return null
  const open = inv.status === 'issued' || inv.status === 'sent' || inv.status === 'overdue'
  const path = { params: { path: { id: inv.id } } }
  return (
    <section className="card card-body mb-3 small" aria-labelledby="payments-heading">
      <div className="mb-2 d-flex flex-wrap align-items-center justify-content-between gap-2">
        <h2 id="payments-heading" className="fs-6 fw-semibold mb-0">Payments</h2>
        {open && (
          <Can need="invoices.pay">
            <button className="btn btn-sm btn-outline-primary" disabled={busy} onClick={() => setRecording(true)}>
              <i className="bi bi-plus-lg me-1" aria-hidden="true" />Record a payment
            </button>
          </Can>
        )}
      </div>
      {payments.length === 0 ? <p className="text-body-secondary mb-2">No payments recorded yet.</p> : (
        <ul className="list-group list-group-flush mb-2">
          {payments.map((p) => (
            <li key={p.id} className="list-group-item d-flex align-items-center gap-3 px-0">
              <div className="flex-grow-1 min-w-0">
                <div>{formatDate(p.receivedOn)}</div>
                {p.reference && <div className="text-body-secondary text-truncate">{p.reference}</div>}
              </div>
              <div className="fw-medium" style={tnum}>{formatMoney(p.amount, inv.currency)}</div>
              {inv.status !== 'void' && (
                <Can need="invoices.pay">
                  <ConfirmButton
                    label={<IconTrash width={15} />}
                    ariaLabel={`Remove the payment of ${formatMoney(p.amount, inv.currency)} on ${formatDate(p.receivedOn)}`}
                    className="btn btn-sm btn-outline-danger btn-icon"
                    disabled={busy}
                    title="Remove this payment?"
                    message={inv.status === 'paid'
                      ? 'Use this for a payment entered by mistake or one that bounced. The invoice is no longer paid and shows the amount as owed again. The removal is recorded in the audit trail.'
                      : 'Use this for a payment entered by mistake or one that bounced. The amount is owed again. The removal is recorded in the audit trail.'}
                    confirmLabel="Remove payment"
                    onConfirm={() => act(() => unwrap(api.POST('/invoices/{id}/payments/{paymentId}/delete', { params: { path: { id: inv.id, paymentId: p.id } } })))}
                  />
                </Can>
              )}
            </li>
          ))}
        </ul>
      )}
      <dl className="vstack gap-1 border-top pt-2 mb-0">
        <div className="d-flex justify-content-between"><dt className="fw-normal text-body-secondary">Paid</dt><dd className="mb-0" style={tnum}>{formatMoney(inv.amountPaid, inv.currency)}</dd></div>
        <div className="d-flex justify-content-between fw-bold"><dt>Still owed</dt><dd className="mb-0" style={tnum}>{formatMoney(inv.balance, inv.currency)}</dd></div>
      </dl>
      {recording && (
        <RecordSheet
          inv={inv}
          busy={busy}
          onClose={() => setRecording(false)}
          onRecord={(body) => act(async () => { await unwrap(api.POST('/invoices/{id}/payments', { ...path, body })); setRecording(false) })}
        />
      )}
    </section>
  )
}

function RecordSheet({ inv, busy, onClose, onRecord }: {
  inv: Invoice
  busy: boolean
  onClose: () => void
  onRecord: (body: { amount: string; receivedOn: string; reference: string }) => void
}) {
  const [amount, setAmount] = useState(inv.balance)
  const [receivedOn, setReceivedOn] = useState(todayISO())
  const [reference, setReference] = useState('')
  const valid = Number(amount) > 0 && Number(amount) <= Number(inv.balance) && receivedOn !== '' && receivedOn <= todayISO()
  return (
    <Sheet title={`Record a payment on ${inv.number}`} open onClose={onClose}>
      <form className="vstack gap-3" onSubmit={(e) => { e.preventDefault(); if (valid) onRecord({ amount: amount.trim(), receivedOn, reference: reference.trim() }) }}>
        <p className="small text-body-secondary mb-0">
          Still owed: <span className="fw-semibold" style={tnum}>{formatMoney(inv.balance, inv.currency)}</span>. When the payments cover the total, the invoice is marked paid.
        </p>
        <Field label={`Amount received (${inv.currency})`} hint={`At most ${formatMoney(inv.balance, inv.currency)}.`}>
          {(id) => <input id={id} className="form-control" inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} autoFocus />}
        </Field>
        <Field label="Received on">
          {(id) => <input id={id} className="form-control" type="date" max={todayISO()} value={receivedOn} onChange={(e) => setReceivedOn(e.target.value)} />}
        </Field>
        <Field label="Reference (optional)" hint="The customer's payment reference or a note, e.g. EFT 4411.">
          {(id) => <input id={id} className="form-control" maxLength={200} value={reference} onChange={(e) => setReference(e.target.value)} />}
        </Field>
        <button type="submit" className="btn btn-primary w-100" disabled={busy || !valid}>Record payment</button>
      </form>
    </Sheet>
  )
}
