import { useState } from 'react'
import { useCan } from './auth'
import { api, idempotencyKey, unwrap, type Invoice } from '../lib/api/client'
import { IconBan, IconClockAlert, IconFilePdf, IconPaid, IconSend } from './icons'
import type { Icon } from './icons'
import { Field, Sheet } from './ui'


function ActionButton({ icon: I, label, onClick, disabled, tone = 'default', href, showLabel }: {
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

/** InvoiceActions is the compact Send / PDF / Paid / Overdue / Void bar for one invoice. */
export function InvoiceActions({ inv, onChanged, onError, showLabels = false }: {
  inv: Invoice
  onChanged: (msg: string) => void
  onError: (e: Error) => void
  showLabels?: boolean
}) {
  const [busy, setBusy] = useState(false)
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
  const send = () =>
    run(
      () => unwrap(api.POST('/invoices/{id}/send', { params: { path: { id: inv.id }, header: { 'Idempotency-Key': idempotencyKey() } }, body: {} })),
      `${inv.number || 'Invoice'} queued for e-mail`,
    )
  return (
    <div className={`d-flex align-items-center gap-1 ${showLabels ? 'flex-wrap' : 'flex-nowrap'}`} role="group" aria-label={`Actions for ${inv.number || 'draft'}`}>
      {can('invoices.send') && (inv.status !== 'draft' || can('invoices.issue')) && (
        <ActionButton showLabel={showLabels} icon={IconSend} label={inv.status === 'draft' ? 'Issue & send' : 'Send e-mail'} onClick={send} disabled={busy || inv.status === 'void'} />
      )}
      <ActionButton showLabel={showLabels} icon={IconFilePdf} label="View PDF" href={`/api/v1/invoices/${inv.id}/pdf`} />
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
