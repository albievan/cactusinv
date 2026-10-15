# ADR-002 — MariaDB 11 with goose and sqlc

**Status:** accepted · **Date:** 2026-10-15

## Context
The brief mandates MariaDB. Queries should be type-checked and migrations versioned.

## Decision
- MariaDB **11 LTS**, InnoDB, `utf8mb4` / `utf8mb4_unicode_ci`, `sql_mode` strict.
- Driver `github.com/go-sql-driver/mysql` via `database/sql`, DSN with `parseTime=true`,
  `loc=UTC`, `time_zone='+00:00'`.
- `goose` SQL migrations (mysql dialect), embedded in the binary; applied by
  `cactusinv migrate up` (or `CACTUSINV_MIGRATE_ON_START=true` in dev).
- `sqlc` with the `mysql` engine generates query code. SQL is kept to the MySQL/MariaDB
  common subset so sqlc can parse it; MariaDB-only features (persistent generated
  columns) appear only in migrations.
- IDs are UUIDv7 in `CHAR(36) CHARACTER SET ascii COLLATE ascii_bin`: sortable by time,
  readable in logs, no `LAST_INSERT_ID` coupling.
- Worker claims use `SELECT … FOR UPDATE SKIP LOCKED` (MariaDB ≥ 10.6).
- Integration tests run against `mariadb:11` via `testcontainers-go`.

## Consequences
- No `RETURNING`-on-UPDATE, no partial indexes, no `pgcrypto`: a stored generated column
  (`default_marker = IF(is_default, 1, NULL)`, unique with `currency_code`) replaces a partial
  unique index for "one default bank account per currency". It must be numeric — MariaDB 11
  rejects string-valued `IF()` in generated columns (error 1901) — and spelled `STORED`, not
  `PERSISTENT`, so sqlc's MySQL parser accepts it.
- DDL is not transactional in MariaDB: a failing migration can leave a partial schema, so
  `make migrate-check` applies up → down → up against a fresh database.
- CHAR(36) costs some index size; irrelevant at this scale.
