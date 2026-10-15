import { useEffect, useId, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { formatDate } from '../lib/format'

export function Page({ title, back, actions, children }: { title: string; back?: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <div className="w-full px-3 pb-28 pt-4 sm:px-4 md:px-6 md:pb-10 md:pt-6">
      <header className="mb-4 flex items-center gap-2">
        {back && (
          <Link to={back} aria-label="Back" className="btn-secondary -ml-1 h-11 w-11 px-0">
            ←
          </Link>
        )}
        <h1 className="flex-1 truncate text-2xl font-bold text-brand-900 md:text-3xl">{title}</h1>
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
      <span className="h-8 w-8 animate-spin rounded-full border-4 border-brand-100 border-t-brand-600" />
      <span className="sr-only">Loading…</span>
    </div>
  )
}

export function ErrorBanner({ error }: { error?: Error | null }) {
  if (!error) return null
  return (
    <div role="alert" className="mb-4 rounded-xl border-l-4 border-red-600 bg-white p-3 text-sm font-medium text-red-800 shadow-sm">
      {error.message}
    </div>
  )
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="card text-center text-sm text-slate-500">{children}</p>
}

const statusColours: Record<string, string> = {
  draft: 'bg-slate-100 text-slate-700 ring-1 ring-slate-200',
  issued: 'bg-sky-50 text-sky-800 ring-1 ring-sky-200',
  emailed: 'bg-brand-50 text-brand-700 ring-1 ring-brand-200',
  sent: 'bg-brand-50 text-brand-700 ring-1 ring-brand-200',
  paid: 'bg-green-100 text-green-800 ring-1 ring-green-200',
  void: 'bg-red-50 text-red-700 ring-1 ring-red-200',
  overdue: 'bg-amber-50 text-amber-800 ring-1 ring-amber-200',
  pending: 'bg-amber-50 text-amber-800 ring-1 ring-amber-200',
  sending: 'bg-sky-50 text-sky-800 ring-1 ring-sky-200',
  failed: 'bg-red-50 text-red-700 ring-1 ring-red-200',
  cancelled: 'bg-slate-100 text-slate-600 ring-1 ring-slate-200',
  active: 'bg-green-100 text-green-800 ring-1 ring-green-200',
  paused: 'bg-amber-50 text-amber-800 ring-1 ring-amber-200',
  ended: 'bg-slate-100 text-slate-600 ring-1 ring-slate-200',
}

/** Human labels for invoice stages. */
export const stageLabels: Record<string, string> = {
  draft: 'Draft',
  issued: 'Issued',
  emailed: 'Issued & emailed',
  overdue: 'Overdue',
  paid: 'Paid',
  void: 'Void',
}

export function Badge({ status, label }: { status: string; label?: string }) {
  return <span className={`chip whitespace-nowrap ${statusColours[status] ?? 'bg-slate-100'}`}>{label ?? stageLabels[status] ?? status}</span>
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
        className="max-h-[90vh] w-full overflow-y-auto rounded-t-3xl bg-white p-5 pb-[calc(env(safe-area-inset-bottom)+1.25rem)] md:max-w-lg md:rounded-3xl"
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
    <div className="fixed inset-x-0 bottom-[calc(4rem+env(safe-area-inset-bottom))] z-30 border-t border-cream-300 bg-white/95 p-3 backdrop-blur md:static md:mt-6 md:border-0 md:bg-transparent md:p-0">
      <div className="flex justify-end gap-2">{children}</div>
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

/** StageTracker shows where an invoice is: Draft → Issued → Emailed → Paid (or Overdue/Void). */
export function StageTracker({ stage, issuedAt, sentAt, paidAt }: { stage: string; issuedAt?: string; sentAt?: string; paidAt?: string }) {
  const end = stage === 'void' ? 'Void' : stage === 'overdue' ? 'Overdue' : 'Paid'
  const steps = [
    { key: 'draft', label: 'Draft', done: true, at: undefined as string | undefined },
    { key: 'issued', label: 'Issued', done: stage !== 'draft', at: issuedAt },
    { key: 'emailed', label: 'Emailed', done: !!sentAt, at: sentAt },
    { key: 'final', label: end, done: stage === 'paid' || stage === 'void' || stage === 'overdue', at: paidAt },
  ]
  const tone = stage === 'void' ? 'bg-red-600' : stage === 'overdue' ? 'bg-amber-500' : 'bg-brand-600'
  return (
    <ol className="flex items-center" aria-label="Invoice stage">
      {steps.map((s, i) => (
        <li key={s.key} className="flex flex-1 items-center last:flex-none">
          <div className="flex flex-col items-center text-center">
            <span className={`flex h-7 w-7 items-center justify-center rounded-full text-xs font-bold ${s.done ? (i === 3 ? tone : 'bg-brand-600') + ' text-white' : 'bg-cream-200 text-slate-500'}`}>
              {s.done ? '✓' : i + 1}
            </span>
            <span className={`mt-1 text-xs font-semibold ${s.done ? 'text-brand-900' : 'text-slate-500'}`}>{s.label}</span>
            {s.at && <span className="text-[11px] text-slate-500">{formatDate(s.at)}</span>}
          </div>
          {i < steps.length - 1 && <span className={`mx-1 mb-5 h-0.5 flex-1 ${steps[i + 1]!.done ? 'bg-brand-600' : 'bg-cream-300'}`} />}
        </li>
      ))}
    </ol>
  )
}
