import { useEffect, useState, type FormEvent } from 'react'
import { InstallCommands } from '../../../components/install'
import { useSearchParams } from 'react-router-dom'
import { api, unwrap, type Schemas } from '../../../lib/api/client'
import { useAsync } from '../../../lib/useAsync'
import { ErrorBanner, Field, Spinner } from '../../../components/ui'
import { SettingsPage } from '../Settings'
import { useCan } from '../../../components/auth'

type System = Schemas['SystemSettings']
type UpdateCheck = Schemas['UpdateCheck']
type UpgradeJob = Schemas['UpgradeJob']

/** SystemSettingsPage is /settings/system: session timeouts and the folder mapping. */
export function SystemSettingsPage() {
  return (
    <SettingsPage>
      <SystemForm />
    </SettingsPage>
  )
}

/** deployCommand is what applies a folder mapping on this deployment. */
function deployCommand(target: string): string {
  return target === 'dockerdesktop' ? './deploy.sh --install --dockerdesktop' : './deploy.sh --install'
}

/** upgradeCommand installs the newest version on this deployment (M27). */
export function upgradeCommand(target: string): string {
  return target ? `./deploy.sh --upgrade --${target}` : './deploy.sh --upgrade --dockerdesktop|--k3s|--k8s'
}

/** The System Settings tabs; the open one is in the address (?tab=…). */
const tabs = [
  { key: 'web', label: 'Web settings' },
  { key: 'sessions', label: 'Sessions' },
  { key: 'folders', label: 'Folders' },
  { key: 'version', label: 'Version' },
] as const
type Tab = (typeof tabs)[number]['key']

function SystemForm() {
  const can = useCan()
  const [params, setParams] = useSearchParams()
  const tab: Tab = tabs.find((t) => t.key === params.get('tab'))?.key ?? 'web'
  const show = (t: Tab) => {
    setSaved('')
    setErr(null)
    setParams(t === 'web' ? {} : { tab: t }, { replace: true })
  }
  const loaded = useAsync(() => unwrap(api.GET('/settings/system')), [])
  const [s, setS] = useState<System | null>(null)
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState('')
  useEffect(() => {
    if (loaded.data) setS(loaded.data)
  }, [loaded.data])

  if (!s) return loaded.error ? <ErrorBanner error={loaded.error} /> : <Spinner />
  const d = s.deployment
  const edit = can('settings.system.edit')
  const docker = d.target === 'dockerdesktop'
  const set = (patch: Partial<System>) => {
    setSaved('')
    setS({ ...s, ...patch })
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setErr(null)
    setSaved('')
    setBusy(true)
    try {
      const out = await unwrap(api.PUT('/settings/system', { body: s }))
      setS(out)
      setSaved(out.pendingFolders ? `Saved. The new folders are used after ${deployCommand(d.target)}.` : 'Saved.')
    } catch (x) {
      setErr(x as Error)
    } finally {
      setBusy(false)
    }
  }

  const folders: { label: string; container: string; now: string; key: 'backupHostDir' | 'invoicesHostDir' }[] = [
    { label: 'Backups', container: d.backupDir, now: d.backupHostDir, key: 'backupHostDir' },
    { label: 'Invoice export (PDF archive)', container: d.invoicesDir, now: d.invoicesHostDir, key: 'invoicesHostDir' },
  ]

  const tabClass = (on: boolean) => `nav-link rounded-pill d-inline-flex align-items-center px-3 small fw-semibold ${on ? 'active' : 'bg-white border'}`
  return (
    <div className="vstack gap-3">
    <h2 id="system-heading" className="fs-5 fw-bold mb-0">
      System Settings
    </h2>
    <div role="tablist" aria-label="System Settings sections" className="nav nav-pills gap-2">
      {tabs.map((t) => (
        <button key={t.key} type="button" role="tab" id={`system-tab-${t.key}`} aria-selected={tab === t.key} aria-controls="system-panel"
          className={tabClass(tab === t.key)} style={{ minHeight: 44 }} onClick={() => show(t.key)}>
          {t.label}
        </button>
      ))}
    </div>
    <div id="system-panel" role="tabpanel" aria-labelledby={`system-tab-${tab}`} className="vstack gap-3">
    {tab === 'version' ? <VersionCard target={d.target} platform={d.platform} /> : (
    <form onSubmit={submit} className="vstack gap-3" aria-labelledby="system-heading">
      <fieldset disabled={!edit} className="vstack gap-3">
      <ErrorBanner error={err} />

      {tab === 'web' && (
      <div className="card card-body vstack gap-3">
        <h3 className="fs-6 fw-semibold mb-0">Web address</h3>
        <Field
          label="External address"
          hint={`The address customers and users reach this installation on, e.g. https://invoice.primesoft.co.za. Every link in an e-mail points here: quote Accept/Decline, invoice Mark as paid and password resets. Empty = the address the e-mail was sent from (now ${window.location.origin}); scheduled e-mails then carry no links.`}
        >
          {(id) => (
            <input id={id} type="url" inputMode="url" autoCapitalize="none" className="form-control" maxLength={300} placeholder="https://invoice.example.com"
              value={s.externalUrl ?? ''} onChange={(e) => set({ externalUrl: e.target.value })} />
          )}
        </Field>
        <div className="form-check">
          <input id="system-hsts" type="checkbox" className="form-check-input" checked={!!s.hsts} onChange={(e) => set({ hsts: e.target.checked })} aria-describedby="system-hsts-help" />
          <label className="form-check-label fw-semibold" htmlFor="system-hsts">HTTPS only (HSTS)</label>
          <div id="system-hsts-help" className="form-text">
            Browsers that open this installation over HTTPS remember, for a year, to use only HTTPS here and refuse to continue past a certificate warning.
            Switch it on only when the site has a certificate browsers trust (not the cluster's own self-signed one); switching it off later takes a year to reach
            browsers that already saw it.
          </div>
          {s.hsts && !(s.externalUrl ?? '').startsWith('https://') && (s.externalUrl ?? '') !== '' && (
            <div role="alert" className="small fw-semibold text-danger-emphasis mt-1">The external address must start with https:// for HTTPS only.</div>
          )}
        </div>
        <div className="form-check">
          <input id="system-cloudflare" type="checkbox" className="form-check-input" checked={!!s.behindCloudflare} onChange={(e) => set({ behindCloudflare: e.target.checked })} aria-describedby="system-cloudflare-help" />
          <label className="form-check-label fw-semibold" htmlFor="system-cloudflare">This site is reached through Cloudflare</label>
          <div id="system-cloudflare-help" className="form-text">
            Then each visitor's own address comes from Cloudflare, so one person's wrong passwords cannot lock everyone else out of signing in. Tick it only when the
            server can be reached through Cloudflare alone (the firewall lets in only Cloudflare's addresses); otherwise anyone could claim any address.
          </div>
        </div>
      </div>
      )}

      {tab === 'sessions' && (
      <div className="card card-body vstack gap-3">
        <h3 className="fs-6 fw-semibold mb-0">Sign-in sessions</h3>
        <p className="small text-body-secondary mb-0">Changes apply at once to everyone who is signed in.</p>
        <Field label="Sign out after this many minutes without activity" hint="0 = never. 5 minutes to 30 days (43200).">
          {(id) => (
            <input id={id} type="number" inputMode="numeric" min={0} max={43200} className="form-control" value={s.sessionIdleMinutes}
              onChange={(e) => set({ sessionIdleMinutes: Number(e.target.value) })} />
          )}
        </Field>
        <Field label="End every sign-in after this many hours" hint="However active. 1 hour to 365 days (8760); 168 = 7 days.">
          {(id) => (
            <input id={id} type="number" inputMode="numeric" min={1} max={8760} className="form-control" value={s.sessionMaxHours}
              onChange={(e) => set({ sessionMaxHours: Number(e.target.value) })} />
          )}
        </Field>
      </div>
      )}

      {tab === 'folders' && (
      <div className="card card-body vstack gap-3">
        <h3 className="fs-6 fw-semibold mb-0">Folders</h3>
        {docker ? (
          <p className="small text-body-secondary mb-0">
            Map each container folder to a folder on the host, e.g. a NAS share. Use an absolute folder (/Volumes/NAS/backups) or one in the
            project folder (./data/backups); leave it empty for the folder in .env. A new mapping is used after{' '}
            <code className="rounded bg-body-secondary px-1">{deployCommand(d.target)}</code>, which copies the existing files into the new folder
            when it is empty. On a Mac, the folder must be shared with Docker Desktop (Settings → Resources → File sharing).
          </p>
        ) : (
          <p className="small text-body-secondary mb-0">
            {d.target
              ? `On ${d.target} these folders are volume claims, set by the deployment (.env K8S_*_STORAGE), not mapped host folders.`
              : 'This installation was not started by deploy.sh, so its folders are set where it runs.'}
          </p>
        )}
        {folders.map((f) => (
          <div key={f.key} className="vstack gap-1 border-top pt-3">
            <p className="small fw-medium mb-0">{f.label}</p>
            <p className="fs-8 text-body-secondary mb-0">
              In the container: <code className="text-break">{f.container || '—'}</code>
              {docker && (
                <>
                  {' '}· mapped now to <code className="text-break">{f.now || '—'}</code>
                </>
              )}
            </p>
            {docker && (
              <Field label={`Host folder for ${f.label.toLowerCase()}`}>
                {(id) => (
                  <input id={id} className="form-control font-monospace" placeholder="(the folder in .env)" value={s[f.key]} autoComplete="off" spellCheck={false}
                    onChange={(e) => set({ [f.key]: e.target.value })} />
                )}
              </Field>
            )}
          </div>
        ))}
        {docker && s.pendingFolders && (
          <p role="note" className="small fw-medium text-warning-emphasis mb-0">
            A saved folder is not mapped yet: run {deployCommand(d.target)} to use it.
          </p>
        )}
      </div>
      )}

      </fieldset>
      {edit && (
        <div className="d-flex align-items-center gap-3">
          <button className="btn btn-primary" disabled={busy}>
            {busy ? 'Saving…' : 'Save'}
          </button>
          {saved && <p className="small text-success mb-0">{saved}</p>}
        </div>
      )}
    </form>
    )}
    </div>
    </div>
  )
}

/** VersionCard shows the running version and, when asked, whether a newer one is published. */
function VersionCard({ target, platform }: { target: string; platform: string }) {
  const ver = useAsync(() => unwrap(api.GET('/version')), [])
  const [check, setCheck] = useState<UpdateCheck | null>(null)
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)

  const run = async () => {
    setErr(null)
    setCheck(null)
    setBusy(true)
    try {
      setCheck(await unwrap(api.GET('/settings/system/update')))
    } catch (x) {
      setErr(x as Error)
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="card card-body vstack gap-3" aria-labelledby="version-heading">
      <h3 id="version-heading" className="fs-6 fw-semibold mb-0">Version</h3>
      <p className="small mb-0">
        {ver.data ? `cactusInv ${ver.data.version} (build ${ver.data.build})` : 'cactusInv'}
      </p>
      <ErrorBanner error={err} />
      <button type="button" className="btn btn-outline-primary align-self-start" disabled={busy} onClick={() => void run()}>
        {busy ? 'Checking…' : 'Check for updates'}
      </button>
      {check?.problem && (
        <p role="status" className="small text-warning-emphasis mb-0">{check.problem}</p>
      )}
      {check && !check.problem && !check.available && (
        <p role="status" className="small text-success mb-0">You have the newest version ({check.current}).</p>
      )}
      {check?.available && check.canInstall && <InstallUpdate check={check} />}
      {check?.available && !check.canInstall && (
        <div role="status" className="vstack gap-2 small">
          <p className="fw-medium text-primary-emphasis mb-0">
            Version {check.latest} is available (this is {check.current}).
          </p>
          {check.installProblem && <p className="text-body-secondary mb-0">{check.installProblem}</p>}
          <p className="text-body-secondary mb-0">
            To upgrade, run this in the cactusInv folder on the machine you deploy from
            {target && target !== 'dockerdesktop' ? ' (add --ssh-access user@host if you deploy over SSH)' : ''}:
          </p>
          <code className="d-block text-break rounded bg-body-secondary px-2 py-1">{upgradeCommand(target)}</code>
          <p className="text-body-secondary mb-0">
            It shows what changes and asks first, takes a backup, and goes back to this version if the new one does not start.
            The app is unavailable for a minute or two while it restarts; nobody is signed out.
          </p>
        </div>
      )}
      <hr className="my-1" />
      <div className="vstack gap-2">
        <h4 id="install-heading" className="fs-6 fw-semibold mb-0">Install or update by hand</h4>
        <p className="small text-body-secondary mb-0">
          {target
            ? 'This installation is a container image, upgraded with deploy.sh. These commands install cactusinv as a standalone program on another computer.'
            : 'Run one command on the computer cactusInv runs on, in its folder, then restart cactusinv. Your data stays where it is.'}{' '}
          The setup scripts pick the right file for the system and check it against the release's signed checksums; the other
          commands download one particular file as <code>cactusinv</code> (<code>cactusinv.exe</code> on Windows). A first
          install also needs a MariaDB 11 database:{' '}
          <a href="https://github.com/albievan/cactusinv#installing-the-program" target="_blank" rel="noreferrer" className="text-decoration-underline">installing the program</a>.
        </p>
        <InstallCommands platform={target ? '' : platform} />
      </div>
    </section>
  )
}

const upgradeSteps: Record<string, string> = {
  'backing-up': 'Taking a backup…', downloading: 'Downloading…', installing: 'Installing…', restarting: 'Restarting…',
}

/**
 * InstallUpdate installs the newest release in place of this standalone server (M31): two
 * clicks, a progress bar, then it waits for the new version to answer and reloads the page.
 */
function InstallUpdate({ check }: { check: UpdateCheck }) {
  const can = useCan()
  const [armed, setArmed] = useState(false)
  const [job, setJob] = useState<UpgradeJob | null>(null)
  const [err, setErr] = useState<Error | null>(null)
  const [waiting, setWaiting] = useState('')

  useEffect(() => {
    if (!job || job.state === 'failed') return
    let stop = false
    const timer = window.setInterval(async () => {
      if (stop) return
      if (job.state !== 'restarting') {
        try {
          const next = await unwrap(api.GET('/settings/system/update/progress'))
          if (!stop) setJob(next)
        } catch {
          if (!stop) setJob({ ...job, state: 'restarting' }) // the server went away: it is restarting
        }
        return
      }
      try {
        const v = await unwrap(api.GET('/version'))
        if (v.version === job.to) window.location.reload()
      } catch {
        // still starting
      }
    }, 1000)
    const giveUp = window.setTimeout(() => setWaiting('The server has not come back with version ' + job.to + ' after two minutes. Reload the page; if it shows ' + job.from + ', the new version did not start and the old one was put back.'), 120000)
    return () => {
      stop = true
      window.clearInterval(timer)
      window.clearTimeout(giveUp)
    }
  }, [job])

  const start = async () => {
    setErr(null)
    try {
      setJob(await unwrap(api.POST('/settings/system/update')))
    } catch (x) {
      setErr(x as Error)
      setArmed(false)
    }
  }

  const pct = job && job.total > 0 ? Math.round((job.done / job.total) * 100) : null
  return (
    <div role="status" className="vstack gap-2 small">
      <p className="fw-medium text-primary-emphasis mb-0">Version {check.latest} is available (this is {check.current}).</p>
      <ErrorBanner error={err} />
      {!job && can('administer') && (
        <>
          <p className="text-body-secondary mb-0">
            cactusInv takes a backup, downloads {check.latest}, checks it, puts it in place of this version and restarts.
            The app is unavailable for a few seconds; nobody is signed out.
          </p>
          <button type="button" className={`btn align-self-start ${armed ? 'btn-primary' : 'btn-outline-primary'}`} onClick={() => (armed ? void start() : setArmed(true))}>
            <i className="bi bi-download" aria-hidden="true" /> {armed ? `Yes, install ${check.latest} and restart` : 'Install and restart'}
          </button>
        </>
      )}
      {!job && !can('administer') && <p className="text-body-secondary mb-0">An administrator can install it here.</p>}
      {job && job.state !== 'failed' && (
        <>
          <p className="mb-0">{upgradeSteps[job.state] ?? 'Starting…'}{job.backup ? ` Backup: ${job.backup}.` : ''}</p>
          <div className="progress" style={{ height: 8 }} role="progressbar" aria-label="Update progress" aria-valuenow={pct ?? undefined} aria-valuemin={0} aria-valuemax={100}>
            <div className={`progress-bar${job.state === 'restarting' ? ' progress-bar-striped progress-bar-animated' : ''}`} style={{ width: `${job.state === 'restarting' ? 100 : pct ?? 5}%` }} />
          </div>
          {waiting && <p className="text-warning-emphasis mb-0">{waiting}</p>}
        </>
      )}
      {job?.state === 'failed' && (
        <p className="text-danger mb-0">The update failed and {job.from} keeps running: {job.error}</p>
      )}
    </div>
  )
}
