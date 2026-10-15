import { useEffect, useState, type FormEvent } from 'react'
import { api, ApiError, idempotencyKey, unwrap, type Schemas } from '../../../lib/api/client'
import { formatDateTime } from '../../../lib/format'
import { useAsync } from '../../../lib/useAsync'
import { Empty, ErrorBanner, Field, Sheet, Spinner } from '../../../components/ui'
import { useAuth } from '../../../components/auth'
import { SettingsPage } from '../Settings'

type Overview = Schemas['BackupOverview']
type BackupFile = Schemas['BackupFile']
type Preview = Schemas['RestorePreview']

const MIN_PASSPHRASE = 16

/** formatSize prints a byte count for people. */
export function formatSize(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

const kindLabels: Record<string, string> = {
  scheduled: 'Scheduled',
  manual: 'Manual',
  upload: 'Uploaded',
  prerestore: 'Before a restore',
  legacy: 'Old plaintext dump',
}

/** Why a scheduled backup failed, in words (the server reports a category only). */
const failureText: Record<string, string> = {
  database: 'the database could not be read',
  disk: 'the backups folder could not be written (full or not writable?)',
  key: 'the server backup key is missing',
  other: 'see the server log',
}

/** BackupSettingsPage is /settings/backup: backups, downloads and restores (ADR-010). */
export function BackupSettingsPage() {
  return (
    <SettingsPage>
      <BackupAndRestore />
    </SettingsPage>
  )
}

function BackupAndRestore() {
  const overview = useAsync(() => unwrap(api.GET('/backups')), [])
  const [preview, setPreview] = useState<Preview | null>(null)
  const [restoring, setRestoring] = useState<{ name: string; needsPassphrase: boolean } | null>(null)

  // A restore staged earlier (by anyone) is shown until it is applied, discarded or expires.
  useEffect(() => {
    let alive = true
    api.GET('/restores/current').then(({ data }) => alive && data && setPreview(data))
    return () => {
      alive = false
    }
  }, [])

  if (overview.loading && !overview.data) return <Spinner />
  return (
    <section className="space-y-4" aria-labelledby="backup-heading">
      <h2 id="backup-heading" className="text-xl font-bold text-brand-900">Backup & restore</h2>
      <ErrorBanner error={overview.error} />
      {overview.data && <Schedule ov={overview.data} />}
      {preview ? (
        <RestorePreviewCard preview={preview} onDiscarded={() => setPreview(null)} />
      ) : (
        overview.data && (
          <>
            <Actions ov={overview.data} onChanged={overview.reload} onUploaded={(name) => setRestoring({ name, needsPassphrase: true })} />
            <StoredBackups ov={overview.data} onRestore={(f) => setRestoring({ name: f.name, needsPassphrase: f.kind === 'upload' })} />
          </>
        )
      )}
      {restoring && (
        <StageSheet
          name={restoring.name}
          needsPassphrase={restoring.needsPassphrase}
          onClose={() => setRestoring(null)}
          onStaged={(p) => {
            setRestoring(null)
            setPreview(p)
          }}
        />
      )}
    </section>
  )
}

function Schedule({ ov }: { ov: Overview }) {
  const last = ov.lastRun
  return (
    <div className="card space-y-2 text-sm">
      <h3 className="font-semibold">Automatic backups</h3>
      {ov.keyConfigured ? (
        <p>
          Daily at <strong>{ov.times.join(', ') || '—'}</strong> ({ov.timeZone}), kept for{' '}
          <strong>{ov.retentionDays > 0 ? `${ov.retentionDays} days` : 'ever'}</strong>.
          {ov.nextRun && <> Next: {formatDateTime(ov.nextRun)}.</>}
        </p>
      ) : (
        <p role="alert" className="font-medium text-amber-800">
          The server backup key (backup_key) is not set up, so backups are not stored here or taken automatically. You can still download a
          backup protected with a passphrase.
        </p>
      )}
      {last && (
        <p>
          Last scheduled backup: {formatDateTime(last.slot)} —{' '}
          <span className={last.result === 'failed' ? 'font-semibold text-red-700' : 'font-semibold text-green-700'}>
            {last.result === 'ok' ? 'done' : last.result}
          </span>
          {last.result === 'failed' && last.error && <span className="block text-red-700">Reason: {failureText[last.error] ?? last.error}.</span>}
        </p>
      )}
      <p className="text-xs text-slate-500">The schedule and how long backups are kept are set by the deployment (.env).</p>
      {ov.restoreUnavailable && <p className="font-medium text-amber-800">Restoring is not available: {ov.restoreUnavailable}.</p>}
    </div>
  )
}

function Actions({ ov, onChanged, onUploaded }: { ov: Overview; onChanged: () => void; onUploaded: (name: string) => void }) {
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const [download, setDownload] = useState(false)

  const backupNow = async () => {
    setBusy(true)
    setErr(null)
    setMsg('')
    try {
      const b = await unwrap(api.POST('/backups', { params: { header: { 'Idempotency-Key': idempotencyKey() } } }))
      setMsg(`Backup stored: ${b.name}`)
      onChanged()
    } catch (e) {
      setErr(e as Error)
    } finally {
      setBusy(false)
    }
  }

  const upload = async (file: File | undefined) => {
    if (!file) return
    setErr(null)
    setMsg('')
    if (file.size > ov.maxUploadBytes) {
      setErr(new Error(`The file is larger than ${formatSize(ov.maxUploadBytes)}.`))
      return
    }
    setBusy(true)
    try {
      const f = await unwrap(
        api.POST('/backups/uploads', {
          body: file as unknown as string,
          bodySerializer: (b: unknown) => b as BodyInit,
          headers: { 'Content-Type': 'application/octet-stream' },
        }),
      )
      onChanged()
      onUploaded(f.name)
    } catch (e) {
      setErr(e as Error)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="card space-y-3">
      <ErrorBanner error={err} />
      <div className="flex flex-wrap gap-2">
        <button className="btn-primary" onClick={backupNow} disabled={busy || !ov.keyConfigured}>
          Back up now
        </button>
        <button className="btn-secondary" onClick={() => setDownload(true)} disabled={busy}>
          Download a backup
        </button>
        <label className={`btn-secondary cursor-pointer ${busy || ov.restoreUnavailable ? 'pointer-events-none opacity-50' : ''}`}>
          Restore from file
          <input
            type="file"
            accept=".age"
            className="sr-only"
            aria-label="Restore from file"
            disabled={busy || !!ov.restoreUnavailable}
            onChange={(e) => void upload(e.target.files?.[0])}
          />
        </label>
      </div>
      {busy && <p className="text-sm text-slate-500">Working…</p>}
      {msg && <p className="text-sm text-green-700">{msg}</p>}
      <DownloadSheet open={download} onClose={() => setDownload(false)} />
    </div>
  )
}

/** filenameOf reads the file name from a Content-Disposition header. */
function filenameOf(disposition: string | null): string {
  const m = /filename="([^"]+)"/.exec(disposition ?? '')
  return m?.[1] ?? 'cactusinv-backup.age'
}

function DownloadSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [pass, setPass] = useState('')
  const [again, setAgain] = useState('')
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState('')
  const close = () => {
    setPass('')
    setAgain('')
    setErr(null)
    setDone('')
    onClose()
  }
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setErr(null)
    if (pass.length < MIN_PASSPHRASE) return setErr(new Error(`The passphrase needs at least ${MIN_PASSPHRASE} characters.`))
    if (pass !== again) return setErr(new Error('The two passphrases are not the same.'))
    setBusy(true)
    try {
      const { data, error, response } = await api.POST('/backups/export', { body: { passphrase: pass }, parseAs: 'blob' })
      if (error !== undefined || !response.ok || !data) {
        throw new ApiError((error as Schemas['Problem'] | undefined) ?? { type: 'about:blank', title: response.statusText, status: response.status })
      }
      const name = filenameOf(response.headers.get('Content-Disposition'))
      const url = URL.createObjectURL(data as Blob)
      const a = document.createElement('a')
      a.href = url
      a.download = name
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(url)
      setPass('')
      setAgain('')
      setDone(`Downloaded ${name}. Keep the passphrase safe: without it the file cannot be restored.`)
    } catch (x) {
      setErr(x as Error)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Sheet title="Download a backup" open={open} onClose={close}>
      <form onSubmit={submit} className="space-y-3">
        <p className="text-sm text-slate-600">
          A complete backup is made now and encrypted with your passphrase. It contains customer details and bank accounts, so store it
          safely. Invoice PDFs are not included; they are recreated after a restore.
        </p>
        <ErrorBanner error={err} />
        <Field label="Passphrase" hint={`At least ${MIN_PASSPHRASE} characters. It cannot be recovered.`}>
          {(f) => <input id={f} type="password" autoComplete="new-password" className="input" value={pass} onChange={(e) => setPass(e.target.value)} required />}
        </Field>
        <Field label="Passphrase again">
          {(f) => <input id={f} type="password" autoComplete="new-password" className="input" value={again} onChange={(e) => setAgain(e.target.value)} required />}
        </Field>
        {done && <p className="text-sm text-green-700">{done}</p>}
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={close}>
            Close
          </button>
          <button className="btn-primary" disabled={busy}>
            {busy ? 'Preparing…' : 'Download'}
          </button>
        </div>
      </form>
    </Sheet>
  )
}

function StoredBackups({ ov, onRestore }: { ov: Overview; onRestore: (f: BackupFile) => void }) {
  if (ov.files.length === 0) return <Empty>No backups are stored yet.</Empty>
  const canRestore = (f: BackupFile) => !f.legacy && !ov.restoreUnavailable
  return (
    <div className="card overflow-x-auto p-0">
      <table className="w-full text-sm">
        <caption className="sr-only">Stored backups</caption>
        <thead className="bg-cream-100 text-left text-xs uppercase tracking-wide text-slate-600">
          <tr>
            <th className="px-3 py-2">Taken</th>
            <th className="px-3 py-2">Kind</th>
            <th className="hidden px-3 py-2 md:table-cell">File</th>
            <th className="px-3 py-2 text-right">Size</th>
            <th className="px-3 py-2">
              <span className="sr-only">Actions</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {ov.files.map((f) => (
            <tr key={f.name} className="border-t border-cream-200">
              <td className="whitespace-nowrap px-3 py-2">{formatDateTime(f.createdAt)}</td>
              <td className="px-3 py-2">{kindLabels[f.kind] ?? f.kind}</td>
              <td className="hidden break-all px-3 py-2 font-mono text-xs md:table-cell">{f.name}</td>
              <td className="whitespace-nowrap px-3 py-2 text-right">{formatSize(f.size)}</td>
              <td className="px-3 py-2 text-right">
                {canRestore(f) ? (
                  <button className="btn-secondary" onClick={() => onRestore(f)} aria-label={`Restore ${f.name}`}>
                    Restore…
                  </button>
                ) : f.legacy ? (
                  <span className="text-xs text-slate-500">deploy.sh --restore</span>
                ) : null}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function StageSheet({ name, needsPassphrase, onClose, onStaged }: { name: string; needsPassphrase: boolean; onClose: () => void; onStaged: (p: Preview) => void }) {
  const [pass, setPass] = useState('')
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setErr(null)
    setBusy(true)
    try {
      onStaged(await unwrap(api.POST('/restores', { body: { backup: name, ...(pass ? { passphrase: pass } : {}) } })))
    } catch (x) {
      setErr(x as Error)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Sheet title="Check a backup" open onClose={onClose}>
      <form onSubmit={submit} className="space-y-3">
        <p className="text-sm text-slate-600">
          The backup is opened and checked first; nothing changes until you confirm the restore. <span className="break-all font-mono text-xs">{name}</span>
        </p>
        <ErrorBanner error={err} />
        <Field label="Passphrase" hint={needsPassphrase ? 'The passphrase the file was downloaded with.' : 'Leave empty for backups stored by this server.'}>
          {(f) => <input id={f} type="password" autoComplete="off" className="input" value={pass} onChange={(e) => setPass(e.target.value)} required={needsPassphrase} />}
        </Field>
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button className="btn-primary" disabled={busy}>
            {busy ? 'Checking…' : 'Check backup'}
          </button>
        </div>
      </form>
    </Sheet>
  )
}

function RestorePreviewCard({ preview, onDiscarded }: { preview: Preview; onDiscarded: () => void }) {
  const [confirming, setConfirming] = useState(false)
  const [err, setErr] = useState<Error | null>(null)
  const discard = async () => {
    setErr(null)
    try {
      await unwrap(api.DELETE('/restores/{id}', { params: { path: { id: preview.id } } }))
      onDiscarded()
    } catch (x) {
      setErr(x as Error)
    }
  }
  return (
    <div className="card space-y-3" aria-label="Restore preview">
      <h3 className="font-semibold">Ready to restore</h3>
      <ErrorBanner error={err} />
      <dl className="grid grid-cols-1 gap-x-4 gap-y-1 text-sm sm:grid-cols-[max-content_1fr]">
        <dt className="text-slate-500">Backup taken</dt>
        <dd>
          {formatDateTime(preview.createdAt)} by {preview.createdBy} (version {preview.appVersion || '—'})
        </dd>
        <dt className="text-slate-500">Invoices made since</dt>
        <dd className={preview.invoicesSince > 0 ? 'font-semibold text-red-700' : ''}>{preview.invoicesSince}</dd>
        <dt className="text-slate-500">Audit log</dt>
        <dd>{preview.audit.summary}</dd>
        <dt className="text-slate-500">Safety backup</dt>
        <dd>{preview.safetyBackup ? 'The current data is backed up first, so this can be undone.' : 'None (no server backup key) — download a backup first.'}</dd>
        <dt className="text-slate-500">Expires</dt>
        <dd>{formatDateTime(preview.expiresAt)}</dd>
      </dl>
      {preview.warnings.length > 0 && (
        <ul aria-label="Warnings" className="list-disc space-y-1 rounded-xl bg-amber-50 p-3 pl-7 text-sm text-amber-900">
          {preview.warnings.map((w) => (
            <li key={w}>{w}</li>
          ))}
        </ul>
      )}
      <details className="text-sm">
        <summary className="cursor-pointer font-medium">Rows in the backup and now</summary>
        <table className="mt-2 w-full">
          <tbody>
            {preview.tables.map((t) => (
              <tr key={t.name} className="border-t border-cream-200">
                <td className="py-1 font-mono text-xs">{t.name}</td>
                <td className="py-1 text-right">{t.backup}</td>
                <td className="py-1 text-right text-slate-500">now {t.live}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </details>
      <div className="flex justify-end gap-2">
        <button className="btn-secondary" onClick={discard}>
          Discard
        </button>
        <button className="btn-danger" onClick={() => setConfirming(true)}>
          Restore…
        </button>
      </div>
      {confirming && <ConfirmRestore preview={preview} onClose={() => setConfirming(false)} />}
    </div>
  )
}

function ConfirmRestore({ preview, onClose }: { preview: Preview; onClose: () => void }) {
  const { endSession } = useAuth()
  const [password, setPassword] = useState('')
  const [word, setWord] = useState('')
  const [err, setErr] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setErr(null)
    setBusy(true)
    try {
      const res = await unwrap(api.POST('/restores/{id}/apply', { params: { path: { id: preview.id } }, body: { currentPassword: password, confirmation: word } }))
      endSession(
        'The backup was restored and everyone was signed out. Sign in again; automatic sending stays paused until you resume it.' +
          (res.safetyBackup ? ` The data before the restore is in ${res.safetyBackup}.` : ''),
      )
    } catch (x) {
      setErr(x as Error)
      setBusy(false)
    }
  }
  return (
    <Sheet title="Replace all data with this backup?" open onClose={onClose}>
      <form onSubmit={submit} className="space-y-3">
        <p className="text-sm text-slate-700">
          Everything is replaced by the backup from {formatDateTime(preview.createdAt)}. Invoice numbers already used are not reused. Everyone,
          you included, is signed out.
        </p>
        <p className={`text-sm font-medium ${preview.safetyBackup ? 'text-green-800' : 'text-amber-800'}`}>
          {preview.safetyBackup
            ? 'The current data is saved first as a stored backup ("Before a restore"); restore that to undo this.'
            : 'No safety backup will be taken: without the server backup key the current data cannot be saved first.'}
        </p>
        <ErrorBanner error={err} />
        <Field label="Your password">
          {(f) => <input id={f} type="password" autoComplete="current-password" className="input" value={password} onChange={(e) => setPassword(e.target.value)} required />}
        </Field>
        <Field label="Type RESTORE to confirm">
          {(f) => <input id={f} className="input" autoComplete="off" value={word} onChange={(e) => setWord(e.target.value)} required />}
        </Field>
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button className="btn-danger" disabled={busy || word.trim() !== 'RESTORE'}>
            {busy ? 'Restoring…' : 'Restore'}
          </button>
        </div>
      </form>
    </Sheet>
  )
}
