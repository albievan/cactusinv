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

## Amendment — billing periods (2026-10-17, M21)
Each run record carries the billing period it covered, `[period_start, period_end)`, and a run
whose period overlaps a billed one is refused. The primary key is `(schedule_id,
period_start)`; `period_key` (`2026-10`, `2026-Q4`, …) stays as a label.
- **Periods count from the start date** (Albie): a quarterly schedule starting in February
  bills February–April, May–July, …; monthly and yearly likewise in runs of interval months
  from the start month; weekly in runs of interval weeks from the Monday of the start week;
  a once schedule bills its day. Before, periods were calendar months, quarters and years
  (00017), and weekly runs were keyed by their date.
- **An edited schedule continues in the next unbilled period** (Albie): changing the day,
  weekday, frequency, interval or start date after a run never bills that time again. The
  search for the next occurrence starts no earlier than the end of the latest billed period
  (`ActivateFrom(pending, billedUntil, now)`); an occurrence whose period is only partly
  billed is skipped to the next whole one (October billed, switched to quarterly from
  1 October → the next run is in January).
- **Resuming retries a failed period** (Albie): a schedule paused while its pending period was
  failing (by the user, or automatically after `MaxScheduleFailures`) runs that period on
  resume; a healthy schedule paused by the user skips the periods missed while paused, as
  before.
- The editor's preview (`POST /schedules/preview?scheduleId=`) shows the runs as saving would
  set them.
- Migration `00020` backfills existing runs under each schedule's current recurrence; a
  period already billed twice keeps its first run and the others cover only their own day.
  The label is no longer unique: after a recurrence is edited, an old run's calendar label
  can equal that of a later, non-overlapping period, which a unique label would have
  skipped silently.
