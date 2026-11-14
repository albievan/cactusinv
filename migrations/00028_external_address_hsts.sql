-- +goose Up
-- System Settings (M45): the external address customers and users reach this installation
-- on (links in e-mails), and whether browsers are told to use HTTPS only (HSTS). Both are
-- the installation's own, like the rest of system_settings (never backed up); and whether
-- the site is reached through Cloudflare (the visitor's address is then CF-Connecting-IP). A public
-- address saved under Settings → Quotes (M41) moves here.
ALTER TABLE system_settings
    ADD COLUMN external_url VARCHAR(300) NOT NULL DEFAULT '',
    ADD COLUMN hsts         BOOLEAN      NOT NULL DEFAULT FALSE,
    ADD COLUMN behind_cloudflare BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE system_settings s JOIN quote_settings q ON q.id = 1 SET s.external_url = q.public_url WHERE s.external_url = '';

-- +goose Down
ALTER TABLE system_settings DROP COLUMN behind_cloudflare, DROP COLUMN hsts, DROP COLUMN external_url;
