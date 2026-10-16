import { useState, type ComponentType, type SVGProps } from 'react'
import { useCan } from './auth'
import { api, idempotencyKey, unwrap, type Invoice } from '../lib/api/client'
import { IconBan, IconClockAlert, IconFilePdf, IconPaid, IconSend } from './icons'
import { Field, Sheet } from './ui'

type Icon = ComponentType<SVGProps<SVGSVGElement>>

function ActionButton({ icon: I, label, onClick, disabled, tone = 'default', href, showLabel }: {
  icon: Icon; label: string; onClick?: () => void; disabled?: boolean; tone?: 'default' | 'danger'; href?: string; showLabel: boolean
}) {
  const cls = `inline-flex h-8 items-center justify-center gap-1.5 rounded-lg text-xs font-semibold ring-1 transition disabled:cursor-not-allowed disabled:opacity-30 ${
    showLabel ? 'px-2.5' : 'w-8'
  } ${tone === 'danger' ? 'text-red-700 ring-red-200 hover:bg-red-50' : 'text-brand-800 ring-cream-300 hover:bg-cream-50'}`
  const text = showLabel ? <span>{label}</span> : <span className="sr-only">{label}</span>
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
    <div className={`flex items-center gap-1 ${showLabels ? 'flex-wrap' : 'flex-nowrap'}`} role="group" aria-label={`Actions for ${inv.number || 'draft'}`}>
      {can('invoices.send') && (inv.status !== 'draft' || can('invoices.edit')) && (
        <ActionButton showLabel={showLabels} icon={IconSend} label={inv.status === 'draft' ? 'Issue & send' : 'Send e-mail'} onClick={send} disabled={busy || inv.status === 'void'} />
      )}
      <ActionButton showLabel={showLabels} icon={IconFilePdf} label="View PDF" href={`/api/v1/invoices/${inv.id}/pdf`} />
      {can('invoices.status') && (
        <>
      <ActionButton showLabel={showLabels} icon={IconPaid} label="Mark paid" disabled={busy || !open} onClick={() => run(() => unwrap(api.POST('/invoices/{id}/mark-paid', path)), `${inv.number} marked paid`)} />
      <ActionButton showLabel={showLabels} icon={IconClockAlert} label="Mark overdue" disabled={busy || !(inv.status === 'issued' || inv.status === 'sent')} onClick={() => run(() => unwrap(api.POST('/invoices/{id}/mark-overdue', path)), `${inv.number} marked overdue`)} />
      <ActionButton showLabel={showLabels} icon={IconBan} label="Void" tone="danger" disabled={busy || !open} onClick={() => setVoiding(true)} />
        </>
      )}
      <Sheet title={`Void ${inv.number}`} open={voiding} onClose={() => setVoiding(false)}>
        <div className="space-y-4">
          <p className="text-sm text-slate-600">The number stays used, scheduled e-mails are cancelled and the void is recorded in the audit trail. This cannot be undone.</p>
          <Field label="Reason">{(f) => <input id={f} className="input" value={reason} onChange={(e) => setReason(e.target.value)} autoFocus />}</Field>
          <button
            className="btn-danger w-full"
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
