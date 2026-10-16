import { createContext, useCallback, useContext, useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { api, ApiError, setUnauthorizedHandler, unwrap } from '../lib/api/client'
import { useVersion } from '../lib/version'
import { Wordmark } from './Logo'
import { Spinner } from './ui'

interface Auth {
  user: { id: string; email: string; automationPaused?: boolean; mustChangePassword?: boolean } | null
  logout: () => Promise<void>
  /** refresh re-reads the signed-in user (e.g. after resuming automation). */
  refresh: () => Promise<void>
  /** endSession shows the sign-in screen with a notice (the server already ended the session). */
  endSession: (notice: string) => void
}

const AuthContext = createContext<Auth>({ user: null, logout: async () => {}, refresh: async () => {}, endSession: () => {} })

export const useAuth = () => useContext(AuthContext)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<Auth['user']>(null)
  const [checked, setChecked] = useState(false)
  const [notice, setNotice] = useState('')
  const load = useCallback(async () => {
    try {
      setUser(await unwrap(api.GET('/auth/me')))
      setNotice('')
    } catch {
      setUser(null)
    } finally {
      setChecked(true)
    }
  }, [])
  useEffect(() => {
    setUnauthorizedHandler(() =>
      setUser((u) => {
        if (u) setNotice('You were signed out: your session timed out.')
        return null
      }),
    )
    void load()
  }, [load])
  const logout = async () => {
    await api.POST('/auth/logout')
    setNotice('You signed out.')
    setUser(null)
  }
  const endSession = (n: string) => {
    setNotice(n)
    setUser(null)
  }
  if (!checked) return <Spinner />
  if (!user) return <Login onLogin={load} notice={notice} />
  // The server allows nothing else until the password is changed (ADR-011).
  if (user.mustChangePassword) return <ForcedPasswordChange login={user.email} onChanged={load} onSignOut={logout} />
  return <AuthContext.Provider value={{ user, logout, refresh: load, endSession }}>{children}</AuthContext.Provider>
}

function WaveLines() {
  const lines = Array.from({ length: 14 }, (_, i) => i)
  return (
    <svg className="pointer-events-none absolute inset-x-0 bottom-0 h-1/3 w-full" viewBox="0 0 1200 300" preserveAspectRatio="none" aria-hidden="true">
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
    const { response, error } = await api.POST('/auth/login', { body: { email: user, password } })
    setBusy(false)
    if (!response.ok) {
      setError(error ? new ApiError(error).problem.title : 'Sign-in failed')
      return
    }
    onLogin()
  }
  return (
    <main className="flex min-h-screen flex-col lg:flex-row">
      <section className="relative overflow-hidden bg-gradient-to-br from-brand-700 via-brand-800 to-brand-950 px-6 py-10 text-cream-100 sm:px-12 lg:flex-1 lg:py-0">
        <div className="relative z-10 max-w-xl lg:mt-[18vh]">
          <div className="mb-8 lg:hidden">
            <Wordmark dark />
          </div>
          <h1 className="text-3xl font-bold leading-tight text-cream-50 sm:text-4xl">Invoicing that runs itself</h1>
          <p className="mt-5 text-base leading-relaxed text-cream-200/90">
            cactusInv issues your invoices, e-mails them on schedule and keeps every PDF on file. Customers are billed in
            their own currency and paid into the matching bank account, and statements show at a glance who still owes what.
          </p>
          <ul className="mt-6 flex flex-wrap gap-2 text-sm">
            {['Scheduled invoices & e-mails', 'Multi-currency with bank per currency', 'Statements & aging', 'Every PDF archived'].map((t) => (
              <li key={t} className="rounded-full border border-cream-300/40 px-3 py-1 text-cream-100/90">
                {t}
              </li>
            ))}
          </ul>
        </div>
        <WaveLines />
      </section>

      <section className="flex w-full flex-col bg-[#f3ece0] px-6 py-10 sm:px-10 lg:w-[30rem] lg:py-14">
        <div className="hidden lg:block">
          <Wordmark />
        </div>
        <form onSubmit={submit} className="mt-2 w-full space-y-4 lg:mt-14">
          <h2 className="text-2xl font-bold text-brand-900">Sign in</h2>
          {(notice || error) && (
            <div role="alert" className={`rounded-xl border-l-4 bg-white p-4 text-sm font-semibold shadow-sm ${error ? 'border-red-600 text-red-800' : 'border-[#c7b48a] text-brand-900'}`}>
              {error || notice}
            </div>
          )}
          <div>
            <label className="label" htmlFor="login">
              User name
            </label>
            <input id="login" className="input" autoComplete="username" autoCapitalize="none" value={user} onChange={(e) => setUser(e.target.value)} required />
          </div>
          <div>
            <label className="label" htmlFor="password">
              Password
            </label>
            <input id="password" className="input" type={show ? 'text' : 'password'} autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          </div>
          <label className="flex items-center gap-2 text-sm text-slate-600">
            <input type="checkbox" checked={show} onChange={(e) => setShow(e.target.checked)} /> Show what I type
          </label>
          <button className="btn-primary w-full py-3 text-base" disabled={busy}>
            {busy ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
        <div className="mt-auto border-t border-cream-300 pt-4 text-xs text-slate-500">{version || ' '}</div>
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
    <main className="flex min-h-screen flex-col bg-[#f3ece0] px-5 pb-[calc(env(safe-area-inset-bottom)+1.5rem)] pt-8 sm:items-center sm:justify-center sm:px-10">
      <div className="w-full sm:max-w-md">
        <Wordmark />
        <form onSubmit={submit} className="mt-8 space-y-4" aria-labelledby="forced-heading">
          <h1 id="forced-heading" className="text-2xl font-bold text-brand-900">
            Choose a new password
          </h1>
          <p className="text-sm text-slate-700">
            You are signed in as <strong className="break-all">{login}</strong> with a password someone else chose. Choose your own to continue —
            nothing else can be done until then.
          </p>
          {error && (
            <div role="alert" className="rounded-xl border-l-4 border-red-600 bg-white p-4 text-sm font-semibold text-red-800 shadow-sm">
              {error}
            </div>
          )}
          <div>
            <label className="label" htmlFor="forced-current">
              Current password
            </label>
            <input id="forced-current" className="input" type={show ? 'text' : 'password'} autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
            <p className="mt-1 text-xs text-slate-500">The password you just signed in with.</p>
          </div>
          <div>
            <label className="label" htmlFor="forced-new">
              New password
            </label>
            <input id="forced-new" className="input" type={show ? 'text' : 'password'} autoComplete="new-password" minLength={12} maxLength={200} value={next} onChange={(e) => setNext(e.target.value)} required />
            <p className="mt-1 text-xs text-slate-500">At least 12 characters. A few unrelated words work well.</p>
          </div>
          <div>
            <label className="label" htmlFor="forced-again">
              New password again
            </label>
            <input id="forced-again" className="input" type={show ? 'text' : 'password'} autoComplete="new-password" maxLength={200} value={again} onChange={(e) => setAgain(e.target.value)} required />
          </div>
          <label className="flex min-h-[44px] items-center gap-2 text-sm text-slate-600">
            <input type="checkbox" checked={show} onChange={(e) => setShow(e.target.checked)} /> Show what I type
          </label>
          <button className="btn-primary w-full py-3 text-base" disabled={busy}>
            {busy ? 'Saving…' : 'Save and continue'}
          </button>
          <button type="button" className="btn-secondary w-full py-3 text-base" onClick={() => void onSignOut()}>
            Sign out
          </button>
        </form>
        <div className="mt-8 border-t border-cream-300 pt-4 text-xs text-slate-500">{version || '\u00a0'}</div>
      </div>
    </main>
  )
}
