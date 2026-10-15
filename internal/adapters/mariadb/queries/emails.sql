-- name: InsertEmail :exec
INSERT INTO email_outbox (id, kind, invoice_id, customer_id, to_addrs, cc_addrs, subject, body, status, attempts,
    send_at, next_attempt_at, last_error, sent_at, idempotency_key, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetEmail :one
SELECT * FROM email_outbox WHERE id = ?;

-- name: GetEmailByIdempotencyKey :one
SELECT * FROM email_outbox WHERE idempotency_key = ?;

-- name: UpdateEmail :execrows
UPDATE email_outbox SET to_addrs = ?, cc_addrs = ?, subject = ?, body = ?, status = ?, attempts = ?,
    next_attempt_at = ?, last_error = ?, sent_at = ?, updated_at = ?
WHERE id = ?;

-- name: ListEmails :many
SELECT * FROM email_outbox
WHERE (sqlc.arg(status) = '' OR status = sqlc.arg(status))
  AND (sqlc.arg(invoice_id) = '' OR invoice_id = sqlc.arg(invoice_id))
  AND (sqlc.arg(customer_id) = '' OR customer_id = sqlc.arg(customer_id))
ORDER BY created_at DESC, id DESC
LIMIT ? OFFSET ?;

-- name: CountEmails :one
SELECT COUNT(*) FROM email_outbox
WHERE (sqlc.arg(status) = '' OR status = sqlc.arg(status))
  AND (sqlc.arg(invoice_id) = '' OR invoice_id = sqlc.arg(invoice_id))
  AND (sqlc.arg(customer_id) = '' OR customer_id = sqlc.arg(customer_id));

-- name: ClaimDueEmails :many
SELECT * FROM email_outbox
WHERE (status = 'pending' AND next_attempt_at <= sqlc.arg(now))
   OR (status = 'sending' AND updated_at <= sqlc.arg(stale_before))
ORDER BY next_attempt_at
LIMIT ?
FOR UPDATE SKIP LOCKED;

-- name: OutboxStats :one
SELECT CAST(COUNT(*) AS SIGNED) AS pending, MIN(next_attempt_at) AS oldest
FROM email_outbox WHERE status = 'pending';

-- name: CountSendingEmails :one
SELECT COUNT(*) FROM email_outbox WHERE status = 'sending' AND updated_at > ?;
