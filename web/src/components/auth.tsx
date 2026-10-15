import { createContext, useCallback, useContext, useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { api, ApiError, setUnauthorizedHandler, unwrap } from '../lib/api/client'
import { ErrorBanner, Spinner } from './ui'

interface Auth {
  user: { id: string; email: string } | null
  logout: () => Promise<void>
}

const AuthContext = createContext<Auth>({ user: null, logout: async () => {} })

export const useAuth = () => useContext(AuthContext)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<Auth['user']>(null)
  const [checked, setChecked] = useState(false)
  const load = useCallback(async () => {
    try {
      setUser(await unwrap(api.GET('/auth/me')))
    } catch {
      setUser(null)
    } finally {
      setChecked(true)
    }
  }, [])
  useEffect(() => {
    setUnauthorizedHandler(() => setUser(null))
    void load()
  }, [load])
  const logout = async () => {
    await api.POST('/auth/logout')
    setUser(null)
  }
  if (!checked) return <Spinner />
  if (!user) return <Login onLogin={load} />
  return <AuthContext.Provider value={{ user, logout }}>{children}</AuthContext.Provider>
}

function Login({ onLogin }: { onLogin: () => void }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    const { response, error } = await api.POST('/auth/login', { body: { email, password } })
    setBusy(false)
    if (!response.ok) {
      setError(error ? new ApiError(error) : new Error('Sign-in failed'))
      return
    }
    onLogin()
  }
  return (
    <main className="flex min-h-screen items-center justify-center p-4">
      <form onSubmit={submit} className="card w-full max-w-sm space-y-4">
        <div className="text-center">
          <img src="/favicon.svg" alt="" className="mx-auto mb-2 h-12 w-12" />
          <h1 className="text-2xl font-bold">cactusInv</h1>
          <p className="text-sm text-slate-500">Sign in to manage invoices</p>
        </div>
        <ErrorBanner error={error} />
        <div>
          <label className="label" htmlFor="login">
            User
          </label>
          <input id="login" className="input" autoComplete="username" autoCapitalize="none" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </div>
        <div>
          <label className="label" htmlFor="password">
            Password
          </label>
          <input id="password" className="input" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        </div>
        <button className="btn-primary w-full" disabled={busy}>
          {busy ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
    </main>
  )
}
