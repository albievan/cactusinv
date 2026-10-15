-- name: GetSystemState :one
SELECT * FROM system_state WHERE id = 1;

-- name: LockSystemState :one
SELECT * FROM system_state WHERE id = 1 FOR UPDATE;

-- name: AutomationHeldShared :one
-- Read by the dispatcher's and schedule runner's claims: a restore holds the row
-- exclusively while it swaps the data, and automation_held stays set afterwards.
SELECT automation_held FROM system_state WHERE id = 1 LOCK IN SHARE MODE;

-- name: SaveSystemState :execrows
UPDATE system_state SET automation_held = ?, held_at = ?, held_by = ?, held_reason = ?, held_restore_id = ?, restore_id = ?,
    restore_expires_at = ?, restore_source = ?, restore_sha256 = ?, restore_staged_by = ?, restore_header = ?
WHERE id = 1;

-- name: InsertBackupRun :exec
INSERT INTO backup_runs (slot, name, result, error, started_at, finished_at) VALUES (?, ?, ?, ?, ?, ?);

-- name: FinishBackupRun :execrows
UPDATE backup_runs SET name = ?, result = ?, error = ?, finished_at = ? WHERE slot = ?;

-- name: LatestBackupRun :one
SELECT * FROM backup_runs ORDER BY slot DESC LIMIT 1;
