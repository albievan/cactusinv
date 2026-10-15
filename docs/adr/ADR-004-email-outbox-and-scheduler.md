# ADR-004 — E-mail outbox and schedule runner

**Status:** accepted · **Date:** 2026-10-15

## Context
Invoices must be e-mailed now, at a chosen future time, or automatically on a recurring
schedule. E-mail is an external side effect that cannot join a database transaction.

## Decision
- **Transactional outbox.** Any decision to e-mail writes an `email_outbox` row in the same
  transaction as the business change. A dispatcher worker delivers rows whose
  `next_attempt_at <= now`.
- **Scheduled send** is an outbox row with a future `next_attempt_at`; it can be
  cancelled until claimed.
- **Recurring schedules** are rows in `schedules` with a `next_run_at`. A runner worker
  claims due schedules with `FOR UPDATE SKIP LOCKED`, inserts `schedule_runs(schedule_id,
  period_start)` (UNIQUE → exactly one invoice per period), issues the invoice, writes the
  outbox row if `auto_send`, and advances `next_run_at` — all in one transaction.
- Delivery is **at-least-once**. Each message carries a `Message-ID` derived from the
  outbox row id so duplicate deliveries after a crash between SMTP accept and DB commit
  are recognisable by mail clients.
- Backoff `1 min × 2^attempts`, capped at 6 h; `failed` after 8 attempts; `sending` rows
  older than 10 min are reclaimed.

## Consequences
- One mechanism, three features. Workers are safe to run on several replicas.
- An SMTP outage delays mail; it never loses it. Failures are visible in the Outbox UI
  with retry.
