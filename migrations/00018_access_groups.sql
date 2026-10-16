-- +goose Up
-- Access groups (ADR-012): a user may do what their groups allow. The built-in
-- Administrators group has every permission (none are stored for it) and administration.
-- On upgrade only the default administrator joins it (Albie, 2026-10-16); every other user
-- has no access until an administrator puts them in a group.
CREATE TABLE access_groups (
    id           CHAR(36)      CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    name         VARCHAR(80)   NOT NULL,
    description  VARCHAR(500)  NOT NULL DEFAULT '',
    builtin      BOOLEAN       NOT NULL DEFAULT FALSE,
    created_at   DATETIME(6)   NOT NULL,
    updated_at   DATETIME(6)   NOT NULL,
    UNIQUE KEY access_groups_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE access_group_permissions (
    group_id    CHAR(36)     CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    permission  VARCHAR(64)  CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    PRIMARY KEY (group_id, permission),
    CONSTRAINT access_group_permissions_group FOREIGN KEY (group_id) REFERENCES access_groups (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE access_group_members (
    group_id  CHAR(36)  CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    user_id   CHAR(36)  CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    PRIMARY KEY (group_id, user_id),
    KEY access_group_members_user (user_id),
    CONSTRAINT access_group_members_group FOREIGN KEY (group_id) REFERENCES access_groups (id) ON DELETE CASCADE,
    CONSTRAINT access_group_members_user FOREIGN KEY (user_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO access_groups (id, name, description, builtin, created_at, updated_at)
VALUES ('administrators', 'Administrators', 'Everything, including users, groups, backups and restore.', TRUE, UTC_TIMESTAMP(6), UTC_TIMESTAMP(6));

-- The default administrator is found as in 00015: the user the audit trail records as
-- created as "default administrator …", or a login that is not an e-mail address.
INSERT INTO access_group_members (group_id, user_id)
SELECT 'administrators', id FROM users
WHERE email NOT LIKE '%@%'
   OR id IN (SELECT CONVERT(entity_id USING ascii) COLLATE ascii_bin FROM audit_log
             WHERE action = 'user.created' AND entity_type = 'user' AND summary LIKE 'default administrator %');

-- Never leave an installation without an active administrator: if none was found (or the
-- one found is disabled), the oldest active user becomes one.
INSERT INTO access_group_members (group_id, user_id)
SELECT 'administrators', u.id FROM users u
WHERE u.disabled_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM access_group_members m JOIN users a ON a.id = m.user_id WHERE a.disabled_at IS NULL)
ORDER BY u.created_at, u.id LIMIT 1;

-- +goose Down
DROP TABLE access_group_members;
DROP TABLE access_group_permissions;
DROP TABLE access_groups;
