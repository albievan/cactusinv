import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, unwrap, type Schedule, type Schemas } from '../../lib/api/client'
import { formatDate, formatDateTime } from '../../lib/format'
import { useCurrencies } from '../../lib/lookups'
import { useAsync } from '../../lib/useAsync'
import { LineEditor, newLine, type EditLine } from '../../components/LineEditor'
import { Badge, Empty, ErrorBanner, Field, Page, Spinner, StickyActions } from '../../components/ui'
import { uuid } from '../../lib/uuid'

const weekdays = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

export function describe(r: Schemas['Recurrence']): string {
  const every = r.interval > 1 ? `every ${r.interval} ` : 'every '
  const day = r.dayOfMonth === 0 ? 'last day' : `day ${r.dayOfMonth}`
  switch (r.frequency) {
    case 'once': return `once on ${formatDate(r.startDate)} at ${r.sendTime}`
    case 'weekly': return `${every}${r.interval > 1 ? 'weeks' : 'week'} on ${weekdays[r.weekday]} at ${r.sendTime}`
    case 'monthly': return `${every}${r.interval > 1 ? 'months' : 'month'} on ${day} at ${r.sendTime}`
    case 'quarterly': return `${every}${r.interval > 1 ? 'quarters' : 'quarter'} on ${day} at ${r.sendTime}`
    default: return `${every}${r.interval > 1 ? 'years' : 'year'} on ${day} at ${r.sendTime}`
  }
}

export function purposeLabel(s: Schedule): string {
  if (s.kind !== 'email') {
    if (!s.autoSend) return 'Raises a recurring invoice'
    return s.emailDelayDays > 0 ? `Raises a recurring invoice, e-mails it ${s.emailDelayDays} day${s.emailDelayDays === 1 ? '' : 's'} later` : 'Raises and e-mails a recurring invoice'
  }
  return s.emailScope === 'statement' ? 'E-mails a statement' : s.emailScope === 'outstanding' ? 'E-mails outstanding invoices' : 'E-mails unsent invoices'
}

export function ScheduleList() {
  const { data, error, loading } = useAsync(() => unwrap(api.GET('/schedules', { params: { query: { limit: 200 } } })), [])
  const customers = useAsync(() => unwrap(api.GET('/customers', { params: { query: { limit: 200, includeInactive: true } } })).then((r) => r.items), [])
  const name = (id: string) => customers.data?.find((c) => c.id === id)?.name ?? ''
  return (
    <Page title="Schedules" actions={<Link to="/schedules/new" className="btn-primary">+ New</Link>}>
      <p className="mb-3 text-sm text-slate-500">Recurring invoices are issued and e-mailed automatically.</p>
      <ErrorBanner error={error} />
      {loading && !data ? <Spinner /> : data?.items.length === 0 ? <Empty>No schedules yet.</Empty> : (
        <ul className="grid gap-2 md:grid-cols-2">
          {data?.items.map((s) => (
            <li key={s.id}>
              <Link to={`/schedules/${s.id}`} className="card block">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-semibold">{s.name}</span>
                  <Badge status={s.status ?? 'active'} />
                </div>
                <div className="text-sm text-slate-500">{name(s.customerId)} · {purposeLabel(s)} · {describe(s.recurrence)}</div>
                <div className="mt-1 text-sm">{s.nextRunAt ? `Next: ${formatDateTime(s.nextRunAt)}` : 'No upcoming run'}{s.autoSend ? ' · auto-send' : ''}</div>
                {s.lastError && <div className="mt-1 text-xs text-red-600">Last error: {s.lastError}</div>}
              </Link>
            </li>
          ))}
        </ul>
      )}
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
      <div className="mb-4 grid grid-cols-2 gap-2 md:flex">
        <Link to={`/schedules/${id}/edit`} className="btn-secondary">Edit</Link>
        {s.status === 'active' && <button className="btn-secondary" onClick={() => act(api.POST('/schedules/{id}/pause', path))}>Pause</button>}
        {s.status === 'paused' && <button className="btn-primary" onClick={() => act(api.POST('/schedules/{id}/resume', path))}>Resume</button>}
        {s.status === 'active' && <button className="btn-primary" onClick={() => act(api.POST('/schedules/{id}/run-now', path))}>Run now</button>}
        <button className="btn-danger" onClick={async () => { try { await unwrap(api.DELETE('/schedules/{id}', path)); nav('/schedules') } catch (e) { setActionError(e as Error) } }}>Delete</button>
      </div>
      <section className="card mb-4 text-sm">
        <h2 className="mb-2 font-semibold">Upcoming runs</h2>
        {data.upcoming.length === 0 ? <p className="text-slate-500">None.</p> : <ul className="space-y-1">{data.upcoming.map((t) => <li key={t}>{formatDateTime(t)}</li>)}</ul>}
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

const blankRecurrence: Schemas['Recurrence'] = { frequency: 'monthly', interval: 1, dayOfMonth: 1, weekday: 1, sendTime: '09:00', startDate: new Date().toISOString().slice(0, 10) }

type Purpose = 'invoice' | 'unsent' | 'outstanding' | 'statement'

const purposes: { key: Purpose; title: string; hint: string }[] = [
  { key: 'invoice', title: 'Recurring invoice: raise, then e-mail', hint: 'Raises a predefined invoice from the products below on every run, then e-mails it straight away or a few days later.' },
  { key: 'unsent', title: 'E-mail invoices not yet e-mailed', hint: "Sends each of the customer's issued invoices that has never been e-mailed." },
  { key: 'outstanding', title: 'E-mail all outstanding invoices', hint: 'Sends every unpaid invoice (issued, e-mailed or overdue) again.' },
  { key: 'statement', title: 'E-mail a statement', hint: 'Sends one statement of account listing everything unpaid, with aging.' },
]

export function ScheduleEditor() {
  const { id } = useParams()
  const [params] = useSearchParams()
  const nav = useNavigate()
  const { minor } = useCurrencies()
  const customers = useAsync(() => unwrap(api.GET('/customers', { params: { query: { limit: 200 } } })).then((r) => r.items), [])
  const existing = useAsync(() => (id ? unwrap(api.GET('/schedules/{id}', { params: { path: { id } } })) : Promise.resolve(undefined)), [id])
  const [s, setS] = useState<Schedule>({
    customerId: params.get('customer') ?? '', name: '', kind: 'invoice', recurrence: blankRecurrence, lines: [], header: {}, notes: '',
    autoSend: false, emailDelayDays: 0, emailSubject: '', emailBody: '', version: 0,
  })
  const [purpose, setPurpose] = useState<Purpose>(params.get('kind') === 'statement' ? 'statement' : params.get('kind') === 'email' ? 'unsent' : 'invoice')
  const [lines, setLines] = useState<EditLine[]>([newLine()])
  const [preview, setPreview] = useState<string[]>([])
  const [error, setError] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    const x = existing.data?.schedule
    if (!x) return
    setS(x)
    setPurpose(x.kind === 'email' ? ((x.emailScope ?? 'unsent') as Purpose) : 'invoice')
    setLines(x.lines.map((l) => ({ key: uuid(), productId: l.productId, productName: l.description ?? '', description: l.description ?? '', quantity: l.quantity, unitPrice: l.unitPrice ?? '', discountPercent: l.discountPercent === '0' ? '' : l.discountPercent ?? '' })))
  }, [existing.data])
  useEffect(() => {
    const t = setTimeout(() => {
      api.POST('/schedules/preview', { body: s.recurrence }).then(({ data }) => setPreview(data?.runs ?? []))
    }, 300)
    return () => clearTimeout(t)
  }, [s.recurrence])
  const set = (patch: Partial<Schedule>) => setS({ ...s, ...patch })
  const setR = (patch: Partial<Schemas['Recurrence']>) => setS({ ...s, recurrence: { ...s.recurrence, ...patch } })
  const customer = customers.data?.find((c) => c.id === s.customerId)
  const r = s.recurrence
  const isInvoice = purpose === 'invoice'

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (isInvoice && lines.some((l) => !l.productId)) {
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
      lines: isInvoice ? lines.map((l) => ({ productId: l.productId, quantity: l.quantity, discountPercent: l.discountPercent || '0' })) : [],
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
            <Field label="Day of month" hint="0 = last day">{(f) => <input id={f} type="number" min={0} max={31} inputMode="numeric" className="input" value={r.dayOfMonth} onChange={(e) => setR({ dayOfMonth: Number(e.target.value) })} />}</Field>
          )}
          <Field label="Time">{(f) => <input id={f} type="time" className="input" value={r.sendTime} onChange={(e) => setR({ sendTime: e.target.value })} />}</Field>
          <Field label="Starts">{(f) => <input id={f} type="date" className="input" value={r.startDate} onChange={(e) => setR({ startDate: e.target.value })} />}</Field>
          <Field label="Ends (optional)">{(f) => <input id={f} type="date" className="input" value={r.endDate ?? ''} onChange={(e) => setR({ endDate: e.target.value || undefined })} />}</Field>
          <div className="text-sm text-slate-500 md:col-span-3">
            <span className="font-semibold text-brand-900">Next runs: </span>
            {preview.length ? preview.slice(0, 4).map(formatDateTime).join(' · ') : '—'}
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
            {preview.length > 0 && (
              <div>
                <h3 className="mb-2 text-sm font-semibold text-brand-900">Timeline</h3>
                <ol className="space-y-1 text-sm">
                  {preview.slice(0, 3).map((t) => {
                    const mail = new Date(new Date(t).getTime() + s.emailDelayDays * 86400000).toISOString()
                    return (
                      <li key={t} className="flex flex-wrap items-center gap-2">
                        <span className="chip bg-brand-50 text-brand-800 ring-1 ring-brand-200">Raise</span> {formatDateTime(t)}
                        {s.autoSend && <><span aria-hidden="true">→</span><span className="chip bg-cream-200 text-brand-900">E-mail</span> {formatDateTime(mail)}</>}
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
            </>
          )}
          <Field label="E-mail subject" hint="Empty uses the default. Placeholders: {{invoice_number}} {{customer_name}} {{total}} {{due_date}} {{business_name}}">{(f) => <input id={f} className="input" value={s.emailSubject} onChange={(e) => set({ emailSubject: e.target.value })} />}</Field>
          <Field label="E-mail body">{(f) => <textarea id={f} className="input min-h-[96px]" value={s.emailBody} onChange={(e) => set({ emailBody: e.target.value })} />}</Field>
        </div>
        <StickyActions>
          <button type="button" className="btn-secondary" onClick={() => nav(-1)}>Cancel</button>
          <button className="btn-primary" disabled={busy || !s.customerId}>{busy ? 'Saving…' : 'Save schedule'}</button>
        </StickyActions>
      </form>
    </Page>
  )
}
