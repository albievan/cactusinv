import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { ApiError, api, unwrap, type Schemas } from '../../lib/api/client'
import { formatDate } from '../../lib/format'
import { formatMoney } from '../../lib/money'
import { Badge, Spinner } from '../../components/ui'
import { IconFilePdf } from '../../components/icons'
import { CactusMark } from '../../components/Logo'

type Portal = Schemas['Portal']
type PortalInvoice = Schemas['PortalInvoice']

const tnum = { fontVariantNumeric: 'tabular-nums' } as const

/**
 * AccountPage is /a/:token, the customer's account page a statement e-mail links to (M59).
 * No sign-in: the token is the key (90 days). It shows what is owed, invoice by invoice,
 * the latest paid invoices, and each invoice and the statement as a PDF. Opening it
 * changes nothing.
 */
export function AccountPage() {
  const { token = '' } = useParams()
  const [acct, setAcct] = useState<Portal>()
  const [missing, setMissing] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    unwrap(api.GET('/public/accounts/{token}', { params: { path: { token } } }))
      .then(setAcct)
      .catch((e) => (e instanceof ApiError && e.problem.status === 404 ? setMissing(true) : setError('Your account could not be loaded. Please try again later.')))
  }, [token])

  const pdf = (id: string) => `/api/v1/public/accounts/${token}/invoices/${encodeURIComponent(id)}/pdf`
  return (
    <main className="min-vh-100 bg-paper px-3 py-4 py-sm-5">
      <div className="mx-auto" style={{ maxWidth: '48rem' }}>
        <div className="d-flex align-items-center gap-2 mb-4">
          <CactusMark size={32} />
          <span className="fw-semibold text-body-secondary small">{acct?.businessName || 'Your account'}</span>
        </div>
        {missing ? (
          <section className="card card-body">
            <h1 className="h4 mb-2">This link does not work</h1>
            <p className="mb-0 text-body-secondary">Account links work for 90 days after the statement was sent, and only when copied completely. Please ask the business that sent it for a new statement.</p>
          </section>
        ) : !acct ? (
          error ? <div role="alert" className="alert alert-danger">{error}</div> : <Spinner />
        ) : (
          <div className="vstack gap-3">
            <section className="card card-body">
              <div className="d-flex flex-wrap align-items-start justify-content-between gap-3">
                <div>
                  <h1 className="h4 mb-1">{acct.customerName}</h1>
                  <div className="small text-body-secondary">Your account with {acct.businessName} on {formatDate(acct.asOf)}</div>
                </div>
                <div className="text-sm-end">
                  <div className="small text-body-secondary">Still owed</div>
                  <div className="fs-3 fw-bold" style={tnum}>{formatMoney(acct.balance, acct.currency)}</div>
                </div>
              </div>
              <a className="btn btn-outline-primary align-self-start mt-3" href={`/api/v1/public/accounts/${token}/statement`} target="_blank" rel="noreferrer">
                <IconFilePdf width={16} /> Statement (PDF)
              </a>
            </section>

            <InvoiceTable title="Unpaid invoices" empty="Nothing is owed. Thank you!" items={acct.open} currency={acct.currency} pdf={pdf} owed />
            {acct.paid.length > 0 && <InvoiceTable title="Recently paid" empty="" items={acct.paid} currency={acct.currency} pdf={pdf} />}

            <p className="small text-body-secondary mb-0">
              Questions about your account? Contact {acct.businessName}
              {acct.businessEmail && <> at <a href={`mailto:${acct.businessEmail}`}>{acct.businessEmail}</a></>}.
            </p>
          </div>
        )}
      </div>
    </main>
  )
}

function InvoiceTable({ title, empty, items, currency, pdf, owed }: {
  title: string; empty: string; items: PortalInvoice[]; currency: string; pdf: (id: string) => string; owed?: boolean
}) {
  const heading = title.replace(/\s+/g, '-').toLowerCase()
  return (
    <section className="card overflow-hidden" aria-labelledby={heading}>
      <h2 id={heading} className="fs-6 fw-semibold p-3 pb-2 mb-0">{title}</h2>
      {items.length === 0 ? <p className="small text-body-secondary px-3 pb-3 mb-0">{empty}</p> : (
        <ul className="list-group list-group-flush">
          {items.map((i) => (
            <li key={i.id} className="list-group-item d-flex flex-wrap align-items-center gap-3 px-3 py-3 small">
              <div className="flex-grow-1 min-w-0">
                <div className="d-flex flex-wrap align-items-center gap-2">
                  <span className="fw-semibold">{i.number}</span>
                  <Badge status={i.stage} />
                </div>
                <div className="text-body-secondary">
                  {formatDate(i.issueDate)} · due {formatDate(i.dueDate)}{i.daysOverdue > 0 ? ` · ${i.daysOverdue} days overdue` : ''}
                </div>
              </div>
              <div className="text-end" style={tnum}>
                <div className="fw-semibold">{formatMoney(owed ? i.balance : i.total, currency)}</div>
                {owed && i.balance !== i.total && <div className="fs-8 text-body-secondary">of {formatMoney(i.total, currency)}</div>}
              </div>
              <a className="btn btn-sm btn-outline-primary" href={pdf(i.id)} target="_blank" rel="noreferrer" aria-label={`Invoice ${i.number} (PDF)`}>
                <IconFilePdf width={15} /> PDF
              </a>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
