import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, unwrap, type Schedule, type Schemas } from '../../lib/api/client'
import { formatDate, formatDateTime } from '../../lib/format'
import { useCurrencies } from '../../lib/lookups'
import { useAsync } from '../../lib/useAsync'
import { LineEditor, newLine, type EditLine } from '../../components/LineEditor'
import { Badge, Empty, ErrorBanner, Field, Page, Spinner, StickyActions } from '../../components/ui'

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
                <div className="text-sm text-slate-500">{name(s.customerId)} · {describe(s.recurrence)}</div>
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
        <div>{s.autoSend ? 'Invoices are e-mailed automatically.' : 'Invoices are issued but not e-mailed.'}</div>
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
        <h2 className="mb-2 font-semibold">Invoices created</h2>
        {data.runs.length === 0 ? <p className="text-slate-500">None yet.</p> : (
          <ul className="space-y-1">{data.runs.map((r) => <li key={r.period}><Link className="text-brand-600 underline" to={`/invoices/${r.invoiceId}`}>Period {formatDate(r.period)}</Link></li>)}</ul>
        )}
      </section>
    </Page>
  )
}

const blankRecurrence: Schemas['Recurrence'] = { frequency: 'monthly', interval: 1, dayOfMonth: 1, weekday: 1, sendTime: '09:00', startDate: new Date().toISOString().slice(0, 10) }

export function ScheduleEditor() {
  const { id } = useParams()
  const nav = useNavigate()
  const { minor } = useCurrencies()
  const customers = useAsync(() => unwrap(api.GET('/customers', { params: { query: { limit: 200 } } })).then((r) => r.items), [])
  const existing = useAsync(() => (id ? unwrap(api.GET('/schedules/{id}', { params: { path: { id } } })) : Promise.resolve(undefined)), [id])
  const [s, setS] = useState<Schedule>({ customerId: '', name: '', recurrence: blankRecurrence, lines: [], header: {}, notes: '', taxPercent: '0', autoSend: true, emailSubject: '', emailBody: '', version: 0 })
  const [lines, setLines] = useState<EditLine[]>([newLine()])
  const [preview, setPreview] = useState<string[]>([])
  const [error, setError] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    const x = existing.data?.schedule
    if (!x) return
    setS(x)
    setLines(x.lines.map((l) => ({ key: crypto.randomUUID(), productId: l.productId ?? '', description: l.description ?? '', quantity: l.quantity, unitPrice: l.unitPrice ?? '', discountPercent: l.discountPercent === '0' ? '' : l.discountPercent ?? '' })))
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

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    const body: Schedule = { ...s, lines: lines.map((l) => ({ productId: l.productId || undefined, description: l.description, quantity: l.quantity, unitPrice: l.unitPrice === '' ? undefined : l.unitPrice, discountPercent: l.discountPercent || '0' })) }
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
      <form onSubmit={submit} className="space-y-4">
        <ErrorBanner error={error} />
        <div className="card grid gap-4 md:grid-cols-2">
          <Field label="Name">{(f) => <input id={f} className="input" value={s.name} onChange={(e) => set({ name: e.target.value })} placeholder="Acme monthly" required />}</Field>
          <Field label="Customer" hint={customer ? `Billed in ${customer.currency}` : undefined}>
            {(f) => (
              <select id={f} className="input" value={s.customerId} onChange={(e) => set({ customerId: e.target.value })} required>
                <option value="">Choose…</option>
                {customers.data?.map((c) => <option key={c.id} value={c.id}>{c.name} ({c.currency})</option>)}
              </select>
            )}
          </Field>
        </div>
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
          <Field label="Send time">{(f) => <input id={f} type="time" className="input" value={r.sendTime} onChange={(e) => setR({ sendTime: e.target.value })} />}</Field>
          <Field label="Starts">{(f) => <input id={f} type="date" className="input" value={r.startDate} onChange={(e) => setR({ startDate: e.target.value })} />}</Field>
          <Field label="Ends (optional)">{(f) => <input id={f} type="date" className="input" value={r.endDate ?? ''} onChange={(e) => setR({ endDate: e.target.value || undefined })} />}</Field>
          <div className="md:col-span-3 text-sm text-slate-500">
            <span className="font-medium text-slate-700 dark:text-slate-300">Next runs: </span>
            {preview.length ? preview.slice(0, 4).map(formatDateTime).join(' · ') : '—'}
          </div>
        </div>
        {customer && <LineEditor lines={lines} onChange={setLines} currency={customer.currency} minorUnits={minor(customer.currency)} allowListPrice />}
        <div className="card space-y-4">
          <label className="flex min-h-[44px] items-center gap-3"><input type="checkbox" className="h-5 w-5" checked={s.autoSend} onChange={(e) => set({ autoSend: e.target.checked })} /> E-mail each invoice to the customer automatically</label>
          <Field label="E-mail subject" hint="Empty uses the default. Placeholders: {{invoice_number}} {{customer_name}} {{total}} {{due_date}} {{business_name}}">{(f) => <input id={f} className="input" value={s.emailSubject} onChange={(e) => set({ emailSubject: e.target.value })} />}</Field>
          <Field label="E-mail body">{(f) => <textarea id={f} className="input min-h-[96px]" value={s.emailBody} onChange={(e) => set({ emailBody: e.target.value })} />}</Field>
          <Field label="Notes on the invoice" hint="Empty uses the customer's notes.">{(f) => <textarea id={f} className="input" value={s.notes} onChange={(e) => set({ notes: e.target.value })} />}</Field>
        </div>
        <StickyActions>
          <button type="button" className="btn-secondary" onClick={() => nav(-1)}>Cancel</button>
          <button className="btn-primary" disabled={busy || !s.customerId}>{busy ? 'Saving…' : 'Save schedule'}</button>
        </StickyActions>
      </form>
    </Page>
  )
}
