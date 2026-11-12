import { useEffect, useState, type FormEvent } from 'react'
import { api, unwrap, type QuoteSettings } from '../../lib/api/client'
import { useAsync } from '../../lib/useAsync'
import { ErrorBanner, Field, Spinner } from '../../components/ui'
import { useCan } from '../../components/auth'
import { SettingsPage } from './Settings'

/** QuoteSettingsPage is /settings/quotes. */
export function QuoteSettingsPage() {
  return <SettingsPage><QuoteSettingsForm /></SettingsPage>
}

const placeholders = '{{quote_number}}, {{customer_name}}, {{attention}}, {{total}}, {{valid_until}}, {{date}}, {{business_name}}'

function QuoteSettingsForm() {
  const can = useCan()
  const { data, error, loading } = useAsync(() => unwrap(api.GET('/settings/quotes')), [])
  const [q, setQ] = useState<QuoteSettings>()
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState<Error | null>(null)
  // As on Business: the next number is sent only when changed, so a quote made meanwhile
  // does not make the save fail as "may only increase".
  const [loadedSeq, setLoadedSeq] = useState(0)
  useEffect(() => { if (data) { setQ(data); setLoadedSeq(data.nextSequence) } }, [data])
  if (!q) return error && !loading ? <ErrorBanner error={error} /> : <Spinner />
  const set = (patch: Partial<QuoteSettings>) => { setMsg(''); setQ({ ...q, ...patch }) }
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setErr(null)
    try {
      const out = await unwrap(api.PUT('/settings/quotes', { body: { ...q, nextSequence: q.nextSequence === loadedSeq ? 0 : q.nextSequence } }))
      setQ(out)
      setLoadedSeq(out.nextSequence)
      setMsg('Saved.')
    } catch (x) {
      setErr(x as Error)
    }
  }
  const editable = can('settings.quotes')
  return (
    <form onSubmit={submit} className="card card-body vstack gap-3">
      <div>
        <h2 className="fs-5 fw-semibold mb-1">Quotes</h2>
        <p className="small text-body-secondary mb-0">Defaults for new quotes. Each quote keeps its own validity date and terms once made.</p>
      </div>
      <ErrorBanner error={err ?? error} />
      <fieldset disabled={!editable} className="row row-cols-1 row-cols-md-2 g-3">
        <Field label="Quotes are valid for (days)" hint="A new quote is valid until its date plus this many days (1–366).">
          {(f) => <input id={f} type="number" min={1} max={366} inputMode="numeric" className="form-control" value={q.validDays} onChange={(e) => set({ validDays: Number(e.target.value) })} required />}
        </Field>
        <Field label="Quote number format" hint="{seq} sequence, {dd} day, {mm} month, {yy} or {yyyy} year — e.g. Q-{seq}/{yy} → Q-17/26">
          {(f) => <input id={f} className="form-control" value={q.numberFormat} onChange={(e) => set({ numberFormat: e.target.value })} required />}
        </Field>
        <Field label="Next quote sequence" hint="May only increase.">
          {(f) => <input id={f} type="number" inputMode="numeric" className="form-control" value={q.nextSequence} onChange={(e) => set({ nextSequence: Number(e.target.value) })} />}
        </Field>
        <Field label="Quote e-mail subject" hint={`Placeholders: ${placeholders}`}>
          {(f) => <input id={f} className="form-control" value={q.emailSubject} onChange={(e) => set({ emailSubject: e.target.value })} />}
        </Field>
        <div className="w-100">
          <Field label="Quote e-mail body" hint="The e-mail a quote is sent with; the quote PDF is attached. Empty restores the default text.">
            {(f) => <textarea id={f} className="form-control" style={{ minHeight: 140 }} value={q.emailBody} onChange={(e) => set({ emailBody: e.target.value })} />}
          </Field>
        </div>
        <div className="w-100">
          <Field label="Quote terms" hint="Printed on every new quote, e.g. what the price includes or the deposit asked for.">
            {(f) => <textarea id={f} className="form-control" style={{ minHeight: 96 }} value={q.terms} onChange={(e) => set({ terms: e.target.value })} />}
          </Field>
        </div>
      </fieldset>
      <div className="d-flex align-items-center justify-content-end gap-3">
        {msg && <span role="status" className="small text-success">{msg}</span>}
        {editable && <button className="btn btn-primary">Save quote settings</button>}
      </div>
    </form>
  )
}
