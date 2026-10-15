-- name: GetSMTPSettings :one
SELECT * FROM smtp_settings WHERE id = 1;

-- name: InsertSMTPSettings :exec
INSERT INTO smtp_settings (id, enabled, host, port, username, password_enc, from_address, tls_mode, version, updated_at)
VALUES (1, ?, ?, ?, ?, ?, ?, ?, 1, ?);

-- name: UpdateSMTPSettings :execrows
UPDATE smtp_settings SET enabled = ?, host = ?, port = ?, username = ?, password_enc = ?, from_address = ?,
    tls_mode = ?, version = version + 1, updated_at = ?
WHERE id = 1 AND version = ?;
