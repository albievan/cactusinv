-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ?;

-- name: GetUser :one
SELECT * FROM users WHERE id = ?;

-- name: InsertUser :exec
INSERT INTO users (id, email, password_hash, created_at, must_change_password) VALUES (?, ?, ?, ?, ?);

-- name: UpdateUserPassword :execrows
UPDATE users SET password_hash = ?, must_change_password = ? WHERE id = ?;

-- name: LockUser :one
SELECT * FROM users WHERE id = ? FOR UPDATE;

-- name: ListUsers :many
SELECT * FROM users ORDER BY email, id;

-- name: SetUserDisabled :execrows
UPDATE users SET disabled_at = ? WHERE id = ?;

-- name: TouchUserLogin :execrows
UPDATE users SET last_login_at = ? WHERE id = ?;

-- LockActiveUsers locks every active user row: disabling serialises on it, so the last
-- active user can never be disabled by two requests at once (ADR-011).
-- name: LockActiveUsers :many
SELECT id FROM users WHERE disabled_at IS NULL ORDER BY id FOR UPDATE;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?;

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

-- name: DeleteAllSessions :exec
DELETE FROM sessions;

-- name: DeleteAllIdempotencyKeys :exec
DELETE FROM idempotency_keys;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = ? WHERE token_hash = ?;

-- name: UpdateUserProfile :execrows
UPDATE users SET first_name = ?, last_name = ?, email_address = ? WHERE id = ?;

-- name: InsertPasswordReset :exec
INSERT INTO password_resets (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?);

-- name: LockPasswordReset :one
SELECT * FROM password_resets WHERE token_hash = ? FOR UPDATE;

-- name: GetPasswordReset :one
SELECT * FROM password_resets WHERE token_hash = ?;

-- name: LatestPasswordReset :one
SELECT created_at FROM password_resets WHERE user_id = ? ORDER BY created_at DESC LIMIT 1;

-- name: UsePasswordResets :exec
UPDATE password_resets SET used_at = ? WHERE user_id = ? AND used_at IS NULL;

-- name: DeleteOldPasswordResets :exec
DELETE FROM password_resets WHERE expires_at < ?;

-- name: UpdateUserLogin :execrows
UPDATE users SET email = ? WHERE id = ?;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = ?;
