-- +goose Up
-- Outgoing mail server configured in the app (Settings → E-mail server). The password is
-- AES-256-GCM encrypted with the app_key secret; it is never stored in clear.
CREATE TABLE smtp_settings (
    id            TINYINT        NOT NULL PRIMARY KEY,
    enabled       BOOLEAN        NOT NULL,
    host          VARCHAR(255)   NOT NULL,
    port          INT            NOT NULL,
    username      VARCHAR(254)   NOT NULL,
    password_enc  VARBINARY(512) NULL,
    from_address  VARCHAR(320)   NOT NULL,
    tls_mode      VARCHAR(10)    NOT NULL,
    version       INT            NOT NULL,
    updated_at    DATETIME(6)    NOT NULL,
    CONSTRAINT smtp_settings_single_row CHECK (id = 1),
    CONSTRAINT smtp_settings_tls CHECK (tls_mode IN ('starttls', 'tls', 'none'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE smtp_settings;
