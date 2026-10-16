import { useEffect, useState, type FormEvent, type SVGProps } from 'react'
import { Can } from '../../components/Access'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, unwrap, type Schedule, type Schemas } from '../../lib/api/client'
import { formatDate, formatInZone, todayISO } from '../../lib/format'
import { useCurrencies } from '../../lib/lookups'
import { useAsync } from '../../lib/useAsync'
import { LineEditor, newLine, type EditLine } from '../../components/LineEditor'
import { Badge, ConfirmButton, Empty, ErrorBanner, Field, Page, Sheet, Spinner, StickyActions } from '../../components/ui'
import { IconBolt, IconEdit, IconPause, IconPlay, IconTrash } from '../../components/icons'
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
      <span className="block">{at}</span>
      <span className="block text-xs font-medium text-red-700" title={s.lastError}>Failed{s.failureCount ? ` (${s.failureCount}×)` : ''}{s.lastError ? `: ${s.lastError}` : ''}</span>
    </span>
  )
}

type Pending = { kind: 'run' | 'delete'; s: Schedule }

function IconAction({ icon: I, label, title, onClick, disabled, danger, to }: {
  icon: (p: SVGProps<SVGSVGElement>) => JSX.Element; label: string; title: string; onClick?: () => void; disabled?: boolean; danger?: boolean; to?: string
}) {
  const cls = `inline-flex h-11 w-11 items-center justify-center rounded-lg ring-1 transition disabled:cursor-not-allowed disabled:opacity-30 md:h-8 md:w-8 ${
    danger ? 'text-red-700 ring-red-200 hover:bg-red-50' : 'text-brand-800 ring-cream-300 hover:bg-cream-50'
  }`
  if (to) return <Link to={to} className={cls} aria-label={label} title={title}><I width={16} height={16} /></Link>
  return <button type="button" className={cls} aria-label={label} title={title} onClick={onClick} disabled={disabled}><I width={16} height={16} /></button>
}

/** ScheduleActions is the compact Edit / Pause or Resume / Run now / Delete bar for one schedule. */
function ScheduleActions({ s, busy, act, ask }: { s: Schedule; busy: boolean; act: (s: Schedule, what: 'pause' | 'resume') => void; ask: (p: Pending) => void }) {
  return (
    <Can need="schedules.edit">
    <div className="flex flex-nowrap items-center gap-1" role="group" aria-label={`Actions for ${s.name}`}>
      <IconAction icon={IconEdit} label={`Edit ${s.name}`} title="Edit" to={`/schedules/${s.id}/edit`} />
      {s.status === 'paused'
        ? <IconAction icon={IconPlay} label={`Resume ${s.name}`} title="Resume" onClick={() => act(s, 'resume')} disabled={busy} />
        : <IconAction icon={IconPause} label={`Pause ${s.name}`} title="Pause" onClick={() => act(s, 'pause')} disabled={busy || s.status !== 'active'} />}
      <IconAction icon={IconBolt} label={`Run ${s.name} now`} title="Run now" onClick={() => ask({ kind: 'run', s })} disabled={busy || s.status !== 'active'} />
      <IconAction icon={IconTrash} label={`Delete ${s.name}`} title="Delete" onClick={() => ask({ kind: 'delete', s })} disabled={busy} danger />
    </div>
    </Can>
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
  const nextRun = (s: Schedule) => (s.nextRunAt ? formatInZone(s.nextRunAt, s.recurrence.timeZone) : 'No upcoming run')
  return (
    <Page title="Schedules" actions={<Can need="schedules.edit"><Link to="/schedules/new" className="btn-primary">+ New</Link></Can>}>
      <p className="mb-3 text-sm text-slate-500">Recurring invoices are issued and e-mailed automatically.</p>
      <ErrorBanner error={error ?? actionError} />
      {notice && <div role="status" className="mb-3 rounded-xl border-l-4 border-brand-500 bg-white p-3 text-sm font-medium text-brand-900 shadow-sm">{notice}</div>}
      {loading && !data ? <Spinner /> : items.length === 0 ? <Empty>No schedules yet.</Empty> : (
        <>
          <ul className="grid gap-2 md:hidden" aria-label="Schedules">
            {items.map((s) => (
              <li key={s.id} className="card space-y-2 text-sm">
                <div className="flex items-center justify-between gap-2">
                  <Link to={`/schedules/${s.id}`} className="font-semibold text-brand-900 underline-offset-2 hover:underline">{s.name}</Link>
                  <Badge status={s.status ?? 'active'} />
                </div>
                <div className="text-slate-600">{name(s.customerId)} · {typeLabel(s)}</div>
                <div className="text-slate-500">{describe(s.recurrence)}</div>
                <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
                  <dt className="text-slate-500">Last run</dt><dd><LastRun s={s} /></dd>
                  <dt className="text-slate-500">Next run</dt><dd>{nextRun(s)}</dd>
                </dl>
                <ScheduleActions s={s} busy={busy === s.id} act={act} ask={setPending} />
              </li>
            ))}
          </ul>
          <div className="card hidden overflow-x-auto p-0 md:block">
            <table className="w-full text-left text-sm">
              <thead className="table-head">
                <tr>
                  <th className="px-4 py-3">Name</th>
                  <th className="px-4 py-3">Customer</th>
                  <th className="px-4 py-3">Type</th>
                  <th className="px-4 py-3">Repeats</th>
                  <th className="px-4 py-3">Last run</th>
                  <th className="px-4 py-3">Next run</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3 text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-cream-200">
                {items.map((s) => (
                  <tr key={s.id} className="align-top">
                    <td className="px-4 py-2.5"><Link to={`/schedules/${s.id}`} className="font-semibold text-brand-900 underline-offset-2 hover:underline">{s.name}</Link></td>
                    <td className="px-4 py-2.5">{name(s.customerId)}</td>
                    <td className="px-4 py-2.5">{typeLabel(s)}</td>
                    <td className="px-4 py-2.5">{describe(s.recurrence)}</td>
                    <td className="px-4 py-2.5"><LastRun s={s} /></td>
                    <td className="px-4 py-2.5">{nextRun(s)}</td>
                    <td className="px-4 py-2.5"><Badge status={s.status ?? 'active'} /></td>
                    <td className="px-4 py-2.5"><div className="flex justify-end"><ScheduleActions s={s} busy={busy === s.id} act={act} ask={setPending} /></div></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      <Sheet title={pending?.kind === 'run' ? 'Run schedule now?' : 'Delete schedule?'} open={pending !== null} onClose={() => setPending(null)}>
        {pending && (
          <div className="space-y-4 text-sm">
            <p>
              {pending.kind === 'run'
                ? pending.s.kind === 'email'
                  ? `“${pending.s.name}” e-mails ${name(pending.s.customerId) || 'the customer'} for real within a minute.`
                  : `“${pending.s.name}” raises a real invoice${pending.s.autoSend ? ' and e-mails it' : ''} within a minute.`
                : `“${pending.s.name}” is deleted. Invoices it created are kept.`}
            </p>
            <div className="flex justify-end gap-2">
              <button type="button" className="btn-secondary" onClick={() => setPending(null)}>Cancel</button>
              <button type="button" className={pending.kind === 'run' ? 'btn-primary' : 'btn-danger'} onClick={confirm}>{pending.kind === 'run' ? 'Run now' : 'Delete'}</button>
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
      <div className="card mb-4 space-y-1 text-sm">
        <div className="flex items-center gap-2"><Badge status={s.status ?? 'active'} /> <span>{describe(s.recurrence)} ({s.recurrence.timeZone})</span></div>
        <div className="font-medium text-brand-900">{purposeLabel(s)}</div>
        {s.lastError && <div className="text-red-600">Last error ({s.failureCount}×): {s.lastError}</div>}
      </div>
      <Can need="schedules.edit">
      <div className="mb-4 grid grid-cols-2 gap-2 md:flex">
        <Link to={`/schedules/${id}/edit`} className="btn-secondary">Edit</Link>
        {s.status === 'active' && <button className="btn-secondary" onClick={() => act(api.POST('/schedules/{id}/pause', path))}>Pause</button>}
        {s.status === 'paused' && <button className="btn-primary" onClick={() => act(api.POST('/schedules/{id}/resume', path))}>Resume</button>}
        {s.status === 'active' && (
          <ConfirmButton
            label="Run now"
            danger={false}
            title={`Run ${s.name} now?`}
            message="It runs for real within a minute: it raises the invoice or e-mails the customer as it would on its next run."
            confirmLabel="Run now"
            onConfirm={() => act(api.POST('/schedules/{id}/run-now', path))}
          />
        )}
        <ConfirmButton
          label="Delete"
          title={`Delete ${s.name}?`}
          message="The schedule stops and is deleted. Invoices and e-mails it already made stay. This cannot be undone."
          confirmLabel="Delete schedule"
          onConfirm={async () => { try { await unwrap(api.DELETE('/schedules/{id}', path)); nav('/schedules') } catch (e) { setActionError(e as Error) } }}
        />
      </div>
      </Can>
      <section className="card mb-4 text-sm">
        <h2 className="mb-2 font-semibold">Upcoming runs</h2>
        {data.upcoming.length === 0 ? <p className="text-slate-500">None.</p> : <ul className="space-y-1">{data.upcoming.map((t) => <li key={t}>{formatInZone(t, s.recurrence.timeZone)}</li>)}</ul>}
      </section>
      <section className="card text-sm">
        <h2 className="mb-2 font-semibold">Runs</h2>
        {data.runs.length === 0 ? <p className="text-slate-500">None yet.</p> : (
          <ul className="space-y-1">{data.runs.map((r) => <li key={r.period}>{r.invoiceId ? <Link className="text-brand-600 underline" to={`/invoices/${r.invoiceId}`}>Invoice for {formatDate(r.period)}</Link> : <span>E-mails sent for {formatDate(r.period)}</span>}</li>)}</ul>
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
    <div data-testid="schedule-disclaimer" className="rounded-xl bg-cream-50 p-3 text-sm ring-1 ring-cream-200">
      <p className="font-medium text-brand-900">Disclaimer printed on every invoice — {own ? "this customer's own" : 'the business disclaimer'}:</p>
      <p className="mt-1 whitespace-pre-line text-slate-700">{text || 'None set.'}</p>
      <Link className="mt-1 inline-block text-brand-700 underline" to={own ? `/customers/${customer.id}` : '/settings/business'}>
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
      api.POST('/schedules/preview', { body: s.recurrence }).then(({ data }) => setPreview(data ?? null))
    }, 300)
    return () => clearTimeout(t)
  }, [s.recurrence])
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
      <form onSubmit={submit} className="space-y-5">
        <ErrorBanner error={error} />
        <div className="card grid gap-4 md:grid-cols-2">
          <Field label="Customer" hint={customer ? `Billed in ${customer.currency}` : undefined}>
            {(f) => (
              <select id={f} className="input" value={s.customerId} onChange={(e) => set({ customerId: e.target.value })} required>
                <option value="">Choose…</option>
                {customers.data?.map((c) => <option key={c.id} value={c.id}>{c.name} ({c.currency})</option>)}
              </select>
            )}
          </Field>
          <Field label="Name">{(f) => <input id={f} className="input" value={s.name} onChange={(e) => set({ name: e.target.value })} placeholder="e.g. Monthly subscription, or Weekly reminder" required />}</Field>
        </div>
        <fieldset className="card">
          <legend className="sr-only">What should this schedule do?</legend>
          <h2 className="mb-3 text-lg font-semibold">What should this schedule do?</h2>
          <div className="grid gap-2 md:grid-cols-2">
            {purposes.map((p) => (
              <label key={p.key} className={`flex cursor-pointer gap-3 rounded-xl p-3 ring-1 ${purpose === p.key ? 'bg-brand-50 ring-brand-400' : 'ring-cream-300 hover:bg-cream-50'}`}>
                <input type="radio" name="purpose" className="mt-1" checked={purpose === p.key} onChange={() => setPurpose(p.key)} />
                <span>
                  <span className="block font-semibold text-brand-900">{p.title}</span>
                  <span className="text-sm text-slate-600">{p.hint}</span>
                </span>
              </label>
            ))}
          </div>
        </fieldset>
        <div className="card grid gap-4 md:grid-cols-3">
          <Field label="Repeats">
            {(f) => (
              <select id={f} className="input" value={r.frequency} onChange={(e) => setR({ frequency: e.target.value as Schemas['Recurrence']['frequency'] })}>
                <option value="once">Once</option><option value="weekly">Weekly</option><option value="monthly">Monthly</option><option value="quarterly">Quarterly</option><option value="yearly">Yearly</option>
              </select>
            )}
          </Field>
          {r.frequency !== 'once' && <Field label="Every">{(f) => <input id={f} type="number" min={1} max={52} inputMode="numeric" className="input" value={r.interval} onChange={(e) => setR({ interval: Number(e.target.value) })} />}</Field>}
          {r.frequency === 'weekly' && (
            <Field label="On">{(f) => <select id={f} className="input" value={r.weekday} onChange={(e) => setR({ weekday: Number(e.target.value) })}>{weekdays.map((w, i) => <option key={w} value={i}>{w}</option>)}</select>}</Field>
          )}
          {['monthly', 'quarterly', 'yearly'].includes(r.frequency) && (
            <>
              <Field label="On">
                {(f) => (
                  <select id={f} className="input" value={dayMode(r.dayOfMonth)} onChange={(e) => {
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
                  {(f) => <input id={f} type="number" min={2} max={31} inputMode="numeric" className="input" value={r.dayOfMonth} onChange={(e) => setR({ dayOfMonth: Math.min(31, Math.max(2, Number(e.target.value) || 2)) })} />}
                </Field>
              )}
              <Field label="If it falls on a weekend" hint="A run moved to the Friday before is still for its own month.">
                {(f) => (
                  <select id={f} className="input" value={r.weekendRule ?? preview?.weekendRule ?? ''} onChange={(e) => setR({ weekendRule: e.target.value as Schemas['Recurrence']['weekendRule'] })}>
                    {!(r.weekendRule ?? preview?.weekendRule) && <option value="" disabled>…</option>}
                    <option value="previous-friday">Send on the Friday before</option>
                    <option value="none">Send on that day</option>
                  </select>
                )}
              </Field>
            </>
          )}
          <Field label="Time" hint={preview?.timeZone ? `${preview.timeZone} time` : undefined}>{(f) => <input id={f} type="time" className="input" value={r.sendTime} onChange={(e) => setR({ sendTime: e.target.value })} />}</Field>
          <Field label="Starts">{(f) => <input id={f} type="date" className="input" value={r.startDate} onChange={(e) => setR({ startDate: e.target.value })} />}</Field>
          <Field label="Ends (optional)">{(f) => <input id={f} type="date" className="input" value={r.endDate ?? ''} onChange={(e) => setR({ endDate: e.target.value || undefined })} />}</Field>
          <div className="text-sm text-slate-500 md:col-span-3">
            <span className="font-semibold text-brand-900">Next runs: </span>
            {preview?.runs.length ? preview.runs.slice(0, 4).map((t) => formatInZone(t, preview.timeZone)).join(' · ') : '—'}
          </div>
        </div>
        {isInvoice && (
          <h2 className="pt-1 text-lg font-semibold">Step 1 · Raise the invoice</h2>
        )}
        {isInvoice && customer && <LineEditor lines={lines} onChange={setLines} currency={customer.currency} minorUnits={minor(customer.currency)} />}
        {isInvoice && (
          <section className="card space-y-4">
            <h2 className="text-lg font-semibold">Step 2 · E-mail it</h2>
            <div className="grid gap-2 md:grid-cols-3" role="radiogroup" aria-label="E-mail the raised invoice">
              {[
                { key: 'none', label: "Don't e-mail", hint: 'Raise only; send it yourself later.' },
                { key: 'now', label: 'Immediately', hint: 'E-mail as soon as it is raised.' },
                { key: 'later', label: 'Some days later', hint: 'E-mail a set number of days after raising.' },
              ].map((o) => {
                const mode = !s.autoSend ? 'none' : s.emailDelayDays > 0 ? 'later' : 'now'
                return (
                  <label key={o.key} className={`flex cursor-pointer gap-3 rounded-xl p-3 ring-1 ${mode === o.key ? 'bg-brand-50 ring-brand-400' : 'ring-cream-300 hover:bg-cream-50'}`}>
                    <input
                      type="radio"
                      name="emailmode"
                      className="mt-1"
                      checked={mode === o.key}
                      onChange={() => set(o.key === 'none' ? { autoSend: false, emailDelayDays: 0 } : o.key === 'now' ? { autoSend: true, emailDelayDays: 0 } : { autoSend: true, emailDelayDays: Math.max(s.emailDelayDays, 2) })}
                    />
                    <span>
                      <span className="block font-semibold text-brand-900">{o.label}</span>
                      <span className="text-sm text-slate-600">{o.hint}</span>
                    </span>
                  </label>
                )
              })}
            </div>
            {s.autoSend && s.emailDelayDays > 0 && (
              <Field label="Days after raising">
                {(f) => <input id={f} type="number" min={1} max={90} inputMode="numeric" className="input md:w-40" value={s.emailDelayDays} onChange={(e) => set({ emailDelayDays: Math.min(90, Math.max(1, Number(e.target.value) || 1)) })} />}
              </Field>
            )}
            {preview && preview.runs.length > 0 && (
              <div>
                <h3 className="mb-2 text-sm font-semibold text-brand-900">Timeline</h3>
                <ol className="space-y-1 text-sm">
                  {preview.runs.slice(0, 3).map((t) => {
                    const mail = new Date(new Date(t).getTime() + s.emailDelayDays * 86400000).toISOString()
                    return (
                      <li key={t} className="flex flex-wrap items-center gap-2">
                        <span className="chip bg-brand-50 text-brand-800 ring-1 ring-brand-200">Raise</span> {formatInZone(t, preview.timeZone)}
                        {s.autoSend && <><span aria-hidden="true">→</span><span className="chip bg-cream-200 text-brand-900">E-mail</span> {formatInZone(mail, preview.timeZone)}</>}
                      </li>
                    )
                  })}
                </ol>
              </div>
            )}
          </section>
        )}
        <div className="card space-y-4">
          {isInvoice && (
            <>
              <Field label="VAT %" hint="Empty uses the business default VAT. 0 = no VAT.">{(f) => <input id={f} className="input" inputMode="decimal" placeholder="Business default" value={s.vatPercent ?? ''} onChange={(e) => set({ vatPercent: e.target.value === '' ? undefined : e.target.value })} />}</Field>
              <Field label="Invoice notes" hint="Optional. The disclaimer is added automatically.">{(f) => <textarea id={f} className="input" value={s.notes} onChange={(e) => set({ notes: e.target.value })} />}</Field>
              {customer && <DisclaimerNote customer={customer} businessText={business.data?.defaultNotes} />}
            </>
          )}
          <Field label="E-mail subject" hint={`Empty uses the default.${template ? ` Placeholders: ${template.placeholders.join(' ')}` : ''}`}>
            {(f) => <input id={f} className="input" value={s.emailSubject} onChange={(e) => { setEdited((x) => ({ ...x, subject: true })); set({ emailSubject: e.target.value }) }} />}
          </Field>
          <Field label="E-mail body">
            {(f) => <textarea id={f} className="input min-h-[160px]" value={s.emailBody} onChange={(e) => { setEdited((x) => ({ ...x, body: true })); set({ emailBody: e.target.value }) }} />}
          </Field>
        </div>
        <StickyActions>
          <button type="button" className="btn-secondary" onClick={() => nav(-1)}>Cancel</button>
          <button className="btn-primary" disabled={busy || !s.customerId}>{busy ? 'Saving…' : 'Save schedule'}</button>
        </StickyActions>
      </form>
    </Page>
  )
}
