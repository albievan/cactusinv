-- +goose Up
-- The built-in Admin settings group (M57): its members change every setting and may view the
-- container secrets. Its access is computed in code (domain.AdminSettingsPermissions), so it
-- stores no permissions; it starts without members.
INSERT INTO access_groups (id, name, description, builtin, created_at, updated_at)
VALUES ('admin-settings', 'Admin settings', 'Every setting, the database and container status, and the container secrets (after their own password).', TRUE, UTC_TIMESTAMP(6), UTC_TIMESTAMP(6));

-- +goose Down
DELETE FROM access_group_members WHERE group_id = 'admin-settings';
DELETE FROM access_groups WHERE id = 'admin-settings';
