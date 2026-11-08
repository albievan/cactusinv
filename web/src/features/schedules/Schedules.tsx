import { useEffect, useState, type FormEvent } from 'react'
import { TimeZoneSelect, deviceZone } from '../../components/TimeZoneSelect'
import { Can } from '../../components/Access'
import { useCan } from '../../components/auth'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, unwrap, type Schedule, type Schemas } from '../../lib/api/client'
import { formatDate, formatInZone, todayISO } from '../../lib/format'
import { useCurrencies } from '../../lib/lookups'
import { useAsync } from '../../lib/useAsync'
import { LineEditor, newLine, type EditLine } from '../../components/LineEditor'
import { Badge, ConfirmButton, Empty, ErrorBanner, Field, NumberInput, Page, Sheet, Spinner, StickyActions } from '../../components/ui'
import { IconBolt, IconEdit, IconPause, IconPlay, IconPlus, IconTrash } from '../../components/icons'
import type { Icon } from '../../components/icons'
import { uuid } from '../../lib/uuid'

const weekdays = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

export function describe(r: Schemas['Recurrence']): string {
  const every = r.interval > 1 ? `every ${r.interval} ` : 'every '
  const day = r.dayOfMonth === 0 ? 'the last day' : r.dayOfMonth === 1 ? 'the first day' : `day ${r.dayOfMonth}`
  const friday = r.weekendRule === 'previous-friday' ? ' (the Friday before if on a weekend)' : ''
  switch (r.frequency) {
    case 'once': return `once on ${formatDate(r.startDate)} at ${r.sendTime}`
    case 'weekly': return `${every}${r.interval > 1 ? 'weeks' : 'week'} on ${weekdays[r.weekday]} at ${r.sendTime}`
    case 'monthly': return `${every}${r.interval > 1 ? 'months' : 'month'} on ${day} at ${r.sendTime}${friday}`
    case 'quarterly': return `${every}${r.interval > 1 ? 'quarters' : 'quarter'} on ${day} at ${r.sendTime}${friday}`
    default: return `${every}${r.interval > 1 ? 'years' : 'year'} on ${day} at ${r.sendTime}${friday}`
  }
}

type DayMode = 'first' | 'last' | 'day'
const dayMode = (dayOfMonth: number): DayMode => (dayOfMonth === 0 ? 'last' : dayOfMonth === 1 ? 'first' : 'day')

export function purposeLabel(s: Schedule): string {
  if (s.kind !== 'email') {
    if (!s.autoSend) return 'Raises a recurring invoice'
    return s.emailDelayDays > 0 ? `Raises a recurring invoice, e-mails it ${s.emailDelayDays} day${s.emailDelayDays === 1 ? '' : 's'} later` : 'Raises and e-mails a recurring invoice'
  }
  return s.emailScope === 'statement' ? 'E-mails a statement' : s.emailScope === 'outstanding' ? 'E-mails outstanding invoices' : 'E-mails unsent invoices'
}

type Purpose = 'invoice' | 'unsent' | 'outstanding' | 'statement'

const typeLabels: Record<Purpose, string> = {
  invoice: 'Recurring invoice',
  unsent: 'E-mail unsent invoices',
  outstanding: 'E-mail outstanding invoices',
  statement: 'Statement',
}

/** typeLabel names what a schedule does, from its kind and e-mail scope. */
export function typeLabel(s: Schedule): string {
  return s.kind === 'email' ? typeLabels[(s.emailScope ?? 'unsent') as Purpose] : typeLabels.invoice
}

/** LastRun shows when the schedule last ran and the server's outcome of its latest run. */
function LastRun({ s }: { s: Schedule }) {
  const at = s.lastRunAt ? formatInZone(s.lastRunAt, s.recurrence.timeZone) : 'Never'
  if (s.lastRunResult !== 'failed') return <span>{at}</span>
  return (
    <span>
      <span className="d-block">{at}</span>
      <span className="d-block fs-8 fw-medium text-danger" title={s.lastError}>Failed{s.failureCount ? ` (${s.failureCount}×)` : ''}{s.lastError ? `: ${s.lastError}` : ''}</span>
    </span>
  )
}

type Pending = { kind: 'run' | 'delete'; s: Schedule }

function IconAction({ icon: I, label, title, onClick, disabled, danger, to }: {
  icon: Icon; label: string; title: string; onClick?: () => void; disabled?: boolean; danger?: boolean; to?: string
}) {
  const cls = `btn btn-sm btn-icon ${danger ? 'btn-outline-danger' : 'btn-outline-primary'}`
  if (to) return <Link to={to} className={cls} aria-label={label} title={title}><I width={16} /></Link>
  return <button type="button" className={cls} aria-label={label} title={title} onClick={onClick} disabled={disabled}><I width={16} /></button>
}

/** ScheduleActions is the compact Edit / Pause or Resume / Run now / Delete bar for one schedule. */
function ScheduleActions({ s, busy, act, ask }: { s: Schedule; busy: boolean; act: (s: Schedule, what: 'pause' | 'resume') => void; ask: (p: Pending) => void }) {
  return (
    <div className="d-flex flex-nowrap align-items-center gap-1" role="group" aria-label={`Actions for ${s.name}`}>
      <Can need="schedules.edit"><IconAction icon={IconEdit} label={`Edit ${s.name}`} title="Edit" to={`/schedules/${s.id}/edit`} /></Can>
      {s.status === 'paused'
        ? <Can need="schedules.resume"><IconAction icon={IconPlay} label={`Resume ${s.name}`} title="Resume" onClick={() => act(s, 'resume')} disabled={busy} /></Can>
        : <Can need="schedules.pause"><IconAction icon={IconPause} label={`Pause ${s.name}`} title="Pause" onClick={() => act(s, 'pause')} disabled={busy || s.status !== 'active'} /></Can>}
      <Can need="schedules.run"><IconAction icon={IconBolt} label={`Run ${s.name} now`} title="Run now" onClick={() => ask({ kind: 'run', s })} disabled={busy || s.status !== 'active'} /></Can>
      <Can need="schedules.delete"><IconAction icon={IconTrash} label={`Delete ${s.name}`} title="Delete" onClick={() => ask({ kind: 'delete', s })} disabled={busy} danger /></Can>
    </div>
  )
}

export function ScheduleList() {
  const { data, error, loading, reload } = useAsync(() => unwrap(api.GET('/schedules', { params: { query: { limit: 200 } } })), [])
  const customers = useAsync(() => unwrap(api.GET('/customers', { params: { query: { limit: 200, includeInactive: true } } })).then((r) => r.items), [])
  const name = (id: string) => customers.data?.find((c) => c.id === id)?.name ?? ''
  const [busy, setBusy] = useState<string | null>(null)
  const [notice, setNotice] = useState('')
  const [actionError, setActionError] = useState<Error | null>(null)
  const [pending, setPending] = useState<Pending | null>(null)
  const run = async (s: Schedule, req: () => Promise<unknown>, done: string) => {
    setBusy(s.id ?? '')
    setNotice('')
    setActionError(null)
    try {
      await req()
      setNotice(done)
      reload()
    } catch (e) {
      setActionError(e as Error)
    } finally {
      setBusy(null)
    }
  }
  const path = (s: Schedule) => ({ params: { path: { id: s.id ?? '' } } })
  const act = (s: Schedule, what: 'pause' | 'resume') =>
    what === 'pause'
      ? run(s, () => unwrap(api.POST('/schedules/{id}/pause', path(s))), `${s.name} paused`)
      : run(s, () => unwrap(api.POST('/schedules/{id}/resume', path(s))), `${s.name} resumed`)
  const confirm = () => {
    if (!pending) return
    const { kind, s } = pending
    setPending(null)
    if (kind === 'run') run(s, () => unwrap(api.POST('/schedules/{id}/run-now', path(s))), `${s.name} runs within a minute`)
    else run(s, () => unwrap(api.DELETE('/schedules/{id}', path(s))), `${s.name} deleted`)
  }
  const items = data?.items ?? []
  const confirmClass = pending?.kind === 'run' ? 'btn btn-primary' : 'btn btn-danger'
  const nextRun = (s: Schedule) => (s.nextRunAt ? formatInZone(s.nextRunAt, s.recurrence.timeZone) : 'No upcoming run')
  return (
    <Page title="Schedules" actions={<Can need="schedules.create"><Link to="/schedules/new" className="btn btn-primary"><IconPlus width={16} />New</Link></Can>}>
      <p className="mb-3 small text-body-secondary">Recurring invoices are issued and e-mailed automatically.</p>
      <ErrorBanner error={error ?? actionError} />
      {notice && <div role="status" className="mb-3 rounded-3 border-start border-4 border-primary bg-white p-3 small fw-medium text-primary-emphasis shadow-sm">{notice}</div>}
      {loading && !data ? <Spinner /> : items.length === 0 ? <Empty>No schedules yet.</Empty> : (
        <>
          <ul className="list-unstyled vstack gap-2 mb-0 d-md-none" aria-label="Schedules">
            {items.map((s) => (
              <li key={s.id} className="card card-body vstack gap-2 small">
                <div className="d-flex align-items-center justify-content-between gap-2">
                  <Link to={`/schedules/${s.id}`} className="fw-semibold text-primary-emphasis">{s.name}</Link>
                  <Badge status={s.status ?? 'active'} />
                </div>
                <div className="text-body-secondary">{name(s.customerId)} · {typeLabel(s)}</div>
                <div className="text-body-secondary">{describe(s.recurrence)}</div>
                <dl className="d-grid column-gap-3 row-gap-1 mb-0" style={{ gridTemplateColumns: 'auto 1fr' }}>
                  <dt className="fw-normal text-body-secondary">Last run</dt><dd className="mb-0"><LastRun s={s} /></dd>
                  <dt className="fw-normal text-body-secondary">Next run</dt><dd className="mb-0">{nextRun(s)}</dd>
                </dl>
                <ScheduleActions s={s} busy={busy === s.id} act={act} ask={setPending} />
              </li>
            ))}
          </ul>
          <div className="card table-responsive d-none d-md-block">
            <table className="table table-hover align-top mb-0 small">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Customer</th>
                  <th>Type</th>
                  <th>Repeats</th>
                  <th>Last run</th>
                  <th>Next run</th>
                  <th>Status</th>
                  <th className="text-end">Actions</th>
                </tr>
              </thead>
              <tbody>
                {items.map((s) => (
                  <tr key={s.id}>
                    <td><Link to={`/schedules/${s.id}`} className="fw-semibold text-primary-emphasis">{s.name}</Link></td>
                    <td>{name(s.customerId)}</td>
                    <td>{typeLabel(s)}</td>
                    <td>{describe(s.recurrence)}</td>
                    <td><LastRun s={s} /></td>
                    <td>{nextRun(s)}</td>
                    <td><Badge status={s.status ?? 'active'} /></td>
                    <td><div className="d-flex justify-content-end"><ScheduleActions s={s} busy={busy === s.id} act={act} ask={setPending} /></div></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      <Sheet title={pending?.kind === 'run' ? 'Run schedule now?' : 'Delete schedule?'} open={pending !== null} onClose={() => setPending(null)}>
        {pending && (
          <div className="vstack gap-3 small">
            <p className="mb-0">
              {pending.kind === 'run'
                ? pending.s.kind === 'email'
                  ? `“${pending.s.name}” e-mails ${name(pending.s.customerId) || 'the customer'} for real within a minute.`
                  : `“${pending.s.name}” raises a real invoice${pending.s.autoSend ? ' and e-mails it' : ''} within a minute.`
                : `“${pending.s.name}” is deleted. Invoices it created are kept.`}
            </p>
            <div className="d-flex justify-content-end gap-2">
              <button type="button" className="btn btn-outline-primary" onClick={() => setPending(null)}>Cancel</button>
              <button type="button" className={confirmClass} onClick={confirm}>{pending.kind === 'run' ? 'Run now' : 'Delete'}</button>
            </div>
          </div>
        )}
      </Sheet>
    </Page>
  )
}

export function ScheduleDetailPage() {
  const { id = '' } = useParams()
  const { data, error, loading, setData } = useAsync(() => unwrap(api.GET('/schedules/{id}', { params: { path: { id } } })), [id])
  const nav = useNavigate()
  const [actionError, setActionError] = useState<Error | null>(null)
  const can = useCan()
  if (loading && !data) return <Spinner />
  if (!data) return <Page title="Schedule" back="/schedules"><ErrorBanner error={error} /></Page>
  const s = data.schedule
  const path = { params: { path: { id } } }
  const act = async (p: Promise<{ data?: Schemas['ScheduleDetail']; error?: unknown; response: Response }>) => {
    setActionError(null)
    try { setData(await unwrap(p)) } catch (e) { setActionError(e as Error) }
  }
  return (
    <Page title={s.name} back="/schedules">
      <ErrorBanner error={actionError} />
      <div className="card card-body mb-3 vstack gap-1 small">
        <div className="d-flex align-items-center gap-2"><Badge status={s.status ?? 'active'} /> <span>{describe(s.recurrence)} ({s.recurrence.timeZone})</span></div>
        <div className="fw-medium text-primary-emphasis">{purposeLabel(s)}</div>
        {s.lastError && <div className="text-danger">Last error ({s.failureCount}×): {s.lastError}</div>}
      </div>
      <div className="d-grid d-md-flex gap-2 mb-3" style={{ gridTemplateColumns: '1fr 1fr' }}>
        {can('schedules.edit') && <Link to={`/schedules/${id}/edit`} className="btn btn-outline-primary">Edit</Link>}
        {can('schedules.pause') && s.status === 'active' && <button className="btn btn-outline-primary" onClick={() => act(api.POST('/schedules/{id}/pause', path))}>Pause</button>}
        {can('schedules.resume') && s.status === 'paused' && <button className="btn btn-primary" onClick={() => act(api.POST('/schedules/{id}/resume', path))}>Resume</button>}
        {can('schedules.run') && s.status === 'active' && (
          <ConfirmButton
            label="Run now"
            danger={false}
            title={`Run ${s.name} now?`}
            message="It runs for real within a minute: it raises the invoice or e-mails the customer as it would on its next run."
            confirmLabel="Run now"
            onConfirm={() => act(api.POST('/schedules/{id}/run-now', path))}
          />
        )}
        {can('schedules.delete') && <ConfirmButton
          label="Delete"
          title={`Delete ${s.name}?`}
          message="The schedule stops and is deleted. Invoices and e-mails it already made stay. This cannot be undone."
          confirmLabel="Delete schedule"
          onConfirm={async () => { try { await unwrap(api.DELETE('/schedules/{id}', path)); nav('/schedules') } catch (e) { setActionError(e as Error) } }}
        />}
      </div>
      <section className="card card-body mb-3 small">
        <h2 className="fs-6 fw-semibold mb-2">Upcoming runs</h2>
        {data.upcoming.length === 0 ? <p className="text-body-secondary mb-0">None.</p> : <ul className="list-unstyled vstack gap-1 mb-0">{data.upcoming.map((t) => <li key={t}>{formatInZone(t, s.recurrence.timeZone)}</li>)}</ul>}
      </section>
      <section className="card card-body small">
        <h2 className="fs-6 fw-semibold mb-2">Runs</h2>
        {data.runs.length === 0 ? <p className="text-body-secondary mb-0">None yet.</p> : (
          <ul className="list-unstyled vstack gap-1 mb-0">{data.runs.map((r) => <li key={r.period}>{r.invoiceId ? <Link className="text-decoration-underline" to={`/invoices/${r.invoiceId}`}>Invoice for {formatDate(r.period)}</Link> : <span>E-mails sent for {formatDate(r.period)}</span>}</li>)}</ul>
        )}
      </section>
    </Page>
  )
}

// The start date is today in the browser's zone, taken when the form opens (not at load).
const blankRecurrence = (): Schemas['Recurrence'] => ({ frequency: 'monthly', interval: 1, dayOfMonth: 1, weekday: 1, sendTime: '09:00', startDate: todayISO() })

const purposes: { key: Purpose; title: string; hint: string }[] = [
  { key: 'invoice', title: 'Recurring invoice: raise, then e-mail', hint: 'Raises a predefined invoice from the products below on every run, then e-mails it straight away or a few days later.' },
  { key: 'unsent', title: 'E-mail invoices not yet e-mailed', hint: "Sends each of the customer's issued invoices that has never been e-mailed." },
  { key: 'outstanding', title: 'E-mail all outstanding invoices', hint: 'Sends every unpaid invoice (issued, e-mailed or overdue) again.' },
  { key: 'statement', title: 'E-mail a statement', hint: 'Sends one statement of account listing everything unpaid, with aging.' },
]

/**
 * DisclaimerNote shows, read-only, the disclaimer printed on every invoice for the customer:
 * which one applies is the server's disclaimerSource; it is added when the PDF is rendered,
 * so it is never copied into the notes.
 */
function DisclaimerNote({ customer, businessText }: { customer: Schemas['Customer']; businessText?: string }) {
  const own = customer.disclaimerSource === 'customer'
  const text = (own ? customer.defaultNotes : businessText)?.trim()
  return (
    <div data-testid="schedule-disclaimer" className="rounded-3 bg-body-tertiary border p-3 small">
      <p className="fw-medium text-primary-emphasis mb-0">Disclaimer printed on every invoice — {own ? "this customer's own" : 'the business disclaimer'}:</p>
      <p className="mt-1 mb-0 text-body" style={{ whiteSpace: 'pre-line' }}>{text || 'None set.'}</p>
      <Link className="mt-1 d-inline-block text-decoration-underline" to={own ? `/customers/${customer.id}` : '/settings/business'}>
        {own ? "Edit this customer's disclaimer" : 'Edit the business disclaimer'}
      </Link>
    </div>
  )
}

export function ScheduleEditor() {
  const { id } = useParams()
  const [params] = useSearchParams()
  const nav = useNavigate()
  const { minor } = useCurrencies()
  // Inactive customers too: an existing schedule may belong to one.
  const customers = useAsync(() => unwrap(api.GET('/customers', { params: { query: { limit: 200, includeInactive: true } } })).then((r) => r.items), [])
  const existing = useAsync(() => (id ? unwrap(api.GET('/schedules/{id}', { params: { path: { id } } })) : Promise.resolve(undefined)), [id])
  const [s, setS] = useState<Schedule>({
    customerId: params.get('customer') ?? '', name: '', kind: 'invoice', recurrence: blankRecurrence(), lines: [], header: {}, notes: '',
    autoSend: false, emailDelayDays: 0, emailSubject: '', emailBody: '', version: 0,
  })
  const [purpose, setPurpose] = useState<Purpose>(params.get('kind') === 'statement' ? 'statement' : params.get('kind') === 'email' ? 'unsent' : 'invoice')
  const [lines, setLines] = useState<EditLine[]>([newLine()])
  const [preview, setPreview] = useState<Schemas['RunPreview'] | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  // Suggested e-mail texts per type come from the server; a new schedule starts with them and
  // follows the chosen type until the user edits a field.
  const defaults = useAsync(() => unwrap(api.GET('/schedules/defaults')), [])
  const business = useAsync(() => unwrap(api.GET('/settings/business')), [])
  const [edited, setEdited] = useState({ subject: false, body: false })
  useEffect(() => {
    const x = existing.data?.schedule
    if (!x) return
    setS(x)
    setPurpose(x.kind === 'email' ? ((x.emailScope ?? 'unsent') as Purpose) : 'invoice')
    // The product's name and price come from the look-up (productName, productUnitPrice), never
    // from the stored description; unitPrice is a fixed price that overrides the product's.
    setLines(x.lines.map((l) => ({
      key: uuid(), productId: l.productId, productName: l.productName ?? '', productStatus: l.productStatus, description: l.description ?? '',
      quantity: l.quantity, unitPrice: l.unitPrice ?? l.productUnitPrice ?? '', priceOverride: l.unitPrice ?? undefined,
      discountPercent: l.discountPercent === '0' ? '' : l.discountPercent ?? '',
    })))
  }, [existing.data])
  useEffect(() => {
    const t = setTimeout(() => {
      // Editing: the runs as saving would set them (billed periods are not run again).
      api.POST('/schedules/preview', { params: { query: id ? { scheduleId: id } : {} }, body: s.recurrence }).then(({ data }) => setPreview(data ?? null))
    }, 300)
    return () => clearTimeout(t)
  }, [s.recurrence, id])
  const set = (patch: Partial<Schedule>) => setS({ ...s, ...patch })
  const setR = (patch: Partial<Schemas['Recurrence']>) => setS({ ...s, recurrence: { ...s.recurrence, ...patch } })
  useEffect(() => {
    const t = defaults.data?.[purpose]
    if (id || !t) return
    setS((cur) => ({ ...cur, emailSubject: edited.subject ? cur.emailSubject : t.subject, emailBody: edited.body ? cur.emailBody : t.body }))
  }, [id, defaults.data, purpose, edited])
  const template = defaults.data?.[purpose]
  const customer = customers.data?.find((c) => c.id === s.customerId)
  const r = s.recurrence
  const zone = r.timeZone ?? preview?.timeZone
  const zoneHint = zone && zone !== deviceZone() && preview?.runs[0] ? `The first run is ${formatInZone(preview.runs[0], deviceZone())} on this device.` : undefined
  const isInvoice = purpose === 'invoice'

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (isInvoice && lines.some((l) => !l.productId || l.productStatus === 'missing')) {
      setError(new Error('Choose a product for every line.'))
      return
    }
    setBusy(true)
    setError(null)
    const body: Schedule = {
      ...s,
      kind: isInvoice ? 'invoice' : 'email',
      emailScope: isInvoice ? undefined : purpose,
      autoSend: isInvoice ? s.autoSend : true,
      lines: isInvoice ? lines.map((l) => ({ productId: l.productId, quantity: l.quantity, unitPrice: l.priceOverride, discountPercent: l.discountPercent || '0' })) : [],
    }
    try {
      const d = id ? await unwrap(api.PUT('/schedules/{id}', { params: { path: { id } }, body })) : await unwrap(api.POST('/schedules', { body }))
      nav(`/schedules/${d.schedule.id}`, { replace: true })
    } catch (err) {
      setError(err as Error)
      setBusy(false)
    }
  }
  if (customers.loading || (id && existing.loading)) return <Spinner />
  return (
    <Page title={id ? 'Edit schedule' : 'New schedule'} back={id ? `/schedules/${id}` : '/schedules'}>
      <form onSubmit={submit} className="vstack gap-4">
        <ErrorBanner error={error} />
        <div className="card card-body">
          <div className="row row-cols-1 row-cols-md-2 g-3">
            <Field label="Customer" hint={customer ? `Billed in ${customer.currency}` : undefined}>
              {(f) => (
                <select id={f} className="form-select" value={s.customerId} onChange={(e) => set({ customerId: e.target.value })} required>
                  <option value="">Choose…</option>
                  {customers.data?.map((c) => <option key={c.id} value={c.id}>{c.name} ({c.currency})</option>)}
                </select>
              )}
            </Field>
            <Field label="Name">{(f) => <input id={f} className="form-control" value={s.name} onChange={(e) => set({ name: e.target.value })} placeholder="e.g. Monthly subscription, or Weekly reminder" required />}</Field>
          </div>
        </div>
        <fieldset className="card card-body">
          <legend className="visually-hidden">What should this schedule do?</legend>
          <h2 className="fs-5 fw-semibold mb-3">What should this schedule do?</h2>
          <div className="row row-cols-1 row-cols-md-2 g-2">
            {purposes.map((p) => (
              <div key={p.key}>
                <label className={`d-flex gap-3 h-100 rounded-3 border p-3 ${purpose === p.key ? 'bg-primary-subtle border-primary' : 'bg-white'}`}>
                  <input type="radio" name="purpose" className="form-check-input flex-shrink-0 mt-1" checked={purpose === p.key} onChange={() => setPurpose(p.key)} />
                  <span>
                    <span className="d-block fw-semibold text-primary-emphasis">{p.title}</span>
                    <span className="small text-body-secondary">{p.hint}</span>
                  </span>
                </label>
              </div>
            ))}
          </div>
        </fieldset>
        <div className="card card-body">
          <div className="row row-cols-1 row-cols-md-3 g-3">
            <Field label="Repeats">
              {(f) => (
                <select id={f} className="form-select" value={r.frequency} onChange={(e) => setR({ frequency: e.target.value as Schemas['Recurrence']['frequency'] })}>
                  <option value="once">Once</option><option value="weekly">Weekly</option><option value="monthly">Monthly</option><option value="quarterly">Quarterly</option><option value="yearly">Yearly</option>
                </select>
              )}
            </Field>
            {r.frequency !== 'once' && <Field label="Every">{(f) => <input id={f} type="number" min={1} max={52} inputMode="numeric" className="form-control" value={r.interval} onChange={(e) => setR({ interval: Number(e.target.value) })} />}</Field>}
            {r.frequency === 'weekly' && (
              <Field label="On">{(f) => <select id={f} className="form-select" value={r.weekday} onChange={(e) => setR({ weekday: Number(e.target.value) })}>{weekdays.map((w, i) => <option key={w} value={i}>{w}</option>)}</select>}</Field>
            )}
            {['monthly', 'quarterly', 'yearly'].includes(r.frequency) && (
              <>
                <Field label="On">
                  {(f) => (
                    <select id={f} className="form-select" value={dayMode(r.dayOfMonth)} onChange={(e) => {
                      const m = e.target.value as DayMode
                      setR({ dayOfMonth: m === 'first' ? 1 : m === 'last' ? 0 : dayMode(r.dayOfMonth) === 'day' ? r.dayOfMonth : 15 })
                    }}>
                      <option value="first">First day</option>
                      <option value="last">Last day</option>
                      <option value="day">Day of the month…</option>
                    </select>
                  )}
                </Field>
                {dayMode(r.dayOfMonth) === 'day' && (
                  <Field label="Day" hint="A day past the month's end runs on its last day.">
                    {(f) => <NumberInput id={f} min={2} max={31} className="form-control" value={r.dayOfMonth} onChange={(n) => setR({ dayOfMonth: n })} />}
                  </Field>
                )}
                <Field label="If it falls on a weekend" hint="A run moved to the Friday before is still for its own month.">
                  {(f) => (
                    <select id={f} className="form-select" value={r.weekendRule ?? preview?.weekendRule ?? ''} onChange={(e) => setR({ weekendRule: e.target.value as Schemas['Recurrence']['weekendRule'] })}>
                      {!(r.weekendRule ?? preview?.weekendRule) && <option value="" disabled>…</option>}
                      <option value="previous-friday">Send on the Friday before</option>
                      <option value="none">Send on that day</option>
                    </select>
                  )}
                </Field>
              </>
            )}
            <Field label="Time" hint={preview?.timeZone ? `${preview.timeZone} time` : undefined}>{(f) => <input id={f} type="time" className="form-control" value={r.sendTime} onChange={(e) => setR({ sendTime: e.target.value })} />}</Field>
            <Field label="Time zone" hint={zoneHint}>{(f) => <TimeZoneSelect id={f} value={r.timeZone ?? preview?.timeZone ?? ''} onChange={(z) => setR({ timeZone: z })} />}</Field>
            <Field label="Starts">{(f) => <input id={f} type="date" className="form-control" value={r.startDate} onChange={(e) => setR({ startDate: e.target.value })} />}</Field>
            <Field label="Ends (optional)">{(f) => <input id={f} type="date" className="form-control" value={r.endDate ?? ''} onChange={(e) => setR({ endDate: e.target.value || undefined })} />}</Field>
            <div className="w-100 small text-body-secondary">
              <span className="fw-semibold text-primary-emphasis">Next runs: </span>
              {preview?.runs.length ? preview.runs.slice(0, 4).map((t) => formatInZone(t, preview.timeZone)).join(' · ') : '—'}
            </div>
          </div>
        </div>
        {isInvoice && (
          <h2 className="fs-5 fw-semibold pt-1 mb-0">Step 1 · Raise the invoice</h2>
        )}
        {isInvoice && customer && <LineEditor lines={lines} onChange={setLines} currency={customer.currency} minorUnits={minor(customer.currency)} />}
        {isInvoice && (
          <section className="card card-body vstack gap-3">
            <h2 className="fs-5 fw-semibold mb-0">Step 2 · E-mail it</h2>
            <div className="row row-cols-1 row-cols-md-3 g-2" role="radiogroup" aria-label="E-mail the raised invoice">
              {[
                { key: 'none', label: "Don't e-mail", hint: 'Raise only; send it yourself later.' },
                { key: 'now', label: 'Immediately', hint: 'E-mail as soon as it is raised.' },
                { key: 'later', label: 'Some days later', hint: 'E-mail a set number of days after raising.' },
              ].map((o) => {
                const mode = !s.autoSend ? 'none' : s.emailDelayDays > 0 ? 'later' : 'now'
                return (
                  <div key={o.key}>
                    <label className={`d-flex gap-3 h-100 rounded-3 border p-3 ${mode === o.key ? 'bg-primary-subtle border-primary' : 'bg-white'}`}>
                      <input
                        type="radio"
                        name="emailmode"
                        className="form-check-input flex-shrink-0 mt-1"
                        checked={mode === o.key}
                        onChange={() => set(o.key === 'none' ? { autoSend: false, emailDelayDays: 0 } : o.key === 'now' ? { autoSend: true, emailDelayDays: 0 } : { autoSend: true, emailDelayDays: Math.max(s.emailDelayDays, 2) })}
                      />
                      <span>
                        <span className="d-block fw-semibold text-primary-emphasis">{o.label}</span>
                        <span className="small text-body-secondary">{o.hint}</span>
                      </span>
                    </label>
                  </div>
                )
              })}
            </div>
            {s.autoSend && s.emailDelayDays > 0 && (
              <Field label="Days after raising">
                {(f) => <div style={{ maxWidth: '10rem' }}><NumberInput id={f} min={1} max={90} className="form-control" value={s.emailDelayDays} onChange={(n) => set({ emailDelayDays: n })} /></div>}
              </Field>
            )}
            {preview && preview.runs.length > 0 && (
              <div>
                <h3 className="small fw-semibold text-primary-emphasis mb-2">Timeline</h3>
                <ol className="list-unstyled vstack gap-1 small mb-0">
                  {preview.runs.slice(0, 3).map((t) => {
                    const mail = new Date(new Date(t).getTime() + s.emailDelayDays * 86400000).toISOString()
                    return (
                      <li key={t} className="d-flex flex-wrap align-items-center gap-2">
                        <span className="badge rounded-pill bg-primary-subtle text-primary-emphasis border border-primary-subtle">Raise</span> {formatInZone(t, preview.timeZone)}
                        {s.autoSend && <><i className="bi bi-arrow-right" aria-hidden="true" /><span className="badge rounded-pill bg-secondary-subtle text-secondary-emphasis border border-secondary-subtle">E-mail</span> {formatInZone(mail, preview.timeZone)}</>}
                      </li>
                    )
                  })}
                </ol>
              </div>
            )}
          </section>
        )}
        <div className="card card-body vstack gap-3">
          {isInvoice && (
            <>
              <Field label="VAT %" hint="Empty uses the business default VAT. 0 = no VAT.">{(f) => <input id={f} className="form-control" inputMode="decimal" placeholder="Business default" value={s.vatPercent ?? ''} onChange={(e) => set({ vatPercent: e.target.value === '' ? undefined : e.target.value })} />}</Field>
              <Field label="Invoice notes" hint="Optional. The disclaimer is added automatically.">{(f) => <textarea id={f} className="form-control" value={s.notes} onChange={(e) => set({ notes: e.target.value })} />}</Field>
              {customer && <DisclaimerNote customer={customer} businessText={business.data?.defaultNotes} />}
            </>
          )}
          <Field label="E-mail subject" hint={`Empty uses the default.${template ? ` Placeholders: ${template.placeholders.join(' ')}` : ''}`}>
            {(f) => <input id={f} className="form-control" value={s.emailSubject} onChange={(e) => { setEdited((x) => ({ ...x, subject: true })); set({ emailSubject: e.target.value }) }} />}
          </Field>
          <Field label="E-mail body">
            {(f) => <textarea id={f} className="form-control" style={{ minHeight: 160 }} value={s.emailBody} onChange={(e) => { setEdited((x) => ({ ...x, body: true })); set({ emailBody: e.target.value }) }} />}
          </Field>
        </div>
        <StickyActions>
          <button type="button" className="btn btn-outline-primary" onClick={() => nav(-1)}>Cancel</button>
          <button className="btn btn-primary" disabled={busy || !s.customerId}>{busy ? 'Saving…' : 'Save schedule'}</button>
        </StickyActions>
      </form>
    </Page>
  )
}
