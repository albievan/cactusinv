-- +goose Up
-- Installation state that is never backed up or restored (ADR-010): whether automation is
-- held after a restore, and the restore currently staged in the staging database.
CREATE TABLE system_state (
    id                  TINYINT       NOT NULL PRIMARY KEY,
    automation_held     BOOLEAN       NOT NULL,
    held_at             DATETIME(6)   NULL,
    held_by             VARCHAR(254)  NOT NULL,
    held_reason         VARCHAR(500)  NOT NULL,
    restore_id          CHAR(36)      CHARACTER SET ascii COLLATE ascii_bin NULL,
    restore_expires_at  DATETIME(6)   NULL,
    restore_source      VARCHAR(200)  NOT NULL,
    restore_sha256      CHAR(64)      CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    restore_staged_by   CHAR(36)      CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    restore_header      MEDIUMTEXT    NULL,
    CONSTRAINT system_state_single_row CHECK (id = 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
INSERT INTO system_state (id, automation_held, held_by, held_reason, restore_source, restore_sha256, restore_staged_by)
VALUES (1, FALSE, '', '', '', '', '');

-- One row per scheduled backup slot: the slot is unique, so it runs once even with several
-- replicas.
CREATE TABLE backup_runs (
    slot         DATETIME(6)    NOT NULL PRIMARY KEY,
    name         VARCHAR(100)   NOT NULL,
    result       VARCHAR(10)    NOT NULL,
    error        VARCHAR(1000)  NOT NULL,
    started_at   DATETIME(6)    NOT NULL,
    finished_at  DATETIME(6)    NULL,
    CONSTRAINT backup_runs_result CHECK (result IN ('running', 'ok', 'failed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE backup_runs;
DROP TABLE system_state;
