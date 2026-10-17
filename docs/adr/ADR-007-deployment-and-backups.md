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

## Amendment — k3s pods and volume claims (2026-10-16, M20)
- **No pod mounts the service-account token** (`automountServiceAccountToken: false` on
  every pod spec). None of them calls the Kubernetes API, and the token's mount point
  `/var/run/secrets/kubernetes.io/serviceaccount` lies inside our read-only secrets volume at
  `/run/secrets` (`/var/run` is `/run`): runc could not create it and MariaDB never started on
  k3s (`mkdirat …/run/secrets/kubernetes.io: read-only file system`). Docker Compose has no
  token, so it never showed there. Moving our secrets elsewhere would have changed every
  `*_FILE` path and the app's `CACTUSINV_SECRETS_DIR`; dropping a credential nobody uses is
  also the smaller attack surface.
- **On k3s the image is imported, never pulled**: built without provenance/SBOM manifests,
  `docker save --platform linux/<node arch>`, imported with `k3s ctr` (root or `sudo -n`),
  checked on the node, and run with `imagePullPolicy: Never` (k8s: `IfNotPresent`).
- **An existing volume claim keeps its size.** A claim cannot shrink, so `deploy.sh` reads
  the current request of `mariadb-data`, `invoices` and `backups` and renders that instead of
  `K8S_*_STORAGE`; the `.env` sizes apply to new installs (grow a claim by hand where the
  storage class allows it).
- A rollout not ready within five minutes prints pods, claims, events and the current and
  previous container logs before the script stops.
