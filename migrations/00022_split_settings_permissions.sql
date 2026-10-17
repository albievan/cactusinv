-- +goose Up
-- One permission per page and action (M24, Albie 2026-10-17: "split each access into
-- individual access"): seeing the business, e-mail server, currencies and bank accounts is
-- split per page; adding, changing and deleting currencies and bank accounts per action;
-- the test e-mail, resuming schedules and verifying the audit log get their own. A group
-- keeps exactly what it could do: every old grant becomes all the new ones it covered.
INSERT IGNORE INTO access_group_permissions (group_id, permission)
SELECT p.group_id, m.new_permission
FROM access_group_permissions p
JOIN (
              SELECT 'settings.view' AS old_permission, 'settings.business.view' AS new_permission
    UNION ALL SELECT 'settings.view', 'settings.email.view'
    UNION ALL SELECT 'settings.view', 'settings.currencies.view'
    UNION ALL SELECT 'settings.view', 'settings.banks.view'
    UNION ALL SELECT 'settings.business', 'settings.business.view'
    UNION ALL SELECT 'settings.email', 'settings.email.view'
    UNION ALL SELECT 'settings.email', 'settings.email.test'
    UNION ALL SELECT 'settings.currencies', 'settings.currencies.view'
    UNION ALL SELECT 'settings.currencies', 'settings.currencies.create'
    UNION ALL SELECT 'settings.currencies', 'settings.currencies.edit'
    UNION ALL SELECT 'settings.banks', 'settings.currencies.view'
    UNION ALL SELECT 'settings.banks', 'settings.banks.view'
    UNION ALL SELECT 'settings.banks', 'settings.banks.create'
    UNION ALL SELECT 'settings.banks', 'settings.banks.edit'
    UNION ALL SELECT 'settings.banks', 'settings.banks.delete'
    UNION ALL SELECT 'schedules.pause', 'schedules.resume'
    UNION ALL SELECT 'audit.view', 'audit.verify'
) m ON m.old_permission = p.permission;

-- settings.business, settings.email, schedules.pause and audit.view stay with a narrower
-- meaning; these three are gone.
DELETE FROM access_group_permissions WHERE permission IN ('settings.view', 'settings.currencies', 'settings.banks');

-- +goose Down
-- Back to the combined permissions: any of the split ones gives the old one (so a group
-- never loses an action, though it may gain the others it was combined with).
INSERT IGNORE INTO access_group_permissions (group_id, permission)
SELECT p.group_id, m.old_permission
FROM access_group_permissions p
JOIN (
              SELECT 'settings.business.view' AS new_permission, 'settings.view' AS old_permission
    UNION ALL SELECT 'settings.email.view', 'settings.view'
    UNION ALL SELECT 'settings.currencies.view', 'settings.view'
    UNION ALL SELECT 'settings.banks.view', 'settings.view'
    UNION ALL SELECT 'settings.email.test', 'settings.email'
    UNION ALL SELECT 'settings.currencies.create', 'settings.currencies'
    UNION ALL SELECT 'settings.currencies.edit', 'settings.currencies'
    UNION ALL SELECT 'settings.banks.create', 'settings.banks'
    UNION ALL SELECT 'settings.banks.edit', 'settings.banks'
    UNION ALL SELECT 'settings.banks.delete', 'settings.banks'
    UNION ALL SELECT 'schedules.resume', 'schedules.pause'
    UNION ALL SELECT 'audit.verify', 'audit.view'
) m ON m.new_permission = p.permission;

DELETE FROM access_group_permissions WHERE permission IN (
    'settings.business.view', 'settings.email.view', 'settings.email.test',
    'settings.currencies.view', 'settings.currencies.create', 'settings.currencies.edit',
    'settings.banks.view', 'settings.banks.create', 'settings.banks.edit', 'settings.banks.delete',
    'schedules.resume', 'audit.verify',
    'settings.export', 'settings.import', 'settings.system.view', 'settings.system.edit');
