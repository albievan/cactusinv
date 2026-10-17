-- name: GetSystemSettings :one
SELECT * FROM system_settings WHERE id = 1;

-- name: InsertSystemSettings :exec
INSERT INTO system_settings (id, session_idle_minutes, session_max_minutes, backup_host_dir, invoices_host_dir, version, updated_at)
VALUES (1, ?, ?, ?, ?, 1, ?);

-- name: UpdateSystemSettings :execrows
UPDATE system_settings SET session_idle_minutes = ?, session_max_minutes = ?, backup_host_dir = ?, invoices_host_dir = ?,
    version = version + 1, updated_at = ?
WHERE id = 1 AND version = ?;
