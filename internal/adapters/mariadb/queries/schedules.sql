-- name: ListSchedules :many
SELECT * FROM schedules
WHERE (sqlc.arg(customer_id) = '' OR customer_id = sqlc.arg(customer_id))
  AND (sqlc.arg(status) = '' OR status = sqlc.arg(status))
ORDER BY name, id
LIMIT ? OFFSET ?;

-- name: CountSchedules :one
SELECT COUNT(*) FROM schedules
WHERE (sqlc.arg(customer_id) = '' OR customer_id = sqlc.arg(customer_id))
  AND (sqlc.arg(status) = '' OR status = sqlc.arg(status));

-- name: GetSchedule :one
SELECT * FROM schedules WHERE id = ?;

-- name: ListScheduleLines :many
SELECT * FROM schedule_lines WHERE schedule_id IN (sqlc.slice(ids)) ORDER BY schedule_id, position;

-- name: InsertSchedule :exec
INSERT INTO schedules (id, customer_id, name, kind, email_scope, email_delay_days, status, frequency, interval_count, day_of_month, weekday, send_hour,
    send_minute, time_zone, start_date, end_date, sales_person, job, shipping_method, shipping_terms, notes,
    tax_rate_bp, auto_send, email_subject, email_body, next_occurrence, next_run_at, last_run_at, failure_count,
    last_error, created_at, updated_at, version)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1);

-- name: UpdateSchedule :execrows
UPDATE schedules SET customer_id = ?, name = ?, kind = ?, email_scope = ?, email_delay_days = ?, status = ?, frequency = ?, interval_count = ?, day_of_month = ?,
    weekday = ?, send_hour = ?, send_minute = ?, time_zone = ?, start_date = ?, end_date = ?, sales_person = ?,
    job = ?, shipping_method = ?, shipping_terms = ?, notes = ?, tax_rate_bp = ?, auto_send = ?, email_subject = ?,
    email_body = ?, next_occurrence = ?, next_run_at = ?, last_run_at = ?, failure_count = ?, last_error = ?,
    updated_at = ?, version = version + 1
WHERE id = ? AND version = ?;

-- name: DeleteScheduleLines :exec
DELETE FROM schedule_lines WHERE schedule_id = ?;

-- name: InsertScheduleLine :exec
INSERT INTO schedule_lines (schedule_id, position, product_id, description, quantity_milli, unit_price_minor,
    discount_bp)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: DeleteSchedule :execrows
DELETE FROM schedules WHERE id = ?;

-- name: ClaimDueSchedules :many
SELECT * FROM schedules
WHERE status = 'active' AND next_run_at <= ?
ORDER BY next_run_at
LIMIT ?
FOR UPDATE SKIP LOCKED;

-- name: InsertScheduleRun :exec
INSERT INTO schedule_runs (schedule_id, period, invoice_id, at) VALUES (?, ?, ?, ?);

-- name: ListScheduleRuns :many
SELECT * FROM schedule_runs WHERE schedule_id = ? ORDER BY period DESC LIMIT ?;
