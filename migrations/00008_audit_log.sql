-- +goose Up
-- The append-only, hash-chained audit trail (see domain.AuditEntry). Rows are never
-- updated or deleted: triggers in 00011 reject both, and the hash chain detects any
-- change made around them.
CREATE TABLE audit_log (
    seq          BIGINT         NOT NULL AUTO_INCREMENT PRIMARY KEY,
    at           DATETIME(6)    NOT NULL,
    actor_id     VARCHAR(64)    NOT NULL,
    actor_name   VARCHAR(254)   NOT NULL,
    action       VARCHAR(64)    NOT NULL,
    entity_type  VARCHAR(32)    NOT NULL,
    entity_id    VARCHAR(64)    NOT NULL,
    summary      VARCHAR(1000)  NOT NULL,
    prev_hash    CHAR(64)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    hash         CHAR(64)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    UNIQUE KEY audit_log_hash (hash),
    KEY audit_log_entity (entity_type, entity_id, seq),
    KEY audit_log_at (at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- The chain head: one row, locked by every append so entries are chained in order even
-- when several writers append at once.
CREATE TABLE audit_head (
    id         TINYINT   NOT NULL PRIMARY KEY,
    last_hash  CHAR(64)  CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    CONSTRAINT audit_head_single_row CHECK (id = 1)
) ENGINE=InnoDB;
INSERT INTO audit_head (id, last_hash) VALUES (1, '');

-- +goose Down
DROP TABLE audit_head;
DROP TABLE audit_log;
