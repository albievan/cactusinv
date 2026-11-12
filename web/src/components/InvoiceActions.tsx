import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useCan } from './auth'
import { api, idempotencyKey, unwrap, type Invoice } from '../lib/api/client'
import { IconBan, IconClockAlert, IconEdit, IconFilePdf, IconPaid, IconSend, IconStatement } from './icons'
import type { Icon } from './icons'
import { Field, Sheet } from './ui'
import { RichNotesEditor } from './RichNotesEditor'
import { formatDate, todayISO } from '../lib/format'

export function ActionButton({ icon: I, label, onClick, disabled, tone = 'default', href, showLabel }: {
  icon: Icon; label: string; onClick?: () => void; disabled?: boolean; tone?: 'default' | 'danger'; href?: string; showLabel: boolean
}) {
  const cls = `btn btn-sm ${tone === 'danger' ? 'btn-outline-danger' : 'btn-outline-primary'}${showLabel ? '' : ' btn-icon'}`
  const text = showLabel ? <span>{label}</span> : <span className="visually-hidden">{label}</span>
  if (href && !disabled) {
    return (
      <a className={cls} href={href} target="_blank" rel="noreferrer" title={label} aria-label={label}>
        <I width={15} height={15} /> {text}
      </a>
    )
  }
  return (
    <button type="button" className={cls} onClick={onClick} disabled={disabled} title={label} aria-label={label}>
      <I width={15} height={15} /> {text}
    </button>
  )
}

/**
 * NotesSheet shows an invoice's own notes and, with invoices.edit and unless it is void,
 * saves changes: the next PDF and every later e-mail of the invoice (a resend too) carry them.
 */
export function NotesSheet({ inv, open, onClose, onSaved, onError }: {
  inv: Invoice
  open: boolean
  onClose: () => void
  onSaved: (msg: string) => void
  onError: (e: Error) => void
}) {
  const can = useCan()
  const [notes, setNotes] = useState(inv.notes)
  const [busy, setBusy] = useState(false)
  const editable = can('invoices.edit') && inv.status !== 'void'
  const save = async () => {
    setBusy(true)
    try {
      await unwrap(api.PUT('/invoices/{id}/notes', { params: { path: { id: inv.id } }, body: { notes, version: inv.version } }))
      onClose()
      onSaved(`Notes saved for ${inv.number || 'the draft'}`)
    } catch (e) {
      onError(e as Error)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Sheet title={`Notes · ${inv.number || 'Draft'}`} open={open} onClose={onClose}>
      <div className="vstack gap-3">
        <Field label="Invoice notes" hint={editable ? 'Printed under the lines. Resending the invoice uses these notes.' : undefined}>
          {(f) => <RichNotesEditor id={f} value={notes} readOnly={!editable} onChange={setNotes} minHeight={160} />}
        </Field>
        {editable && (
          <button className="btn btn-primary w-100" disabled={busy || notes.trim() === inv.notes.trim()} onClick={() => void save()}>
            {busy ? 'Saving…' : 'Save notes'}
          </button>
        )}
      </div>
    </Sheet>
  )
}

/**
 * ScheduledInvoiceActions is the bar for an invoice raised by a schedule: preview it, open
 * its schedule's edit screen, view or edit its own notes.
 */
export function ScheduledInvoiceActions({ inv, onChanged, onError }: {
  inv: Invoice
  onChanged: (msg: string) => void
  onError: (e: Error) => void
}) {
  const can = useCan()
  const nav = useNavigate()
  const [notes, setNotes] = useState(false)
  return (
    <div className="d-flex align-items-center gap-1 flex-nowrap" role="group" aria-label={`Actions for ${inv.number || 'draft'}`}>
      <ActionButton showLabel={false} icon={IconFilePdf} label="Preview invoice" href={`/api/v1/invoices/${inv.id}/pdf`} />
      {can('schedules.edit') && <ActionButton showLabel={false} icon={IconEdit} label="Edit schedule" onClick={() => nav(`/schedules/${inv.scheduleId}/edit`)} />}
      <ActionButton showLabel={false} icon={IconStatement} label={can('invoices.edit') && inv.status !== 'void' ? 'Edit notes' : 'View notes'} onClick={() => setNotes(true)} />
      {notes && <NotesSheet inv={inv} open onClose={() => setNotes(false)} onSaved={onChanged} onError={onError} />}
    </div>
  )
}

/** InvoiceActions is the compact Send / PDF / Paid / Overdue / Void bar for one invoice. */
export function InvoiceActions({ inv, onChanged, onError, showLabels = false, withNotes = false }: {
  inv: Invoice
  onChanged: (msg: string) => void
  onError: (e: Error) => void
  showLabels?: boolean
  /** withNotes adds a Notes button for an issued invoice (a draft's notes are edited with the draft). */
  withNotes?: boolean
}) {
  const [busy, setBusy] = useState(false)
  const [notes, setNotes] = useState(false)
  const [voiding, setVoiding] = useState(false)
  const [reason, setReason] = useState('')
  const path = { params: { path: { id: inv.id } } }
  const open = ['issued', 'sent', 'overdue'].includes(inv.status)
  const run = async (fn: () => Promise<unknown>, msg: string) => {
    setBusy(true)
    try {
      await fn()
      onChanged(msg)
    } catch (e) {
      onError(e as Error)
    } finally {
      setBusy(false)
    }
  }
  const can = useCan()
  const [askDate, setAskDate] = useState(false)
  // Sending a draft issues it. One dated in the past is asked about first, as Issue does:
  // otherwise one tap gave it a number, a past date (perhaps already overdue) and e-mailed it.
  const send = (datedToday = false) =>
    run(async () => {
      if (datedToday) await unwrap(api.POST('/invoices/{id}/issue', { ...path, body: { issueDate: todayISO() } }))
      await unwrap(api.POST('/invoices/{id}/send', { params: { path: { id: inv.id }, header: { 'Idempotency-Key': idempotencyKey() } }, body: {} }))
    }, `${inv.number || 'Invoice'} queued for e-mail`)
  const startSend = () => (inv.status === 'draft' && inv.issueDate < todayISO() ? setAskDate(true) : void send())
  return (
    <div className={`d-flex align-items-center gap-1 ${showLabels ? 'flex-wrap' : 'flex-nowrap'}`} role="group" aria-label={`Actions for ${inv.number || 'draft'}`}>
      {can('invoices.send') && (inv.status !== 'draft' || can('invoices.issue')) && (
        <ActionButton showLabel={showLabels} icon={IconSend} label={inv.status === 'draft' ? 'Issue & send' : 'Send e-mail'} onClick={startSend} disabled={busy || inv.status === 'void'} />
      )}
      <Sheet title="Issue with which date?" open={askDate} onClose={() => setAskDate(false)}>
        <p className="small text-body-secondary mb-0">This draft is dated {formatDate(inv.issueDate)}. The due date follows from the date it is issued with.</p>
        <div className="mt-3 d-flex flex-wrap justify-content-end gap-2">
          <button className="btn btn-outline-primary" disabled={busy} onClick={() => { setAskDate(false); void send() }}>Keep {formatDate(inv.issueDate)}</button>
          <button className="btn btn-primary" disabled={busy} onClick={() => { setAskDate(false); void send(true) }}>Issue dated today</button>
        </div>
      </Sheet>
      <ActionButton showLabel={showLabels} icon={IconFilePdf} label="View PDF" href={`/api/v1/invoices/${inv.id}/pdf`} />
      {withNotes && inv.status !== 'draft' && <ActionButton showLabel={showLabels} icon={IconStatement} label={can('invoices.edit') && inv.status !== 'void' ? 'Edit notes' : 'View notes'} onClick={() => setNotes(true)} />}
      {notes && <NotesSheet inv={inv} open onClose={() => setNotes(false)} onSaved={onChanged} onError={onError} />}
      {can('invoices.pay') && <ActionButton showLabel={showLabels} icon={IconPaid} label="Mark paid" disabled={busy || !open} onClick={() => run(() => unwrap(api.POST('/invoices/{id}/mark-paid', path)), `${inv.number} marked paid`)} />}
      {can('invoices.overdue') && <ActionButton showLabel={showLabels} icon={IconClockAlert} label="Mark overdue" disabled={busy || !(inv.status === 'issued' || inv.status === 'sent')} onClick={() => run(() => unwrap(api.POST('/invoices/{id}/mark-overdue', path)), `${inv.number} marked overdue`)} />}
      {can('invoices.void') && <ActionButton showLabel={showLabels} icon={IconBan} label="Void" tone="danger" disabled={busy || !open} onClick={() => setVoiding(true)} />}
      <Sheet title={`Void ${inv.number}`} open={voiding} onClose={() => setVoiding(false)}>
        <div className="vstack gap-3">
          <p className="small text-body-secondary mb-0">The number stays used, scheduled e-mails are cancelled and the void is recorded in the audit trail. This cannot be undone.</p>
          <Field label="Reason">{(f) => <input id={f} className="form-control" value={reason} onChange={(e) => setReason(e.target.value)} autoFocus />}</Field>
          <button
            className="btn btn-danger w-100"
            disabled={!reason.trim() || busy}
            onClick={() => {
              setVoiding(false)
              void run(() => unwrap(api.POST('/invoices/{id}/void', { ...path, body: { reason } })), `${inv.number} voided`)
            }}
          >
            Void invoice
          </button>
        </div>
      </Sheet>
    </div>
  )
}
