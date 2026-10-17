-- +goose Up
-- System Settings (M23): the session timeouts and the host folders the deployment maps to
-- the container's backup and invoice folders. Installation-specific, like system_state: never
-- backed up or restored. No row until first saved (defaults apply).
CREATE TABLE system_settings (
    id                    TINYINT        NOT NULL PRIMARY KEY,
    session_idle_minutes  INT            NOT NULL,
    session_max_minutes   INT            NOT NULL,
    backup_host_dir       VARCHAR(1024)  NOT NULL,
    invoices_host_dir     VARCHAR(1024)  NOT NULL,
    version               INT            NOT NULL,
    updated_at            DATETIME(6)    NOT NULL,
    CONSTRAINT system_settings_single_row CHECK (id = 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- The idle timeout needs to know when a session was last used.
ALTER TABLE sessions ADD COLUMN last_seen_at DATETIME(6) NULL;

-- +goose Down
ALTER TABLE sessions DROP COLUMN last_seen_at;
DROP TABLE system_settings;
