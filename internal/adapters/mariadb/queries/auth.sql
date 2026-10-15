-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ?;

-- name: GetUser :one
SELECT * FROM users WHERE id = ?;

-- name: InsertUser :exec
INSERT INTO users (id, email, password_hash, created_at) VALUES (?, ?, ?, ?);

-- name: UpdateUserPassword :execrows
UPDATE users SET password_hash = ? WHERE id = ?;

-- name: InsertSession :exec
INSERT INTO sessions (token_hash, user_id, expires_at, created_at) VALUES (?, ?, ?, ?);

-- name: GetSession :one
SELECT * FROM sessions WHERE token_hash = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= ?;

-- name: InsertIdempotencyKey :exec
INSERT INTO idempotency_keys (idem_key, fingerprint, status, body, created_at) VALUES (?, ?, ?, ?, ?);

-- name: GetIdempotencyKey :one
SELECT * FROM idempotency_keys WHERE idem_key = ?;

-- name: CompleteIdempotencyKey :execrows
UPDATE idempotency_keys SET status = ?, body = ? WHERE idem_key = ?;

-- name: DeleteIdempotencyKey :exec
DELETE FROM idempotency_keys WHERE idem_key = ?;

-- name: DeleteIdempotencyKeysBefore :exec
DELETE FROM idempotency_keys WHERE created_at < ?;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;
