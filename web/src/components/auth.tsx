import { createContext, useCallback, useContext, useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { api, ApiError, setUnauthorizedHandler, unwrap } from '../lib/api/client'
import { useVersion } from '../lib/version'
import { Wordmark } from './Logo'
import { Spinner } from './ui'

interface Auth {
  user: { id: string; email: string } | null
  logout: () => Promise<void>
}

const AuthContext = createContext<Auth>({ user: null, logout: async () => {} })

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
  if (!checked) return <Spinner />
  if (!user) return <Login onLogin={load} notice={notice} />
  return <AuthContext.Provider value={{ user, logout }}>{children}</AuthContext.Provider>
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
