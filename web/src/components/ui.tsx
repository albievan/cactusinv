import { useEffect, useId, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { formatDate } from '../lib/format'
import { IconBack, IconClose } from './icons'

export function Page({ title, back, actions, children }: { title: string; back?: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <div className="app-page w-100">
      <header className="d-flex align-items-center gap-2 mb-3">
        {back && (
          <Link to={back} aria-label="Back" title="Back" className="btn btn-outline-primary btn-icon btn-lg">
            <IconBack />
          </Link>
        )}
        <h1 className="h3 mb-0 flex-grow-1 text-truncate">{title}</h1>
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
      <label htmlFor={id} className="form-label">
        {label}
      </label>
      {children(id)}
      {hint && <div className="form-text">{hint}</div>}
    </div>
  )
}

export function Spinner() {
  return (
    <div role="status" className="d-flex justify-content-center p-5">
      <span className="spinner-border text-primary" aria-hidden="true" />
      <span className="visually-hidden">Loading…</span>
    </div>
  )
}

export function ErrorBanner({ error }: { error?: Error | null }) {
  if (!error) return null
  return (
    <div role="alert" className="alert alert-danger mb-3 small fw-medium">
      {error.message}
    </div>
  )
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="card card-body text-center small text-body-secondary">{children}</p>
}

// Bootstrap 5.3 subtle colours: tinted background, darker text, a matching border.
const tone = (c: string) => `bg-${c}-subtle text-${c}-emphasis border border-${c}-subtle`
const statusColours: Record<string, string> = {
  draft: tone('secondary'),
  issued: tone('info'),
  emailed: tone('primary'),
  sent: tone('primary'),
  paid: tone('success'),
  void: tone('danger'),
  overdue: tone('warning'),
  pending: tone('warning'),
  sending: tone('info'),
  failed: tone('danger'),
  cancelled: tone('secondary'),
  active: tone('success'),
  paused: tone('warning'),
  ended: tone('secondary'),
  accepted: tone('success'),
  declined: tone('danger'),
  converted: tone('success'),
  expired: tone('warning'),
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

/** Human labels for quote stages (a quote's "sent" is not an e-mail's). */
export const quoteStageLabels: Record<string, string> = {
  draft: 'Draft',
  sent: 'Sent',
  accepted: 'Accepted',
  declined: 'Declined',
  converted: 'Invoiced',
  expired: 'Expired',
}

export function Badge({ status, label }: { status: string; label?: string }) {
  return <span className={`badge rounded-pill text-nowrap ${statusColours[status] ?? tone('secondary')}`}>{label ?? stageLabels[status] ?? status}</span>
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
    <div className="modal sheet" onClick={onClose}>
      <div className="modal-dialog">
        <div role="dialog" aria-modal="true" aria-label={title} className="modal-content" onClick={(e) => e.stopPropagation()}>
          <div className="modal-header border-0 pb-0">
            <h2 className="modal-title h5">{title}</h2>
            <button type="button" className="btn btn-outline-primary btn-icon" onClick={onClose} aria-label="Close" title="Close">
              <IconClose width={16} />
            </button>
          </div>
          <div className="modal-body">{children}</div>
        </div>
      </div>
    </div>
  )
}

/** StickyActions keeps form buttons reachable by thumb on phones. */
export function StickyActions({ children }: { children: ReactNode }) {
  return (
    <div className="sticky-actions">
      <div className="d-flex justify-content-end gap-2">{children}</div>
    </div>
  )
}

export function SearchBox({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder: string }) {
  return (
    <input
      type="search"
      className="form-control mb-3"
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
  const finalTone = stage === 'void' ? ' void' : stage === 'overdue' ? ' overdue' : ''
  return (
    <ol className="d-flex align-items-center list-unstyled mb-0" aria-label="Invoice stage">
      {steps.map((s, i) => (
        <li key={s.key} className={`d-flex align-items-center ${i < steps.length - 1 ? 'flex-fill' : ''}`}>
          <div className="d-flex flex-column align-items-center text-center">
            <span className={`stage-dot${s.done ? ' done' + (i === 3 ? finalTone : '') : ''}`}>{s.done ? <i className="bi bi-check-lg" aria-hidden="true" /> : i + 1}</span>
            <span className={`mt-1 fs-8 fw-semibold ${s.done ? 'text-primary-emphasis' : 'text-body-secondary'}`}>{s.label}</span>
            {s.at && <span className="fs-8 text-body-secondary">{formatDate(s.at)}</span>}
          </div>
          {i < steps.length - 1 && <span className={`stage-line${steps[i + 1]!.done ? ' done' : ''}`} />}
        </li>
      ))}
    </ol>
  )
}

/**
 * ConfirmButton asks before an action that cannot be undone (or that e-mails or invoices for
 * real): the button opens a sheet with what will happen, and the action runs once from there.
 */
export function ConfirmButton({ label, title, message, confirmLabel, onConfirm, className, danger = true, disabled }: {
  label: ReactNode
  title: string
  message: ReactNode
  confirmLabel: string
  onConfirm: () => Promise<void> | void
  className?: string
  danger?: boolean
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const run = async () => {
    setBusy(true)
    try {
      await onConfirm()
    } finally {
      setBusy(false)
      setOpen(false)
    }
  }
  return (
    <>
      <button type="button" className={className ?? (danger ? 'btn btn-outline-danger' : 'btn btn-primary')} disabled={disabled || busy} onClick={() => setOpen(true)}>
        {label}
      </button>
      <Sheet title={title} open={open} onClose={() => !busy && setOpen(false)}>
        <div className="vstack gap-3">
          <div className="small text-body">{message}</div>
          <div className="d-flex justify-content-end gap-2">
            <button type="button" className="btn btn-outline-secondary" onClick={() => setOpen(false)} disabled={busy}>
              Cancel
            </button>
            <button type="button" className={danger ? 'btn btn-danger' : 'btn btn-primary'} onClick={() => void run()} disabled={busy}>
              {busy ? 'Working…' : confirmLabel}
            </button>
          </div>
        </div>
      </Sheet>
    </>
  )
}

/** ShowMore is the "Show more" button under a paged list (usePaged). */
export function ShowMore({ list }: { list: { hasMore: boolean; more: () => void; loading: boolean; data?: { items: unknown[]; total: number } } }) {
  if (!list.hasMore || !list.data) return null
  return (
    <button className="btn btn-outline-primary mt-3 w-100" onClick={list.more} disabled={list.loading}>
      Show more ({list.data.total - list.data.items.length} more)
    </button>
  )
}

/**
 * NumberInput is a whole-number field that lets the user type freely (e.g. "1" on the way
 * to "15") and clamps to [min, max] only when it loses focus; a valid value is passed on at
 * once.
 */
export function NumberInput({ id, value, min, max, onChange, className }: { id: string; value: number; min: number; max: number; onChange: (n: number) => void; className?: string }) {
  const [text, setText] = useState(String(value))
  useEffect(() => setText(String(value)), [value])
  return (
    <input
      id={id} type="number" inputMode="numeric" min={min} max={max} className={className} value={text}
      onChange={(e) => {
        setText(e.target.value)
        const n = Number(e.target.value)
        if (e.target.value !== '' && Number.isInteger(n) && n >= min && n <= max) onChange(n)
      }}
      onBlur={() => {
        const n = Math.min(max, Math.max(min, Math.trunc(Number(text)) || min))
        setText(String(n))
        onChange(n)
      }}
    />
  )
}
