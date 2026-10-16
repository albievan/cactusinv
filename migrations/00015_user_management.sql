-- +goose Up
-- User management (ADR-011): users are disabled, never deleted; a password someone else
-- chose must be changed at the next sign-in; the last sign-in is shown in Settings → Users.
ALTER TABLE users
    ADD COLUMN disabled_at           DATETIME(6)  NULL,
    ADD COLUMN must_change_password  BOOLEAN      NOT NULL DEFAULT FALSE,
    ADD COLUMN last_login_at         DATETIME(6)  NULL;

-- The default administrator's password was chosen by the installation, not by its user: it
-- must be changed at the next sign-in. It is the user the audit trail records as created as
-- "default administrator …" (whatever CACTUSINV_DEFAULT_ADMIN_USER was), and — for
-- installations older than the audit trail — any login that is not an e-mail address,
-- which before this version only the default administrator could have
-- (`cactusinv admin create` requires one).
UPDATE users SET must_change_password = TRUE
WHERE email NOT LIKE '%@%'
   OR id IN (SELECT CONVERT(entity_id USING ascii) COLLATE ascii_bin FROM audit_log
             WHERE action = 'user.created' AND entity_type = 'user' AND summary LIKE 'default administrator %');

-- +goose Down
ALTER TABLE users
    DROP COLUMN last_login_at,
    DROP COLUMN must_change_password,
    DROP COLUMN disabled_at;
