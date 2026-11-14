-- +goose Up
-- Your account (M43): a user's first and last name and the e-mail address password reset
-- links go to (the login of users made before v1.6.0, such as "admin", is no address).
ALTER TABLE users
    ADD COLUMN first_name    VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN last_name     VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN email_address VARCHAR(254) NOT NULL DEFAULT '';

-- Forgot password (M43): a single-use link, valid for an hour, e-mailed through the outbox;
-- only its SHA-256 is stored. Not backed up (like sessions).
CREATE TABLE password_resets (
    token_hash  CHAR(64)    CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    user_id     CHAR(36)    CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at  DATETIME(6) NOT NULL,
    expires_at  DATETIME(6) NOT NULL,
    used_at     DATETIME(6) NULL,
    KEY password_resets_user (user_id, created_at),
    CONSTRAINT password_resets_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE email_outbox DROP CONSTRAINT email_outbox_kind;
ALTER TABLE email_outbox MODIFY COLUMN kind VARCHAR(20) NOT NULL DEFAULT 'invoice';
ALTER TABLE email_outbox ADD CONSTRAINT email_outbox_kind CHECK (kind IN ('invoice', 'statement', 'quote', 'password_reset'));

-- +goose Down
DELETE FROM email_outbox WHERE kind = 'password_reset';
ALTER TABLE email_outbox DROP CONSTRAINT email_outbox_kind;
ALTER TABLE email_outbox MODIFY COLUMN kind VARCHAR(10) NOT NULL DEFAULT 'invoice';
ALTER TABLE email_outbox ADD CONSTRAINT email_outbox_kind CHECK (kind IN ('invoice', 'statement', 'quote'));
DROP TABLE password_resets;
ALTER TABLE users DROP COLUMN email_address, DROP COLUMN last_name, DROP COLUMN first_name;
