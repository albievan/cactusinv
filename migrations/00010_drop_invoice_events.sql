-- +goose Up
-- Invoice history now lives in audit_log (copied there by migration 9).
DROP TABLE invoice_events;

-- +goose Down
CREATE TABLE invoice_events (
    id          CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    invoice_id  CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    type        VARCHAR(32)    NOT NULL,
    detail      VARCHAR(1000)  NOT NULL,
    at          DATETIME(6)    NOT NULL,
    KEY invoice_events_invoice (invoice_id, at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
