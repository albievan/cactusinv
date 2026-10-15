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
