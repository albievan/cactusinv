import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { ApiError, api, unwrap, type Schemas } from '../../lib/api/client'
import { formatDate, formatDateTime } from '../../lib/format'
import { formatMoney } from '../../lib/money'
import { Field, Spinner } from '../../components/ui'
import { IconCheck, IconFilePdf } from '../../components/icons'
import { CactusMark } from '../../components/Logo'

type InvoiceResponse = Schemas['InvoiceResponse']

const tnum = { fontVariantNumeric: 'tabular-nums' } as const

/**
 * InvoicePaidPage is /i/:token, the page the "Mark as paid" button in an invoice e-mail
 * opens (M42). No sign-in: the token is the key. The customer tells the business they paid,
 * with an optional note; it records no payment (the business checks and marks it paid).
 * Opening the page changes nothing.
 */
export function InvoicePaidPage() {
  const { token = '' } = useParams()
  const [inv, setInv] = useState<InvoiceResponse>()
  const [missing, setMissing] = useState(false)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState(false)

  useEffect(() => {
    unwrap(api.GET('/public/invoices/{token}', { params: { path: { token } } }))
      .then(setInv)
      .catch((e) => (e instanceof ApiError && e.problem.status === 404 ? setMissing(true) : setError('The invoice could not be loaded. Please try again later.')))
  }, [token])

  const markPaid = async () => {
    setBusy(true)
    setError('')
    try {
      setInv(await unwrap(api.POST('/public/invoices/{token}/paid', { params: { path: { token } }, body: { note } })))
      setDone(true)
    } catch (e) {
      setError(e instanceof ApiError ? e.problem.detail || e.problem.title : 'Your message could not be sent. Please try again.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="min-vh-100 bg-paper px-3 py-4 py-sm-5">
      <div className="mx-auto" style={{ maxWidth: '40rem' }}>
        <div className="d-flex align-items-center gap-2 mb-4">
          <CactusMark size={32} />
          <span className="fw-semibold text-body-secondary small">{inv?.businessName || 'Invoice'}</span>
        </div>
        {missing ? (
          <section className="card card-body">
            <h1 className="h4 mb-2">This link does not work</h1>
            <p className="mb-0 text-body-secondary">The invoice may have been withdrawn, or the link was copied incompletely. Please contact the business that sent it.</p>
          </section>
        ) : !inv ? (
          error ? <div role="alert" className="alert alert-danger">{error}</div> : <Spinner />
        ) : (
          <section className="card card-body vstack gap-3">
            <div className="d-flex flex-wrap align-items-start justify-content-between gap-2">
              <div>
                <h1 className="h4 mb-1">Invoice {inv.number}</h1>
                <div className="small text-body-secondary">from {inv.businessName} to {inv.billToName}</div>
              </div>
              <div className="text-end">
                <div className="fs-4 fw-bold" style={tnum}>{formatMoney(inv.total, inv.currency)}</div>
                <div className="small text-body-secondary">Due {formatDate(inv.dueDate)}</div>
              </div>
            </div>
            <a className="btn btn-outline-primary align-self-start" href={`/api/v1/public/invoices/${token}/pdf`} target="_blank" rel="noreferrer">
              <IconFilePdf width={16} /> View the invoice (PDF)
            </a>

            {done && (
              <div role="status" className="alert alert-success mb-0">
                Thank you: {inv.businessName} has been told that you paid invoice {inv.number}{note.trim() ? ', with your note' : ''}.
              </div>
            )}
            {error && <div role="alert" className="alert alert-danger mb-0">{error}</div>}

            {inv.canMark ? (
              <div className="border-top pt-3 vstack gap-3">
                <p className="mb-0">Have you paid invoice {inv.number} ({formatMoney(inv.total, inv.currency)})? Let {inv.businessName} know.</p>
                <Field label="Note for the business (optional)" hint="e.g. when and how you paid, a payment reference, or any feedback.">
                  {(f) => <textarea id={f} className="form-control" rows={4} maxLength={1000} value={note} onChange={(e) => setNote(e.target.value)} />}
                </Field>
                <button className="btn btn-primary btn-lg" disabled={busy} onClick={markPaid}><IconCheck width={18} /> Mark as paid</button>
              </div>
            ) : !done && (
              <div className="border-top pt-3">
                {inv.stage === 'paid' ? (
                  <p className="mb-0 fw-semibold">This invoice is paid. Thank you.</p>
                ) : inv.customerPaidAt ? (
                  <p className="mb-0 fw-semibold">
                    You told {inv.businessName} on {formatDateTime(inv.customerPaidAt)} that you paid this invoice.
                    {inv.customerPaidNote && <span className="d-block fw-normal mt-1">Your note: “{inv.customerPaidNote}”</span>}
                  </p>
                ) : (
                  <p className="mb-0 fw-semibold">
                    This invoice can no longer be marked here. Please contact {inv.businessName}
                    {inv.businessEmail && <> (<a href={`mailto:${inv.businessEmail}`}>{inv.businessEmail}</a>)</>}.
                  </p>
                )}
              </div>
            )}
          </section>
        )}
      </div>
    </main>
  )
}
