-- name: LockAuditHead :one
SELECT last_hash FROM audit_head WHERE id = 1 FOR UPDATE;

-- name: SetAuditHead :exec
UPDATE audit_head SET last_hash = ? WHERE id = 1;

-- name: NextAuditSeq :one
-- Call with the head locked. seq is assigned explicitly (last + 1), not by AUTO_INCREMENT,
-- which InnoDB does not roll back: a rolled-back append would leave a gap in the chain.
SELECT CAST(COALESCE(MAX(seq), 0) + 1 AS SIGNED) AS next_seq FROM audit_log;

-- name: InsertAudit :exec
INSERT INTO audit_log (seq, at, actor_id, actor_name, action, entity_type, entity_id, summary, prev_hash, hash)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListAudit :many
SELECT * FROM audit_log
WHERE (sqlc.arg(entity_type) = '' OR entity_type = sqlc.arg(entity_type))
  AND (sqlc.arg(entity_id) = '' OR entity_id = sqlc.arg(entity_id))
  AND (sqlc.arg(actor) = '' OR actor_name LIKE CONCAT('%', sqlc.arg(actor), '%'))
  AND (sqlc.arg(action) = '' OR action LIKE CONCAT(sqlc.arg(action), '%'))
ORDER BY seq DESC
LIMIT ? OFFSET ?;

-- name: CountAudit :one
SELECT COUNT(*) FROM audit_log
WHERE (sqlc.arg(entity_type) = '' OR entity_type = sqlc.arg(entity_type))
  AND (sqlc.arg(entity_id) = '' OR entity_id = sqlc.arg(entity_id))
  AND (sqlc.arg(actor) = '' OR actor_name LIKE CONCAT('%', sqlc.arg(actor), '%'))
  AND (sqlc.arg(action) = '' OR action LIKE CONCAT(sqlc.arg(action), '%'));

-- name: ListAuditForEntity :many
SELECT * FROM audit_log WHERE entity_type = ? AND entity_id = ? ORDER BY seq;

-- name: ListAuditChain :many
SELECT * FROM audit_log WHERE seq > ? ORDER BY seq LIMIT ?;
