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
