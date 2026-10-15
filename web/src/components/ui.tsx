import { useEffect, useId, type ReactNode } from 'react'
import { Link } from 'react-router-dom'

export function Page({ title, back, actions, children }: { title: string; back?: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <div className="mx-auto w-full max-w-5xl px-4 pb-28 pt-4 md:pb-10">
      <header className="mb-4 flex items-center gap-2">
        {back && (
          <Link to={back} aria-label="Back" className="btn-secondary -ml-1 h-11 w-11 px-0">
            ←
          </Link>
        )}
        <h1 className="flex-1 truncate text-xl font-bold md:text-2xl">{title}</h1>
        {actions}
      </header>
      {children}
    </div>
  )
}

export function Field({ label, hint, children }: { label: string; hint?: string; children: (id: string) => ReactNode }) {
  const id = useId()
  return (
    <div>
      <label htmlFor={id} className="label">
        {label}
      </label>
      {children(id)}
      {hint && <p className="mt-1 text-xs text-slate-500">{hint}</p>}
    </div>
  )
}

export function Spinner() {
  return (
    <div role="status" className="flex justify-center p-8">
      <span className="h-8 w-8 animate-spin rounded-full border-4 border-brand-200 border-t-brand-500" />
      <span className="sr-only">Loading…</span>
    </div>
  )
}

export function ErrorBanner({ error }: { error?: Error | null }) {
  if (!error) return null
  return (
    <div role="alert" className="mb-4 rounded-xl bg-red-50 p-3 text-sm text-red-800 ring-1 ring-red-200 dark:bg-red-950 dark:text-red-200 dark:ring-red-900">
      {error.message}
    </div>
  )
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="card text-center text-sm text-slate-500">{children}</p>
}

const statusColours: Record<string, string> = {
  draft: 'bg-slate-200 text-slate-700 dark:bg-slate-700 dark:text-slate-200',
  issued: 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-100',
  sent: 'bg-indigo-100 text-indigo-800 dark:bg-indigo-900 dark:text-indigo-100',
  paid: 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-100',
  void: 'bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-200',
  overdue: 'bg-amber-100 text-amber-900 dark:bg-amber-900 dark:text-amber-100',
  pending: 'bg-amber-100 text-amber-900 dark:bg-amber-900 dark:text-amber-100',
  sending: 'bg-blue-100 text-blue-800',
  failed: 'bg-red-100 text-red-700',
  cancelled: 'bg-slate-200 text-slate-700',
  active: 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-100',
  paused: 'bg-amber-100 text-amber-900',
  ended: 'bg-slate-200 text-slate-700',
}

export function Badge({ status }: { status: string }) {
  return <span className={`chip ${statusColours[status] ?? 'bg-slate-100'}`}>{status}</span>
}

/** Sheet is a bottom sheet on phones and a centred dialog on larger screens. */
export function Sheet({ title, open, onClose, children }: { title: string; open: boolean; onClose: () => void; children: ReactNode }) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])
  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/40 md:items-center" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="max-h-[90vh] w-full overflow-y-auto rounded-t-3xl bg-white p-5 pb-[calc(env(safe-area-inset-bottom)+1.25rem)] dark:bg-slate-900 md:max-w-lg md:rounded-3xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-bold">{title}</h2>
          <button className="btn-secondary h-10 w-10 px-0" onClick={onClose} aria-label="Close">
            ✕
          </button>
        </div>
        {children}
      </div>
    </div>
  )
}

/** StickyActions keeps form buttons reachable by thumb on phones. */
export function StickyActions({ children }: { children: ReactNode }) {
  return (
    <div className="fixed inset-x-0 bottom-[calc(4rem+env(safe-area-inset-bottom))] z-30 border-t border-slate-200 bg-white/95 p-3 backdrop-blur dark:border-slate-800 dark:bg-slate-900/95 md:static md:mt-6 md:border-0 md:bg-transparent md:p-0">
      <div className="mx-auto flex max-w-5xl justify-end gap-2">{children}</div>
    </div>
  )
}

export function SearchBox({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder: string }) {
  return (
    <input
      type="search"
      className="input mb-3"
      placeholder={placeholder}
      aria-label={placeholder}
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  )
}
