import { useEffect, useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { ApiError, api, unwrap, type Schemas } from '../../lib/api/client'
import { formatDate } from '../../lib/format'
import { formatMoney, groupAmount } from '../../lib/money'
import { Field, Spinner } from '../../components/ui'
import { IconBan, IconCheck, IconFilePdf } from '../../components/icons'
import { CactusMark } from '../../components/Logo'

type QuoteResponse = Schemas['QuoteResponse']

const tnum = { fontVariantNumeric: 'tabular-nums' } as const

/**
 * QuoteResponsePage is /q/:token, the page the Accept and Decline buttons in a quote e-mail
 * open (M41). No sign-in: the token is the key. Opening it changes nothing; the customer
 * confirms with one click (mail scanners open links on their own).
 */
export function QuoteResponsePage() {
  const { token = '' } = useParams()
  const [params] = useSearchParams()
  const wanted = params.get('answer') === 'decline' ? 'decline' : params.get('answer') === 'accept' ? 'accept' : null
  const [answer, setAnswer] = useState<'accept' | 'decline' | null>(wanted)
  const [q, setQ] = useState<QuoteResponse>()
  const [missing, setMissing] = useState(false)
  const [error, setError] = useState('')
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState('')

  useEffect(() => {
    unwrap(api.GET('/public/quotes/{token}', { params: { path: { token } } }))
      .then(setQ)
      .catch((e) => (e instanceof ApiError && e.problem.status === 404 ? setMissing(true) : setError('The quotation could not be loaded. Please try again later.')))
  }, [token])

  const respond = async (a: 'accept' | 'decline') => {
    setBusy(true)
    setError('')
    try {
      const out = await unwrap(api.POST('/public/quotes/{token}/response', { params: { path: { token } }, body: { answer: a, reason: a === 'decline' ? reason : undefined } }))
      setQ(out)
      setDone(a === 'accept' ? 'Thank you: you accepted this quotation.' : 'Thank you: you declined this quotation.')
    } catch (e) {
      setError(e instanceof ApiError ? e.problem.detail || e.problem.title : 'Your answer could not be sent. Please try again.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="min-vh-100 bg-paper px-3 py-4 py-sm-5">
      <div className="mx-auto" style={{ maxWidth: '40rem' }}>
        <div className="d-flex align-items-center gap-2 mb-4">
          <CactusMark size={32} />
          <span className="fw-semibold text-body-secondary small">{q?.businessName || 'Quotation'}</span>
        </div>
        {missing ? (
          <section className="card card-body">
            <h1 className="h4 mb-2">This link does not work</h1>
            <p className="mb-0 text-body-secondary">The quotation may have been withdrawn, or the link was copied incompletely. Please contact the business that sent it.</p>
          </section>
        ) : !q ? (
          error ? <div role="alert" className="alert alert-danger">{error}</div> : <Spinner />
        ) : (
          <section className="card card-body vstack gap-3">
            <div className="d-flex flex-wrap align-items-start justify-content-between gap-2">
              <div>
                <h1 className="h4 mb-1">Quotation {q.number}</h1>
                <div className="small text-body-secondary">from {q.businessName} for {q.billToName}</div>
              </div>
              <div className="text-end">
                <div className="fs-4 fw-bold" style={tnum}>{formatMoney(q.total, q.currency)}</div>
                <div className="small text-body-secondary">Valid until {formatDate(q.validUntil)}</div>
              </div>
            </div>
            <ul className="list-group">
              {q.lines.map((l, i) => (
                <li key={i} className="list-group-item d-flex justify-content-between gap-3 small">
                  <span className="min-w-0">{l.quantity} × {l.description}</span>
                  <span style={tnum}>{groupAmount(l.amount)}</span>
                </li>
              ))}
            </ul>
            <a className="btn btn-outline-primary align-self-start" href={`/api/v1/public/quotes/${token}/pdf`} target="_blank" rel="noreferrer">
              <IconFilePdf width={16} /> View the quotation (PDF)
            </a>

            {done && <div role="status" className="alert alert-success mb-0">{done} {q.businessName} has been told.</div>}
            {error && <div role="alert" className="alert alert-danger mb-0">{error}</div>}

            {q.canAnswer ? (
              <div className="border-top pt-3 vstack gap-3">
                {answer !== 'decline' && (
                  <div className="vstack gap-2">
                    <p className="mb-0">Accept quotation {q.number} for {formatMoney(q.total, q.currency)}?</p>
                    <button className="btn btn-primary btn-lg" disabled={busy} onClick={() => respond('accept')}><IconCheck width={18} /> Accept quote</button>
                  </div>
                )}
                {answer === 'decline' && (
                  <div className="vstack gap-2">
                    <p className="mb-0">Decline quotation {q.number}?</p>
                    <Field label="Reason (optional)">{(f) => <textarea id={f} className="form-control" maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />}</Field>
                    <button className="btn btn-danger btn-lg" disabled={busy} onClick={() => respond('decline')}><IconBan width={18} /> Decline quote</button>
                  </div>
                )}
                <button type="button" className="btn btn-link p-0 align-self-start small" onClick={() => setAnswer(answer === 'decline' ? 'accept' : 'decline')}>
                  {answer === 'decline' ? 'Accept it instead' : 'Decline it instead'}
                </button>
              </div>
            ) : !done && (
              <div className="border-top pt-3">
                {q.expired ? (
                  <p className="mb-0 fw-semibold text-warning-emphasis">
                    This quotation expired on {formatDate(q.validUntil)} and can no longer be accepted. Please ask {q.businessName}
                    {q.businessEmail && <> (<a href={`mailto:${q.businessEmail}`}>{q.businessEmail}</a>)</>} for a new one.
                  </p>
                ) : (
                  <p className="mb-0 fw-semibold">
                    This quotation was {q.stage === 'converted' ? 'accepted and invoiced' : q.stage}{q.answeredBy === 'customer' ? ' by you' : ''}.
                    {q.stage === 'declined' && q.declineReason && <span className="fw-normal"> Reason: {q.declineReason}</span>}
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
