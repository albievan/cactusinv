-- Integration tests: the staging database a restore loads into (ADR-010), granted to the
-- application user. Run by the MariaDB entrypoint as root, after the user is created.
CREATE DATABASE IF NOT EXISTS cactusinv_restore CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
GRANT ALL PRIVILEGES ON cactusinv_restore.* TO 'cactusinv'@'%';
FLUSH PRIVILEGES;
