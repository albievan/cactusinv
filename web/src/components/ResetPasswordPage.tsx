import { useEffect, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ApiError, api, unwrap } from '../lib/api/client'
import { Wordmark } from './Logo'
import { PasswordInput, PasswordPolicy, passwordProblem } from './password'
import { Spinner } from './ui'

/**
 * ResetPasswordPage is /reset-password/:token, the page a password reset e-mail links to
 * (M43). No sign-in: the link is the key. It works once, for an hour.
 */
export function ResetPasswordPage() {
  const { token = '' } = useParams()
  const [state, setState] = useState<'checking' | 'ok' | 'bad' | 'done'>('checking')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [show, setShow] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    unwrap(api.GET('/auth/password-reset/{token}', { params: { path: { token } } }))
      .then(() => setState('ok'))
      .catch(() => setState('bad'))
  }, [token])

  const problem = passwordProblem(next, confirm)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (problem) return setError(problem)
    setBusy(true)
    setError('')
    try {
      await unwrap(api.POST('/auth/password-reset/{token}', { params: { path: { token } }, body: { newPassword: next } }))
      setState('done')
    } catch (x) {
      if (x instanceof ApiError && x.problem.field === 'token') setState('bad')
      else setError(x instanceof ApiError && x.problem.status === 429 ? 'Too many attempts. Wait a few minutes, then try again.' : x instanceof Error ? x.message : 'The password could not be changed.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="d-flex min-vh-100 flex-column px-4 pt-5 px-sm-5 align-items-sm-center justify-content-sm-center bg-paper" style={{ paddingBottom: 'calc(env(safe-area-inset-bottom) + 1.5rem)' }}>
      <div className="w-100" style={{ maxWidth: '28rem' }}>
        <Wordmark />
        <div className="vstack gap-3 mt-5">
          <h1 className="h4 fw-bold mb-0">Choose a new password</h1>
          {state === 'checking' && <Spinner />}
          {state === 'bad' && (
            <>
              <div role="alert" className="alert alert-warning mb-0">
                This link does not work any more: it was used, it is older than an hour, or a newer one was sent. Ask for a new one on the sign-in page.
              </div>
              <Link to="/" className="btn btn-outline-primary w-100 py-3 fs-6">Go to sign in</Link>
            </>
          )}
          {state === 'done' && (
            <>
              <div role="status" className="alert alert-success mb-0">Your password was changed, and you were signed out everywhere. Sign in with the new password.</div>
              <Link to="/" className="btn btn-primary w-100 py-3 fs-6">Sign in</Link>
            </>
          )}
          {state === 'ok' && (
            <form onSubmit={submit} className="vstack gap-3">
              {error && <div role="alert" className="alert alert-danger mb-0">{error}</div>}
              <div>
                <label className="form-label" htmlFor="reset-new">New password</label>
                <PasswordInput id="reset-new" autoComplete="new-password" value={next} onChange={setNext} show={show} onShow={setShow} describedBy="reset-policy" />
              </div>
              <div>
                <label className="form-label" htmlFor="reset-confirm">Confirm new password</label>
                <PasswordInput id="reset-confirm" autoComplete="new-password" value={confirm} onChange={setConfirm} show={show} onShow={setShow} />
              </div>
              <PasswordPolicy id="reset-policy" next={next} confirm={confirm} />
              <button className="btn btn-primary w-100 py-3 fs-6" disabled={busy || !!problem}>{busy ? 'Saving…' : 'Save new password'}</button>
            </form>
          )}
        </div>
      </div>
    </main>
  )
}
