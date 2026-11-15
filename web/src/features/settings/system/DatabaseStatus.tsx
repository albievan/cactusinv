import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from 'react'
import { api, unwrap, type Schemas } from '../../../lib/api/client'
import { formatDateTime } from '../../../lib/format'
import { useAsync } from '../../../lib/useAsync'
import { useCan } from '../../../components/auth'
import { ErrorBanner, Field, Sheet, Spinner } from '../../../components/ui'
import { SettingsPage } from '../Settings'
import { formatBytes, formatCount, formatMs, formatUptime, stateBadge } from './systemFormat'

type Status = Schemas['SystemStatus']
type Setting = Schemas['Setting']
type Secret = Schemas['SecretInfo']

const targetLabels: Record<string, string> = { dockerdesktop: 'Docker', podman: 'Podman', k3s: 'k3s', k8s: 'Kubernetes' }

/** How long a revealed secret stays on screen. */
const SHOW_SECRET_MS = 60_000

/**
 * DatabaseStatusPage is /settings/database (M57): the containers and services the app
 * reaches, the database's settings and status, the application, its configuration and its
 * secrets (each value only on request, after the viewer's own password).
 */
export function DatabaseStatusPage() {
  return (
    <SettingsPage>
      <DatabaseStatus />
    </SettingsPage>
  )
}

function DatabaseStatus() {
  const status = useAsync(() => unwrap(api.GET('/system/status')), [])
  if (status.loading && !status.data) return <Spinner />
  const st = status.data
  return (
    <section className="vstack gap-3" aria-labelledby="database-heading">
      <div className="d-flex flex-wrap align-items-center justify-content-between gap-2">
        <h2 id="database-heading" className="fs-5 fw-bold mb-0">Database & containers</h2>
        <button type="button" className="btn btn-sm btn-outline-primary" onClick={status.reload} disabled={status.loading}>
          <i className="bi bi-arrow-clockwise" aria-hidden="true" /> {status.loading ? 'Refreshing…' : 'Refresh'}
        </button>
      </div>
      <p className="small text-body-secondary mb-0">
        Seen from inside the cactusInv container: the app reaches the database and the mail servers over the network; it has no rights over
        Docker or the cluster itself.
      </p>
      <ErrorBanner error={status.error} />
      {st && (
        <>
          <Services st={st} />
          <Database st={st} />
          <Application st={st} />
          <Secrets secrets={st.secrets} />
          <SettingsTable title="Application configuration" note="The environment the app was started with (secrets are not shown here)." rows={st.configuration} />
          <SettingsTable title={`All MariaDB variables (${st.database.variables.length})`} rows={st.database.variables} collapsed />
          <SettingsTable title={`All MariaDB status values (${st.database.status.length})`} rows={st.database.status} collapsed />
        </>
      )}
    </section>
  )
}

function StateBadge({ state }: { state: string }) {
  const b = stateBadge(state)
  return (
    <span className={`badge rounded-pill border d-inline-flex align-items-center gap-1 ${b.cls}`}>
      <i className={`bi ${b.icon}`} aria-hidden="true" />
      {b.label}
    </span>
  )
}

function Card({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="card card-body vstack gap-2" aria-label={title}>
      <h3 className="fs-6 fw-semibold mb-0">{title}</h3>
      {children}
    </section>
  )
}

function Services({ st }: { st: Status }) {
  return (
    <Card title="Containers and services">
      <ul className="list-unstyled vstack gap-2 mb-0">
        {st.services.map((s) => (
          <li key={s.name} className="d-flex flex-wrap align-items-start gap-2 border-top pt-2">
            <div className="flex-grow-1" style={{ minWidth: 0 }}>
              <div className="fw-semibold">{s.name} <span className="small fw-normal text-body-secondary">· {s.role}</span></div>
              <div className="small text-body-secondary text-break">{s.detail}{s.state === 'running' && s.latencyMs > 0 ? ` (${formatMs(s.latencyMs)})` : ''}</div>
            </div>
            <StateBadge state={s.state} />
          </li>
        ))}
      </ul>
    </Card>
  )
}

/** Facts is a two-column list of labelled values. */
function Facts({ rows }: { rows: [string, ReactNode][] }) {
  return (
    <dl className="row small mb-0">
      {rows.map(([k, v]) => (
        <div key={k} className="col-12 col-md-6 d-flex gap-2 py-1 border-top">
          <dt className="fw-normal text-body-secondary" style={{ minWidth: '45%' }}>{k}</dt>
          <dd className="mb-0 text-break" style={{ fontVariantNumeric: 'tabular-nums' }}>{v}</dd>
        </div>
      ))}
    </dl>
  )
}

function Database({ st }: { st: Status }) {
  const d = st.database
  if (d.error) {
    return (
      <Card title="Database">
        <p role="alert" className="small fw-medium text-danger mb-0">MariaDB at {d.host}:{d.port}: {d.error}.</p>
      </Card>
    )
  }
  return (
    <Card title="Database">
      <Facts rows={[
        ['Status', <StateBadge key="s" state="running" />],
        ['Server', `${d.version}${d.versionComment ? ` (${d.versionComment})` : ''}`],
        ['Address', `${d.host}:${d.port}`],
        ['Database', d.name],
        ['User', d.user],
        ['Size', `${formatBytes(d.sizeBytes)} in ${d.tables} tables`],
        ['Up for', formatUptime(d.uptimeSeconds)],
        ['Answers in', formatMs(d.pingMs)],
        ['Connections', `${d.connections} of ${d.maxConnections} (most at once: ${d.maxUsedConnections})`],
        ['Queries', `${formatCount(d.queries)} (slow: ${formatCount(d.slowQueries)})`],
        ['Character set', `${d.charset} / ${d.collation}`],
        ['Time zone', d.timeZone],
        ['App connection pool', `${d.pool.open} open, ${d.pool.inUse} in use, ${d.pool.idle} idle${d.pool.maxOpen ? ` (at most ${d.pool.maxOpen})` : ''}`],
        ['Waited for a connection', `${formatCount(d.pool.waitCount)} times, ${formatMs(d.pool.waitMs)} in all`],
      ]} />
    </Card>
  )
}

function Application({ st }: { st: Status }) {
  const r = st.runtime
  return (
    <Card title="Application">
      <Facts rows={[
        ['Version', `${st.version} (build ${st.build})`],
        ['Installed with', st.target ? `${targetLabels[st.target] ?? st.target} (deploy.sh)` : st.container ? 'a container' : 'the program, run by hand'],
        ['Host', r.hostname || '—'],
        ['Started', r.startedAt ? `${formatDateTime(r.startedAt)} (up ${formatUptime(st.uptimeSeconds)})` : '—'],
        ['System', `${r.os}/${r.arch}, ${r.cpus} CPUs, ${r.goVersion}`],
        ['Memory', `${formatBytes(r.heapBytes)} in use, ${formatBytes(r.sysBytes)} from the system`],
        ['Goroutines', formatCount(r.goroutines)],
      ]} />
    </Card>
  )
}

function Secrets({ secrets }: { secrets: Secret[] }) {
  const can = useCan()
  const viewer = can('settings.secrets.view')
  const [asking, setAsking] = useState<Secret | null>(null)
  const [shown, setShown] = useState<{ key: string; value: string } | null>(null)
  useEffect(() => {
    if (!shown) return
    const t = window.setTimeout(() => setShown(null), SHOW_SECRET_MS)
    return () => window.clearTimeout(t)
  }, [shown])
  return (
    <Card title="Secrets">
      <p className="small text-body-secondary mb-0">
        {viewer
          ? 'The secrets this container holds. View asks for your password again; the value is hidden after a minute and each view is recorded in the audit log.'
          : 'The secrets this container holds. Only Administrators and members of Admin settings can see their values.'}
      </p>
      <ul className="list-unstyled vstack gap-2 mb-0">
        {secrets.map((s) => (
          <li key={s.key} className="border-top pt-2 vstack gap-2">
            <div className="d-flex flex-wrap align-items-start gap-2">
              <div className="flex-grow-1" style={{ minWidth: 0 }}>
                <div className="font-monospace fw-semibold small">{s.key}</div>
                <div className="small text-body-secondary">{s.description}</div>
              </div>
              {!s.present ? (
                <span className="badge rounded-pill border bg-secondary-subtle text-secondary-emphasis border-secondary-subtle">Not set</span>
              ) : viewer && (
                shown?.key === s.key ? (
                  <button type="button" className="btn btn-sm btn-outline-primary" onClick={() => setShown(null)} aria-label={`Hide ${s.key}`}>
                    <i className="bi bi-eye-slash" aria-hidden="true" /> Hide
                  </button>
                ) : (
                  <button type="button" className="btn btn-sm btn-outline-primary" onClick={() => setAsking(s)} aria-label={`View ${s.key}`}>
                    <i className="bi bi-eye" aria-hidden="true" /> View
                  </button>
                )
              )}
            </div>
            {shown?.key === s.key && <SecretValue name={s.key} value={shown.value} />}
          </li>
        ))}
      </ul>
      {asking && (
        <RevealSheet
          secret={asking}
          onClose={() => setAsking(null)}
          onShown={(value) => {
            setShown({ key: asking.key, value })
            setAsking(null)
          }}
        />
      )}
    </Card>
  )
}

function SecretValue({ name, value }: { name: string; value: string }) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }
  return (
    <div className="d-flex align-items-start gap-2 rounded bg-body-secondary px-2 py-1">
      <pre className="small mb-0 py-1 flex-grow-1" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }} aria-label={`Value of ${name}`}><code>{value}</code></pre>
      <button type="button" className="btn btn-sm btn-icon btn-outline-secondary flex-shrink-0" onClick={() => void copy()} aria-label={`Copy ${name}`} title="Copy">
        <i className={`bi ${copied ? 'bi-clipboard-check' : 'bi-clipboard'}`} aria-hidden="true" />
      </button>
    </div>
  )
}

function RevealSheet({ secret, onClose, onShown }: { secret: Secret; onClose: () => void; onShown: (value: string) => void }) {
  const [password, setPassword] = useState('')
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setErr(null)
    setBusy(true)
    try {
      const out = await unwrap(api.POST('/system/secrets/reveal', { body: { key: secret.key, currentPassword: password } }))
      setPassword('')
      onShown(out.value)
    } catch (x) {
      setErr(x as Error)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Sheet title={`View ${secret.key}`} open onClose={onClose}>
      <form onSubmit={submit} className="vstack gap-3">
        <p className="small text-body-secondary mb-0">{secret.description} Enter your own password to see it; the view is recorded in the audit log.</p>
        <ErrorBanner error={err} />
        <Field label="Your password">
          {(f) => <input id={f} type="password" autoComplete="current-password" className="form-control" value={password} onChange={(e) => setPassword(e.target.value)} required />}
        </Field>
        <div className="d-flex justify-content-end gap-2">
          <button type="button" className="btn btn-outline-primary" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" disabled={busy}>{busy ? 'Checking…' : 'View'}</button>
        </div>
      </form>
    </Sheet>
  )
}

/** SettingsTable is a filterable list of names and values (collapsed: behind a summary). */
function SettingsTable({ title, note, rows, collapsed }: { title: string; note?: string; rows: Setting[]; collapsed?: boolean }) {
  const [filter, setFilter] = useState('')
  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase()
    return q ? rows.filter((r) => r.name.toLowerCase().includes(q) || r.value.toLowerCase().includes(q)) : rows
  }, [rows, filter])
  const body = (
    <div className="vstack gap-2">
      {note && <p className="small text-body-secondary mb-0">{note}</p>}
      <input type="search" className="form-control form-control-sm" placeholder="Filter by name or value" aria-label={`Filter ${title}`} value={filter} onChange={(e) => setFilter(e.target.value)} />
      <div className="table-responsive" style={{ maxHeight: '28rem' }}>
        <table className="table table-sm small mb-0">
          <caption className="visually-hidden">{title}</caption>
          <thead className="sticky-top">
            <tr><th>Name</th><th>Value</th></tr>
          </thead>
          <tbody>
            {shown.map((r) => (
              <tr key={r.name}>
                <td className="font-monospace fs-8 text-nowrap">{r.name}</td>
                <td className="font-monospace fs-8 text-break">{r.value === '' ? <span className="text-body-secondary">(empty)</span> : r.value}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {shown.length === 0 && <p className="small text-body-secondary mt-2 mb-0">Nothing matches.</p>}
      </div>
    </div>
  )
  if (!collapsed) return <Card title={title}>{body}</Card>
  return (
    <details className="card card-body">
      <summary className="fs-6 fw-semibold">{title}</summary>
      <div className="mt-2">{body}</div>
    </details>
  )
}
