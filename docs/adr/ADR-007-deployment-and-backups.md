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
  audit trail. (That path was removed in v1.22.0, M40.)
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

## Amendment — upgrades (2026-11-08, M27)
- **The app never upgrades itself.** It runs distroless, non-root, without the Docker socket or
  cluster rights; giving it those to pull and restart its own image would hand the web process
  control of the host. `./deploy.sh --upgrade` does it from the checkout the install came from;
  System Settings → Check for updates (on demand only; `CACTUSINV_UPDATE_URL`, `off` to disable)
  just says a newer published version exists and prints the command.
- **Forward only, backup first, new script installs.** The upgrade fast-forwards to the branch's
  upstream or `--to REF` (refusing anything that is not a descendant: a downgrade needs a
  restore), takes a backup before changing anything, and runs the *new* `deploy.sh --install`
  (each step is the script run again, before `.env` is read, so every step reads it afresh). It
  then checks the running version is the new one.
- **Rollback undoes migrations with the newer binary.** A restore cannot take the live schema
  back (ADR-010: a backup is migrated *up* to the binary's head), and goose would start an older
  binary on a newer schema without complaint. So `mariadb.Migrate` now refuses a database whose
  schema is newer than the binary's (`SchemaNewerError`: the app does not start), and a failed
  upgrade runs the new image's `cactusinv migrate down-to <old head>` (Docker: a one-off
  container with the app stopped; k3s/k8s: in the running pod, else the script stops and names
  the backup), then resets the checkout and reinstalls the old version. Down migrations are
  already required and tested for every migration (CLAUDE.md §3).
- Remote Docker (`--ssh-access`) passes the build number to the copy, which has no `.git`.
