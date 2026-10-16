-- name: ListAccessGroups :many
SELECT * FROM access_groups ORDER BY builtin DESC, name, id;

-- name: GetAccessGroup :one
SELECT * FROM access_groups WHERE id = ?;

-- name: LockAccessGroup :one
SELECT * FROM access_groups WHERE id = ? FOR UPDATE;

-- name: InsertAccessGroup :exec
INSERT INTO access_groups (id, name, description, builtin, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateAccessGroup :execrows
UPDATE access_groups SET name = ?, description = ?, updated_at = ? WHERE id = ? AND builtin = FALSE;

-- name: DeleteAccessGroup :execrows
DELETE FROM access_groups WHERE id = ? AND builtin = FALSE;

-- name: ListAccessGroupPermissions :many
SELECT group_id, permission FROM access_group_permissions ORDER BY group_id, permission;

-- name: DeleteAccessGroupPermissions :exec
DELETE FROM access_group_permissions WHERE group_id = ?;

-- name: InsertAccessGroupPermission :exec
INSERT INTO access_group_permissions (group_id, permission) VALUES (?, ?);

-- name: ListAccessGroupMembers :many
SELECT group_id, user_id FROM access_group_members ORDER BY group_id, user_id;

-- name: DeleteAccessGroupMembers :exec
DELETE FROM access_group_members WHERE group_id = ?;

-- name: InsertAccessGroupMember :exec
INSERT INTO access_group_members (group_id, user_id) VALUES (?, ?);

-- name: DeleteUserAccessGroups :exec
DELETE FROM access_group_members WHERE user_id = ?;

-- name: ListUserAccessGroupIDs :many
SELECT group_id FROM access_group_members WHERE user_id = ? ORDER BY group_id;

-- name: ListUserGroupPermissions :many
SELECT m.group_id, p.permission FROM access_group_members m
LEFT JOIN access_group_permissions p ON p.group_id = m.group_id
WHERE m.user_id = ? ORDER BY m.group_id, p.permission;

-- LockActiveAdministrators locks the Administrators memberships of active users (and those
-- users' rows): every change to who administers serialises on it (ADR-012).
-- name: LockActiveAdministrators :many
SELECT m.user_id FROM access_group_members m JOIN users u ON u.id = m.user_id
WHERE m.group_id = 'administrators' AND u.disabled_at IS NULL ORDER BY m.user_id FOR UPDATE;
