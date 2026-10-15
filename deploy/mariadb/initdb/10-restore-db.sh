#!/bin/bash
# Run once by the MariaDB image on a new, empty data directory (sourced by its entrypoint,
# which provides docker_process_sql): the staging database a restore loads a backup into
# (ADR-010), granted to the application user with only the privileges the restore needs.
# deploy.sh --install grants the same on existing installations (keep the lists identical).
if [[ ! "${MARIADB_DATABASE:-}" =~ ^[A-Za-z0-9_]{1,57}$ || ! "${MARIADB_USER:-}" =~ ^[A-Za-z0-9_]{1,80}$ ]]; then
  echo "10-restore-db.sh: MARIADB_DATABASE and MARIADB_USER must be plain names; no staging database created" >&2
  return 1
fi
docker_process_sql --database=mysql <<SQL
CREATE DATABASE IF NOT EXISTS \`${MARIADB_DATABASE}_restore\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, INDEX, REFERENCES, LOCK TABLES, TRIGGER
  ON \`${MARIADB_DATABASE}_restore\`.* TO '${MARIADB_USER}'@'%';
SQL
