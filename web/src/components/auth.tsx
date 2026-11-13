import { createContext, useCallback, useContext, useEffect, useRef, useState, type CSSProperties, type FormEvent, type ReactNode } from 'react'
import { api, ApiError, setUnauthorizedHandler, unwrap } from '../lib/api/client'
import { useVersion } from '../lib/version'
import { Wordmark } from './Logo'
import { Spinner } from './ui'

interface Auth {
  user: { id: string; email: string; automationPaused?: boolean; mustChangePassword?: boolean; administrator?: boolean; permissions?: string[] } | null
  logout: () => Promise<void>
  /** refresh re-reads the signed-in user (e.g. after resuming automation). */
  refresh: () => Promise<void>
  /** endSession shows the sign-in screen with a notice (the server already ended the session). */
  endSession: (notice: string) => void
  /**
   * outside is set only on the default value, used by a component rendered without an
   * AuthProvider (a unit test). In the app every page is inside the provider, which shows
   * nothing but the sign-in screen until a user is signed in.
   */
  outside?: boolean
}

export const AuthContext = createContext<Auth>({ user: null, logout: async () => {}, refresh: async () => {}, endSession: () => {}, outside: true })

export const useAuth = () => useContext(AuthContext)

/**
 * useCan tells what the signed-in user may do (ADR-012): `can('invoices.send')`, any one of
 * several (`can(['settings.export', 'settings.import'])`), or `can('administer')` for users,
 * groups, backups and restore (Administrators only). The server enforces the same rules;
 * this only hides what would be refused.
 */
export function useCan() {
  const { user, outside } = useAuth()
  const one = (p: string) => !!user && (!!user.administrator || (p !== 'administer' && !!user.permissions?.includes(p)))
  return (permission: string | string[]) => !!outside || (Array.isArray(permission) ? permission.some(one) : one(permission))
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<Auth['user']>(null)
  const [checked, setChecked] = useState(false)
  const [notice, setNotice] = useState('')
  // expired: the session timed out while a page was open. The sign-in form is shown over the
  // page instead of replacing it, so what was being typed survives signing in again.
  const [expired, setExpired] = useState(false)
  const userRef = useRef<Auth['user']>(null)
  const load = useCallback(async () => {
    try {
      const me = await unwrap(api.GET('/auth/me'))
      // Someone else signed in over an open page: start clean rather than show its data.
      if (userRef.current && userRef.current.id !== me.id) window.location.reload()
      userRef.current = me
      setUser(me)
      setExpired(false)
      setNotice('')
    } catch {
      userRef.current = null
      setUser(null)
      setExpired(false)
    } finally {
      setChecked(true)
    }
  }, [])
  useEffect(() => {
    setUnauthorizedHandler(() =>
      setUser((u) => {
        if (u) {
          setNotice('Your session timed out. Sign in again to carry on where you were.')
          setExpired(true)
        }
        return u
      }),
    )
    void load()
  }, [load])
  const logout = async () => {
    // Signed out here even when the server cannot be reached (its session then simply
    // expires); the screen must not stay as it was.
    await api.POST('/auth/logout').catch(() => undefined)
    setNotice('You signed out.')
    setExpired(false)
    userRef.current = null
    setUser(null)
  }
  const endSession = (n: string) => {
    setNotice(n)
    setExpired(false)
    userRef.current = null
    setUser(null)
  }
  if (!checked) return <Spinner />
  if (!user) return <Login onLogin={load} notice={notice} />
  // The server allows nothing else until the password is changed (ADR-011).
  if (user.mustChangePassword) return <ForcedPasswordChange login={user.email} onChanged={load} onSignOut={logout} />
  return (
    <AuthContext.Provider value={{ user, logout, refresh: load, endSession }}>
      {children}
      {expired && (
        <div className="position-fixed top-0 start-0 w-100 h-100 overflow-y-auto" style={{ zIndex: 1060 }} role="dialog" aria-modal="true" aria-label="Sign in again">
          <Login onLogin={load} notice={notice} />
        </div>
      )}
    </AuthContext.Provider>
  )
}

/** The public source repository. */
const REPO_URL = 'https://github.com/albievan/cactusinv'

function IconGitHub() {
  return <i className="bi bi-github" style={{ fontSize: '14px' }} aria-hidden="true" />
}

function WaveLines() {
  const lines = Array.from({ length: 14 }, (_, i) => i)
  return (
    <svg className="pe-none position-absolute bottom-0 start-0 w-100" style={{ height: '33.333%' }} viewBox="0 0 1200 300" preserveAspectRatio="none" aria-hidden="true">
      {lines.map((i) => (
        <path
          key={i}
          d={`M0 ${120 + i * 12} C 300 ${40 + i * 14}, 600 ${220 + i * 6}, 900 ${110 + i * 10} S 1200 ${150 + i * 9}, 1200 ${150 + i * 9}`}
          fill="none"
          stroke="#d9c89f"
          strokeOpacity={0.08 + i * 0.012}
          strokeWidth="1"
        />
      ))}
    </svg>
  )
}

/** Notice is the sign-in screens' message box: a white card with a coloured edge. */
function Notice({ error, children }: { error: boolean; children: ReactNode }) {
  return (
    <div
      role="alert"
      className={`rounded-3 border-start border-4 bg-white p-3 small fw-semibold shadow-sm ${error ? 'border-danger text-danger-emphasis' : 'text-primary-emphasis'}`}
      style={error ? undefined : ({ '--bs-border-color': '#c7b48a' } as CSSProperties)}
    >
      {children}
    </div>
  )
}

/**
 * PasswordInput is a password box with an eye button at its end that shows or hides what
 * is typed (show is shared, so a second box on the form follows it).
 */
function PasswordInput({ id, autoComplete, placeholder, minLength, value, onChange, show, onShow }: {
  id: string
  autoComplete: string
  placeholder?: string
  minLength?: number
  value: string
  onChange: (v: string) => void
  show: boolean
  onShow: (v: boolean) => void
}) {
  return (
    <div className="input-group">
      <input id={id} className="form-control" type={show ? 'text' : 'password'} autoComplete={autoComplete} placeholder={placeholder} minLength={minLength} maxLength={autoComplete === 'new-password' ? 200 : undefined}
        value={value} onChange={(e) => onChange(e.target.value)} required />
      <button type="button" className="btn password-toggle" aria-label={show ? 'Hide password' : 'Show password'} aria-pressed={show} title={show ? 'Hide password' : 'Show password'} aria-controls={id} onClick={() => onShow(!show)}>
        <i className={`bi ${show ? 'bi-eye-slash' : 'bi-eye'}`} style={{ fontSize: '18px' }} aria-hidden="true" />
      </button>
    </div>
  )
}

function Login({ onLogin, notice }: { onLogin: () => void; notice: string }) {
  const version = useVersion()
  const [user, setUser] = useState('')
  const [password, setPassword] = useState('')
  const [show, setShow] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    let res
    try {
      res = await api.POST('/auth/login', { body: { email: user, password } })
    } catch {
      setError('Could not reach the server. Check the connection and try again.')
      return
    } finally {
      setBusy(false)
    }
    const { response, error } = res
    if (!response.ok) {
      setError(error ? new ApiError(error).message : 'Sign-in failed')
      return
    }
    onLogin()
  }
  return (
    <main className="d-flex min-vh-100 flex-column flex-lg-row">
      <section
        className="position-relative overflow-hidden px-4 py-5 px-sm-5 py-lg-0 flex-lg-grow-1 text-cream"
        style={{ background: 'linear-gradient(to bottom right, #244a38, #1b382b, #0d1d16)' }}
      >
        <div className="position-relative z-1 mt-lg-5 pt-lg-5" style={{ maxWidth: '36rem' }}>
          <div className="mb-5 d-lg-none">
            <Wordmark dark />
          </div>
          <h1 className="h1 fw-bold lh-sm text-white mb-0">Invoicing that runs itself</h1>
          <p className="mt-4 mb-0 fs-6 lh-lg text-cream">
            cactusInv issues your invoices, e-mails them on schedule and keeps every PDF on file. Customers are billed in
            their own currency and paid into the matching bank account, and statements show at a glance who still owes what.
          </p>
          <ul className="list-unstyled d-flex flex-wrap gap-2 mt-4 mb-0 small">
            {['Scheduled invoices & e-mails', 'Multi-currency with bank per currency', 'Statements & aging', 'Every PDF archived'].map((t) => (
              <li key={t} className="rounded-pill border border-light border-opacity-25 px-3 py-1 text-cream">
                {t}
              </li>
            ))}
          </ul>
          <p className="mt-4 mb-0 small text-cream">
            Free and open source:{' '}
            <a
              href={REPO_URL}
              target="_blank"
              rel="noreferrer"
              className="d-inline-flex align-items-center gap-1 fw-semibold link-light text-decoration-underline link-offset-2 link-underline-opacity-50 link-underline-opacity-100-hover"
            >
              <IconGitHub /> github.com/albievan/cactusinv
            </a>
          </p>
        </div>
        <WaveLines />
      </section>

      <section className="d-flex flex-column col-lg-5 col-xl-4 px-4 py-5 px-sm-5 bg-paper">
        <div className="d-none d-lg-block">
          <Wordmark />
        </div>
        <form onSubmit={submit} className="vstack gap-3 w-100 mt-2 mt-lg-5">
          <h2 className="h4 fw-bold mb-0">Sign in</h2>
          {(notice || error) && <Notice error={!!error}>{error || notice}</Notice>}
          <div>
            <label className="form-label" htmlFor="login">
              User name
            </label>
            <input id="login" className="form-control" autoComplete="username" autoCapitalize="none" placeholder="Enter your user name" value={user} onChange={(e) => setUser(e.target.value)} required />
          </div>
          <div>
            <label className="form-label" htmlFor="password">
              Password
            </label>
            <PasswordInput id="password" autoComplete="current-password" placeholder="Enter your password" value={password} onChange={setPassword} show={show} onShow={setShow} />
          </div>
          <button className="btn btn-primary w-100 py-3 fs-6" disabled={busy}>
            {busy ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
        <div className="mt-auto d-flex align-items-center justify-content-between gap-3 border-top pt-3 fs-8 text-body-secondary">
          <span>{version || ' '}</span>
          <a href={REPO_URL} target="_blank" rel="noreferrer" className="d-inline-flex align-items-center gap-1 fw-medium">
            <IconGitHub /> Source on GitHub
          </a>
        </div>
      </section>
    </main>
  )
}

/**
 * ForcedPasswordChange is all a user sees while their password was chosen by someone else
 * (a new user, a reset, or the default administrator): the server refuses everything but
 * this change and signing out.
 */
function ForcedPasswordChange({ login, onChanged, onSignOut }: { login: string; onChanged: () => Promise<void>; onSignOut: () => Promise<void> }) {
  const version = useVersion()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [again, setAgain] = useState('')
  const [show, setShow] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    if ([...next].length < 12) return setError('The new password needs at least 12 characters.')
    if (next !== again) return setError('The two new passwords are not the same.')
    if (next === current) return setError('Choose a password different from the one you signed in with.')
    setBusy(true)
    try {
      await unwrap(api.POST('/auth/password', { body: { currentPassword: current, newPassword: next } }))
      await onChanged()
    } catch (x) {
      setError(x instanceof ApiError ? (x.problem.field === 'currentPassword' ? 'The current password is not correct.' : x.message) : 'The password could not be changed.')
      setBusy(false)
    }
  }
  return (
    <main
      className="d-flex min-vh-100 flex-column px-4 pt-5 px-sm-5 align-items-sm-center justify-content-sm-center bg-paper"
      style={{ paddingBottom: 'calc(env(safe-area-inset-bottom) + 1.5rem)' }}
    >
      <div className="w-100" style={{ maxWidth: '28rem' }}>
        <Wordmark />
        <form onSubmit={submit} className="vstack gap-3 mt-5" aria-labelledby="forced-heading">
          <h1 id="forced-heading" className="h4 fw-bold mb-0">
            Choose a new password
          </h1>
          <p className="small text-body mb-0">
            You are signed in as <strong className="text-break">{login}</strong> with a password someone else chose. Choose your own to continue —
            nothing else can be done until then.
          </p>
          {error && <Notice error>{error}</Notice>}
          <div>
            <label className="form-label" htmlFor="forced-current">
              Current password
            </label>
            <input id="forced-current" className="form-control" type={show ? 'text' : 'password'} autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
            <div className="form-text">The password you just signed in with.</div>
          </div>
          <div>
            <label className="form-label" htmlFor="forced-new">
              New password
            </label>
            <PasswordInput id="forced-new" autoComplete="new-password" minLength={12} value={next} onChange={setNext} show={show} onShow={setShow} />
            <div className="form-text">At least 12 characters. A few unrelated words work well.</div>
          </div>
          <div>
            <label className="form-label" htmlFor="forced-again">
              New password again
            </label>
            <input id="forced-again" className="form-control" type={show ? 'text' : 'password'} autoComplete="new-password" maxLength={200} value={again} onChange={(e) => setAgain(e.target.value)} required />
          </div>
          <button className="btn btn-primary w-100 py-3 fs-6" disabled={busy}>
            {busy ? 'Saving…' : 'Save and continue'}
          </button>
          <button type="button" className="btn btn-outline-primary w-100 py-3 fs-6" onClick={() => void onSignOut()}>
            Sign out
          </button>
        </form>
        <div className="mt-5 border-top pt-3 fs-8 text-body-secondary">{version || ' '}</div>
      </div>
    </main>
  )
}
