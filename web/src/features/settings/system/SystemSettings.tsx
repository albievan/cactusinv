import { useEffect, useState, type FormEvent } from 'react'
import { api, unwrap, type Schemas } from '../../../lib/api/client'
import { useAsync } from '../../../lib/useAsync'
import { ErrorBanner, Field, Spinner } from '../../../components/ui'
import { SettingsPage } from '../Settings'
import { useCan } from '../../../components/auth'

type System = Schemas['SystemSettings']
type UpdateCheck = Schemas['UpdateCheck']

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

function SystemForm() {
  const can = useCan()
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

  return (
    <div className="space-y-4">
    <form onSubmit={submit} className="space-y-4" aria-labelledby="system-heading">
      <fieldset disabled={!edit} className="contents">
      <h2 id="system-heading" className="text-xl font-bold text-brand-900">
        System Settings
      </h2>
      <ErrorBanner error={err} />

      <div className="card space-y-3">
        <h3 className="font-semibold">Sign-in sessions</h3>
        <p className="text-sm text-slate-600">Changes apply at once to everyone who is signed in.</p>
        <Field label="Sign out after this many minutes without activity" hint="0 = never. 5 minutes to 30 days (43200).">
          {(id) => (
            <input id={id} type="number" inputMode="numeric" min={0} max={43200} className="input" value={s.sessionIdleMinutes}
              onChange={(e) => set({ sessionIdleMinutes: Number(e.target.value) })} />
          )}
        </Field>
        <Field label="End every sign-in after this many hours" hint="However active. 1 hour to 365 days (8760); 168 = 7 days.">
          {(id) => (
            <input id={id} type="number" inputMode="numeric" min={1} max={8760} className="input" value={s.sessionMaxHours}
              onChange={(e) => set({ sessionMaxHours: Number(e.target.value) })} />
          )}
        </Field>
      </div>

      <div className="card space-y-3">
        <h3 className="font-semibold">Folders</h3>
        {docker ? (
          <p className="text-sm text-slate-600">
            Map each container folder to a folder on the host, e.g. a NAS share. Use an absolute folder (/Volumes/NAS/backups) or one in the
            project folder (./data/backups); leave it empty for the folder in .env. A new mapping is used after{' '}
            <code className="rounded bg-cream-100 px-1">{deployCommand(d.target)}</code>, which copies the existing files into the new folder
            when it is empty. On a Mac, the folder must be shared with Docker Desktop (Settings → Resources → File sharing).
          </p>
        ) : (
          <p className="text-sm text-slate-600">
            {d.target
              ? `On ${d.target} these folders are volume claims, set by the deployment (.env K8S_*_STORAGE), not mapped host folders.`
              : 'This installation was not started by deploy.sh, so its folders are set where it runs.'}
          </p>
        )}
        {folders.map((f) => (
          <div key={f.key} className="space-y-1 border-t border-cream-200 pt-3">
            <p className="text-sm font-medium">{f.label}</p>
            <p className="text-xs text-slate-600">
              In the container: <code className="break-all">{f.container || '—'}</code>
              {docker && (
                <>
                  {' '}· mapped now to <code className="break-all">{f.now || '—'}</code>
                </>
              )}
            </p>
            {docker && (
              <Field label={`Host folder for ${f.label.toLowerCase()}`}>
                {(id) => (
                  <input id={id} className="input font-mono" placeholder="(the folder in .env)" value={s[f.key]} autoComplete="off" spellCheck={false}
                    onChange={(e) => set({ [f.key]: e.target.value })} />
                )}
              </Field>
            )}
          </div>
        ))}
        {docker && s.pendingFolders && (
          <p role="note" className="text-sm font-medium text-amber-800">
            A saved folder is not mapped yet: run {deployCommand(d.target)} to use it.
          </p>
        )}
      </div>

      </fieldset>
      {edit && (
        <div className="flex items-center gap-3">
          <button className="btn-primary" disabled={busy}>
            {busy ? 'Saving…' : 'Save'}
          </button>
          {saved && <p className="text-sm text-green-700">{saved}</p>}
        </div>
      )}
    </form>
    <VersionCard target={d.target} />
    </div>
  )
}

/** VersionCard shows the running version and, when asked, whether a newer one is published. */
function VersionCard({ target }: { target: string }) {
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
    <section className="card space-y-3" aria-labelledby="version-heading">
      <h3 id="version-heading" className="font-semibold">Version</h3>
      <p className="text-sm">
        {ver.data ? `cactusInv ${ver.data.version} (build ${ver.data.build})` : 'cactusInv'}
      </p>
      <ErrorBanner error={err} />
      <button type="button" className="btn-secondary" disabled={busy} onClick={() => void run()}>
        {busy ? 'Checking…' : 'Check for updates'}
      </button>
      {check?.problem && (
        <p role="status" className="text-sm text-amber-800">{check.problem}</p>
      )}
      {check && !check.problem && !check.available && (
        <p role="status" className="text-sm text-green-700">You have the newest version ({check.current}).</p>
      )}
      {check?.available && (
        <div role="status" className="space-y-2 text-sm">
          <p className="font-medium text-brand-900">
            Version {check.latest} is available (this is {check.current}).
          </p>
          <p className="text-slate-600">
            The app cannot replace itself. To upgrade, run this in the cactusInv folder on the machine you deploy from
            {target && target !== 'dockerdesktop' ? ' (add --ssh-access user@host if you deploy over SSH)' : ''}:
          </p>
          <code className="block break-all rounded bg-cream-100 px-2 py-1">{upgradeCommand(target)}</code>
          <p className="text-slate-600">
            It shows what changes and asks first, takes a backup, and goes back to this version if the new one does not start.
            The app is unavailable for a minute or two while it restarts; nobody is signed out.
          </p>
        </div>
      )}
    </section>
  )
}
