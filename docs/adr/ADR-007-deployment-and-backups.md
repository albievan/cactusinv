# ADR-007 — One deploy script for Docker, k3s and Kubernetes; scheduled backups

**Status:** accepted · **Date:** 2026-10-15

## Decision
`deploy.sh ACTION TARGET [--ssh-access user@host] [--yes]`
- ACTION `--install` (also upgrades), `--uninstall` (keeps data), `--delete-uninstall`
  (removes data after confirmation), plus `--status`, `--backup-now`, `--restore FILE`.
- TARGET `--dockerdesktop` (Docker Desktop or Docker Engine, via `docker-compose.yml`),
  `--k3s` (image built for the node's architecture and imported with `k3s ctr`, no registry),
  `--k8s` (image pushed to `K8S_REGISTRY`, applied with `kubectl`).
- `--ssh-access`: Docker targets copy the project to `/opt/cactusinv` on the host and run
  there; k3s/k8s targets build locally and run `kubectl` / `k3s ctr` over SSH.
- `.env` provides every default: host volume mappings, ports, image, storage sizes, backup
  schedule. Compose reads it natively; `deploy.sh` parses it without executing it.

**Backups:** `deploy/backup/backup.sh` runs `mariadb-dump --single-transaction` (consistent,
non-blocking), gzips atomically into the backups volume and prunes after
`BACKUP_RETENTION_DAYS`. Docker runs it as a long-lived `backup` service at `BACKUP_TIMES`
(daily, comma-separated); Kubernetes runs it as a `CronJob` on `BACKUP_CRON`.

## Consequences
The same image and data model on every target; data lives on host bind mounts (Docker) or
PersistentVolumeClaims (k3s/k8s). The app Deployment uses one replica because the invoices
volume is ReadWriteOnce; with a ReadWriteMany class it can scale out.

## Amendment — backups move into the application (2026-10-15, ADR-010)
- The `backup` sidecar and the Kubernetes `CronJob` running `deploy/backup/backup.sh` are
  retired (M14.18): the application takes encrypted backups itself at
  `CACTUSINV_BACKUP_TIMES`, prunes after `CACTUSINV_BACKUP_RETENTION_DAYS`, and writes them
  to the backups volume, which is now mounted into the app container (`/data/backups`).
- `deploy.sh --backup-now` runs `cactusinv backup create` in the app container;
  `--restore FILE` restores an `.age` backup through the application (staging, preview,
  apply). A legacy `.sql.gz` dump is still restored the old way for one release, with a
  warning, followed by `cactusinv backup note-legacy-restore FILE` to record it in the
  audit trail.
- `--install` creates the staging database `<db>_restore` and grants it to the app user
  (idempotent, with the root secret), and generates the `backup_key` secret (ADR-006).
