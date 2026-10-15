# ADR-009 — Append-only, hash-chained audit trail

**Status:** accepted · **Date:** 2026-10-15

## Context
Albie needs to see who created, issued, e-mailed, marked paid/overdue or voided each
invoice, and wants a complete audit trail that cannot be deleted.

## Decision
- One table, `audit_log`, records every state change and sign-in: invoices (including
  PDF views and draft deletions), customers, products, bank accounts, currencies, business
  and e-mail-server settings, schedules (including failed runs), statements and users
  (sign-in, failed sign-in, sign-out, password changes). Each entry stores **who** (user id
  and login, or a named system actor: Scheduler, E-mail dispatcher, Invoice archive),
  **what** (`action`), **which record**, a summary and the time.
- Entries are written **in the same transaction** as the change they describe, so a change
  and its record exist together or not at all.
- **Cannot be changed or deleted:** MariaDB triggers reject every `UPDATE` and `DELETE` on
  `audit_log`; there is no API or use case that writes anything but appends; deleting a
  draft invoice no longer cascades to its history.
- **Tamper-evident:** each entry's SHA-256 covers its content and the previous entry's
  hash (`audit_head` serialises appends). `GET /audit/verify` (Audit log → Verify
  integrity) recomputes the chain and pinpoints the first altered or missing entry, so even
  a database administrator who bypasses the triggers is detected. Backups contain the log.
- The pre-existing `invoice_events` history was migrated into the chain (Go migration 9)
  with actor "unknown (before the audit log)".

## Consequences
The log only grows (a few hundred bytes per action). Removing it requires dropping the
table, which a backup and the hash chain both expose.

## Amendment — restores (2026-10-15, ADR-010)
The old `mariadb-dump` restore (ADR-007) dropped and recreated `audit_log`; `DROP` fires no
trigger, so it replaced the trail silently. From M14 a restore never deletes or rewrites
an audit entry:
- The backup's audit chain is verified when the restore is staged; the preview shows the
  result and the merge plan.
- **Merge rules** (`domain.PlanAuditMerge`): if every backup entry is already in the live
  trail, keep the live trail as it is; if the live trail is a prefix of the backup's,
  append the backup's missing entries verbatim (`AuditRepository.AppendVerbatim` refuses
  any entry that does not chain from the head); otherwise (the two diverged) append the
  backup's entries after the fork point *re-sealed* onto the live head, between a
  `backup.audit_resealed_begin` and a `backup.audit_resealed_end` marker that record the
  original sequence numbers and head hash. Re-sealed entries keep their content (time,
  actor, action, record, summary) and get new hashes.
- **Verbatim appends keep the backup author's hashes**, so they are only allowed where the
  live trail is a strict prefix of the backup's. Invariant: an *uploaded* file can never
  contain the live head — the upload is audited (`backup.uploaded`, with the file's
  SHA-256) before it can be staged, and that entry's hash covers the file's own digest —
  so an uploaded file's entries are always re-sealed, never passed off as the live trail's
  own (test `TestUploadedBackupIsNeverVerbatim`). The CLI `backup restore FILE` (14.17)
  must likewise audit the file with its SHA-256 before staging it. `Summary()` names the
  exact verbatim range (#from–#to).
- **Markers name their block**: the begin marker states "the next N entries (#a–#b) …
  ends at marker #e", the end marker "end of the block begun at marker #s", and
  `backup.restored` names both; a marker-like entry inside the backup's content therefore
  sits visibly inside the announced range and cannot pass for the real end.
- A broken chain stays visible: a live trail that does not verify is not repaired by the
  merge, and a backup whose own chain is broken is re-sealed with the break named in the
  marker and in the preview's warnings.
- Every applied restore appends `backup.restored` (source file, SHA-256, schema N → M,
  merge plan, warnings); refused and failed restores append `backup.restore_refused` /
  `backup.restore_failed`.
- Restores of legacy `.sql.gz` dumps (kept one release, ADR-007) still bypass this; the
  operator records them with `cactusinv backup note-legacy-restore FILE`, and the hole
  closes when the sidecar's dumps are retired.
- Appends assign `seq` explicitly (last + 1, under the `audit_head` lock) instead of
  relying on `AUTO_INCREMENT`, which InnoDB does not roll back: a rolled-back append used
  to leave a gap that verification reports as missing entries.
