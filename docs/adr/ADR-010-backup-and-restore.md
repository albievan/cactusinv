# ADR-010 — Encrypted backup and restore in the application

**Status:** accepted · **Date:** 2026-10-15 · Amends ADR-006, ADR-007 and ADR-009 · Amended by ADR-011

## Context
Albie wants backup and restore under Settings. The first backups (ADR-007) were plaintext
`mariadb-dump` files written by a sidecar: they hold customer PII, bank details and
password hashes in clear, and restoring one drops and recreates `audit_log` — triggers do
not fire on `DROP`, so a restore silently replaced the audit trail that ADR-009 promises
cannot be changed. The application image is distroless (no `mariadb` client), so an in-app
backup has to be written in Go.

## Decision

**Contents.** A backup is an application-level export of every base table of the
database except an explicit exclude list: `sessions`, `idempotency_keys`,
`goose_db_version`, `audit_head`, `system_state`, `backup_runs` (all transient or
installation-local). A new table is exported automatically; an integration test fails
until it is listed as either exported or excluded. Generated columns are not exported (the
database recomputes them). Invoice PDFs are not in backups — they are derived data and are
re-exported after a restore (ADR-008). There are no secrets in a backup: `app_key`,
`backup_key` and database passwords never leave the installation; the SMTP password is
exported only as the AES-GCM ciphertext it already is, and is cleared on restore when this
installation's `app_key` cannot decrypt it.

**Format v1.** `age( gzip( tar( header.json, tables/<name>/<part>.jsonl …, digest.json ) ) )`.
- `header.json` (first entry): `formatVersion` (1), `schemaVersion` (the goose version),
  `appVersion`, `build`, `createdAt`, `createdBy`, and `tables[{name, columns[{name,
  kind}]}]` in parent → child order. Column kinds: `int`, `decimal`, `text`, `bytes`,
  `datetime`, `date`; any other database type (e.g. a float) fails the backup.
- `tables/<name>/000001.jsonl`, `…/000002.jsonl`: the table's rows split into parts of at
  most 8 MiB (tar needs each entry's size up front; parts keep memory bounded without
  temporary files), in header table order, parts numbered from 1 without gaps. One
  canonical JSON array per row, in header column order and primary-key order: `null`; integers as JSON numbers; `text` and `decimal` as strings;
  `bytes` as base64; `datetime` as `YYYY-MM-DDTHH:MM:SS.ffffffZ` (UTC); `date` as
  `YYYY-MM-DD`.
- `digest.json` (last entry): per table the row count and the SHA-256 of its rows' bytes
  (all parts, in order), plus the audit head (`seq`, `hash` of the last `audit_log` row).
- Names: `cactusinv-<UTC yyyymmddThhmmssZ>-s<schema>-<scheduled|manual|upload>.age`
  (strict pattern, `s0` = schema not known yet, used for uploads). Stored names are always
  generated, never taken from user input, and never become paths beyond the backups
  directory.

**Integrity.** age authenticates the whole payload (any flipped, truncated or reordered
byte fails). On top, the decoder recomputes every table's SHA-256 and row count and the
audit head and compares them with `digest.json`, accepts only regular-file tar entries with
the expected names in the expected order, requires every row in exactly its canonical
encoding, rejects unknown header fields and invalid identifiers, and enforces limits (header
1 MiB, one row 32 MiB, 4 GiB decompressed). Corrupt, tampered, truncated and
wrong-passphrase files all produce the one error `ErrBackupInvalid`, so the error does not
tell an attacker which check failed.

**Keys.** Stored and scheduled backups are encrypted to the server key: the optional
secret `backup_key` (an age identity, X25519 `AGE-SECRET-KEY-1…`, generated at install —
ADR-006), so `age -d -i backup_key FILE` opens them without the application. Downloads are
encrypted with a passphrase of at least 16 characters (age scrypt, work factor 2^18); the
decoder caps the accepted work factor at 2^18 so an uploaded file cannot demand more
memory. Uploads are stored as the ciphertext they arrive as. Without `backup_key`, stored
and scheduled backups are off (with a warning) and downloads still work. Losing the key or
a passphrase makes the backup unrecoverable.

**Schema policy.** A backup restores into the same or a newer schema: the staging database
is migrated up to the backup's version, loaded, then migrated to head with the real
migrations (Go migrations included). A backup from a newer schema is refused
(`ErrBackupTooNew`); an unknown format version is refused (`ErrBackupUnsupported`).

**Two-phase restore.**
1. *Stage* (`StageRestore`): decrypt and decode the file into the staging database
   `<db>_restore` (`CACTUSINV_RESTORE_DB_NAME`, granted to the app user by the database
   init script) and migrate it to head. The live data is not touched. The preview shows
   schema N → M, row counts (backup vs. live), the audit merge plan, invoices created since
   the backup (they will disappear), whether the actor's account exists in the backup, and
   warnings. A stage expires after 30 minutes.
2. *Safety backup*: before the swap, with `backup_key` configured, the live data is stored
   as a backup of kind `prerestore`, named in the result and in `backup.restored`, so a
   wrong restore can be undone by restoring it. If it cannot be taken the restore stops and
   nothing changes. Without `backup_key` the preview warns that no safety backup will be
   taken, and the confirmation says so. (Writes between that snapshot and the swap are not
   in it; the window is the time the snapshot takes.) It is recorded by the restore's own
   audit entry, so the live trail does not change between preview and swap.
3. *Apply* (`ApplyRestore`, needs the actor's current password and the word `RESTORE`):
   on one connection, `SET autocommit=0; LOCK TABLES <every live table> WRITE, <staging
   tables> READ`; refuse when an outbox row is `sending` (`ErrBusy`); delete the live rows
   child → parent (never `audit_log`); `INSERT … SELECT` parent → child with explicit
   column lists; invoice sequence = GREATEST(live, backup) — numbers never rewind; reset
   `exported_version` so the archive re-renders; clear an undecryptable SMTP password;
   delete every session and idempotency key; merge the audit trail (ADR-009); hold
   automation (`system_state.automation_held`) and append `backup.restored`; `COMMIT`,
   then `UNLOCK TABLES`. Any error rolls back the whole swap. Refusals and failures are
   audited in their own transaction (`backup.restore_refused`, `backup.restore_failed`).
4. After a restore the dispatcher and the schedule runner claim nothing (their claim
   queries read `system_state` in share mode) until an admin resumes automation: restored
   outbox rows and schedules may otherwise repeat e-mails sent after the backup was taken.

**Concurrency and failure.** Stage, apply and discard run under an installation-wide
`GET_LOCK('cactusinv_backup:<db>')` (every replica), plus the in-process slot. Stage,
discard and housekeeping hold it on a dedicated connection; apply takes it on the swap
connection itself (before `SET autocommit=0`, released after `UNLOCK TABLES`), so if that
connection drops the swap aborts and the lock is freed with it, and no other replica can
hold the lock while a swap runs; a busy lock is `ErrBusy` and housekeeping skips its pass. A new stage
first clears `system_state`'s staged restore in its own transaction, so no process can
apply a staging database that is being reloaded. During the swap every exit — error,
panic, failed COMMIT — rolls back, unlocks and restores autocommit, or discards the
connection; COMMIT runs detached from the request with a 2-minute bound, and every detached cleanup
(rollback, unlock, lock release) with a 30-second bound, discarding the connection when it
fails. The swap writes its own restore id into `system_state.held_restore_id` (migration
14): when COMMIT reports an error, that id — not the backup's name, which an earlier
unresumed restore may share — decides. Its id there: committed (`backup.restored`); the
stage still recorded under its id: not committed (`backup.restore_failed`); anything else:
`backup.restore_outcome_unknown` and `ErrOutcomeUnknown`, never success or failure. After a
restore the staging copy is emptied detached from the request, under the lock, and only if
no other stage is recorded; housekeeping also empties a staging database for which no stage
is recorded. Errors name tables and MariaDB error numbers only, never driver text
(which can quote row values). The stager only accepts a staging database named
`<db>_restore` whose pool really is connected to it. The decoders read their input to the
end: trailing data is `ErrBackupInvalid`, and age authenticates its last chunk there.

**Downloads** stream from inside one consistent snapshot; a wall-clock bound
(`ExportTimeout`, default 30 minutes) ends the snapshot and the download if the client
stalls, so an open read view never lingers. (Alternative considered: encrypt to a
temporary file first, then stream — doubles disk use and leaves large
encrypted files behind on failures; rejected for now.)

**Operations.** One backup or restore operation at a time per process (`ErrBusy`);
scheduled backups run at `CACTUSINV_BACKUP_TIMES` (local times in `TZ`), once per slot
across replicas (`backup_runs.slot` is unique); retention prunes `.age` files and legacy
`cactusinv-*.sql.gz` dumps older than `CACTUSINV_BACKUP_RETENTION_DAYS`, never the newest
backup, with one audit entry per pass. Files are written as `.part`, fsynced, linked into place,
mode 0600; unknown files in the directory are ignored, and `.part` files older than a day
(left by a crash) are removed when the directory is listed. A name is reserved
(an `O_EXCL` hidden `.reserved` file) from `Create` until `Commit`/`Abort`, so uploads that
collide within one second take the next free second instead of failing at commit. The prune audit entry records the count and
the first and last name, not every name.

## Consequences
- Backups are safe to copy off the host; the key must be copied off too (and kept apart).
- A restore can roll the business back in time; the preview says how far, and automation
  stays paused until someone has looked at the Outbox and schedules.
- Existing installations need the staging database and its GRANT (done by `deploy.sh
  --install`); until then restore is disabled with a reason. The grant is exactly what the
  stager needs: `SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, INDEX, REFERENCES,
  LOCK TABLES, TRIGGER` on `<db>_restore` (an older `ALL` is narrowed).
- Legacy `.sql.gz` dumps (this release only, `deploy.sh --restore`): the dump is read from
  the backups volume through the app (`cactusinv backup cat-legacy NAME`), checked (`gzip
  -t`, mariadb-dump header) and a safety backup taken before anything changes; only `DROP`
  and `CREATE DATABASE` run as MariaDB root, the dump itself as the application user.
- In the browser a download is assembled in memory (a `Blob`) before it is saved: fine for
  the sizes an invoicing database reaches; very large installations should use
  `cactusinv backup create --out FILE`.
- Format changes need a new `formatVersion`, a decoder for the old one, and a test.
- `filippo.io/age` (BSD-3-Clause) is a new dependency.

## Amendment — users (2026-10-16, ADR-011)
- A session whose user must change their password cannot reach any backup or restore route
  (403 `password-change-required`); the gate covers every API operation.
- Applying a restore shares the sign-in rate limit (10 failed password attempts a minute
  per client address).
- "Can you sign in afterwards" in the preview now means: your account is an **active** user
  in the backup. Restored users keep the disabled and must-change state they had in the
  backup; a backup from before v1.6.0 is migrated like a live database (its non-e-mail
  logins must change their password).
