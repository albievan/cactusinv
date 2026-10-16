-- +goose Up
-- Granular access (ADR-012, Albie 2026-10-16: "add a customer but not edit; create an
-- invoice but not edit, issue or delete"): each "edit" permission is split into one per
-- action. A group keeps exactly what it could do: every old grant becomes all of the new
-- permissions it covered.
INSERT IGNORE INTO access_group_permissions (group_id, permission)
SELECT p.group_id, m.new_permission
FROM access_group_permissions p
JOIN (
              SELECT 'invoices.edit' AS old_permission, 'invoices.create' AS new_permission
    UNION ALL SELECT 'invoices.edit', 'invoices.issue'
    UNION ALL SELECT 'invoices.edit', 'invoices.delete'
    UNION ALL SELECT 'invoices.status', 'invoices.pay'
    UNION ALL SELECT 'invoices.status', 'invoices.overdue'
    UNION ALL SELECT 'invoices.status', 'invoices.void'
    UNION ALL SELECT 'customers.edit', 'customers.create'
    UNION ALL SELECT 'customers.edit', 'customers.delete'
    UNION ALL SELECT 'products.edit', 'products.create'
    UNION ALL SELECT 'products.edit', 'products.delete'
    UNION ALL SELECT 'schedules.edit', 'schedules.create'
    UNION ALL SELECT 'schedules.edit', 'schedules.pause'
    UNION ALL SELECT 'schedules.edit', 'schedules.run'
    UNION ALL SELECT 'schedules.edit', 'schedules.delete'
    UNION ALL SELECT 'emails.manage', 'emails.cancel'
    UNION ALL SELECT 'emails.manage', 'emails.retry'
    UNION ALL SELECT 'settings.edit', 'settings.business'
    UNION ALL SELECT 'settings.edit', 'settings.email'
    UNION ALL SELECT 'settings.edit', 'settings.currencies'
    UNION ALL SELECT 'settings.edit', 'settings.banks'
) m ON m.old_permission = p.permission;

-- invoices.edit, customers.edit, products.edit and schedules.edit stay with a narrower
-- meaning (edit only); these three are gone.
DELETE FROM access_group_permissions WHERE permission IN ('invoices.status', 'emails.manage', 'settings.edit');

-- +goose Down
-- Back to one permission per area: any of the split permissions gives the old one (so a
-- group never loses an action, though it may gain the area's others).
INSERT IGNORE INTO access_group_permissions (group_id, permission)
SELECT p.group_id, m.old_permission
FROM access_group_permissions p
JOIN (
              SELECT 'invoices.create' AS new_permission, 'invoices.edit' AS old_permission
    UNION ALL SELECT 'invoices.issue', 'invoices.edit'
    UNION ALL SELECT 'invoices.delete', 'invoices.edit'
    UNION ALL SELECT 'invoices.pay', 'invoices.status'
    UNION ALL SELECT 'invoices.overdue', 'invoices.status'
    UNION ALL SELECT 'invoices.void', 'invoices.status'
    UNION ALL SELECT 'customers.create', 'customers.edit'
    UNION ALL SELECT 'customers.delete', 'customers.edit'
    UNION ALL SELECT 'products.create', 'products.edit'
    UNION ALL SELECT 'products.delete', 'products.edit'
    UNION ALL SELECT 'schedules.create', 'schedules.edit'
    UNION ALL SELECT 'schedules.pause', 'schedules.edit'
    UNION ALL SELECT 'schedules.run', 'schedules.edit'
    UNION ALL SELECT 'schedules.delete', 'schedules.edit'
    UNION ALL SELECT 'emails.cancel', 'emails.manage'
    UNION ALL SELECT 'emails.retry', 'emails.manage'
    UNION ALL SELECT 'settings.business', 'settings.edit'
    UNION ALL SELECT 'settings.email', 'settings.edit'
    UNION ALL SELECT 'settings.currencies', 'settings.edit'
    UNION ALL SELECT 'settings.banks', 'settings.edit'
) m ON m.new_permission = p.permission;

DELETE FROM access_group_permissions WHERE permission IN (
    'invoices.create', 'invoices.issue', 'invoices.delete', 'invoices.pay', 'invoices.overdue', 'invoices.void',
    'customers.create', 'customers.delete', 'products.create', 'products.delete',
    'schedules.create', 'schedules.pause', 'schedules.run', 'schedules.delete',
    'emails.cancel', 'emails.retry',
    'settings.business', 'settings.email', 'settings.currencies', 'settings.banks');
