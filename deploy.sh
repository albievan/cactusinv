#!/usr/bin/env bash
# cactusInv deployment — one script for Docker Desktop / Docker Engine, k3s and Kubernetes.
#
#   ./deploy.sh ACTION TARGET [--ssh-access user@host] [--yes]
#
# ACTION
#   --install            build and start (or upgrade) cactusInv
#   --uninstall          stop and remove cactusInv, KEEPING the database, invoices and backups
#   --delete-uninstall   remove cactusInv AND DELETE all its data (asks for confirmation)
#   --status             show what is running
#   --backup-now         take an encrypted backup now (stored in the backups volume)
#   --restore FILE       restore a backup: a cactusinv-*.age file (a stored name or a path;
#                        passphrase-encrypted ones read $CACTUSINV_BACKUP_PASSPHRASE), shown
#                        first and confirmed by typing RESTORE; or a legacy cactusinv-*.sql.gz
#                        dump (checked, a safety backup taken, then the database replaced)
#
# TARGET
#   --dockerdesktop      Docker Desktop, or Docker Engine on a Linux host (docker compose)
#   --k3s                a k3s node (image imported straight into k3s; no registry needed)
#   --k8s                any Kubernetes cluster (image pushed to K8S_REGISTRY from .env)
#
# OPTIONS
#   --ssh-access user@host   deploy to a remote machine over SSH, e.g. root@192.0.2.10
#   --yes                    do not ask for confirmation (for --delete-uninstall)
#   -h, --help               show this help
#
# Settings and default volume mappings come from .env next to this script.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

ACTION="" TARGET="" SSH_HOST="" ASSUME_YES=0 RESTORE_FILE=""
REMOTE_DIR="${CACTUSINV_REMOTE_DIR:-/opt/cactusinv}"

die() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
info() { printf '\033[36m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[33mwarning:\033[0m %s\n' "$*" >&2; }
ok() { printf '\033[32m✔\033[0m %s\n' "$*"; }
usage() { sed -n '2,/^set -euo/p' "$0" | sed '$d; s/^# \{0,1\}//'; exit "${1:-0}"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --install | --uninstall | --delete-uninstall | --status | --backup-now)
      [[ -z "$ACTION" ]] || die "choose one action"; ACTION="${1#--}" ;;
    --restore)
      [[ -z "$ACTION" ]] || die "choose one action"; ACTION=restore; RESTORE_FILE="${2:-}"; shift
      [[ -n "$RESTORE_FILE" ]] || die "--restore needs a backup file name" ;;
    --dockerdesktop | --docker | --k3s | --k8s)
      [[ -z "$TARGET" ]] || die "choose one target"; TARGET="${1#--}"; [[ "$TARGET" == docker ]] && TARGET=dockerdesktop ;;
    --ssh-access) SSH_HOST="${2:-}"; shift; [[ -n "$SSH_HOST" && "$SSH_HOST" != --* ]] || die "--ssh-access needs user@host" ;;
    --yes | -y) ASSUME_YES=1 ;;
    -h | --help) usage 0 ;;
    *) die "unknown argument: $1 (see --help)" ;;
  esac
  shift
done
[[ -n "$ACTION" ]] || usage 1
[[ -n "$TARGET" ]] || die "choose a target: --dockerdesktop, --k3s or --k8s"

# ---------------------------------------------------------------------------------------
# .env — parsed line by line (KEY=value, optional quotes), never executed.
# ---------------------------------------------------------------------------------------
load_env() {
  [[ -f .env ]] || die ".env not found in $ROOT"
  local line key val
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" =~ ^[[:space:]]*(#|$) ]] && continue
    [[ "$line" =~ ^([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]] || continue
    key="${BASH_REMATCH[1]}" val="${BASH_REMATCH[2]}"
    if [[ "$val" =~ ^\"(.*)\"$ || "$val" =~ ^\'(.*)\'$ ]]; then val="${BASH_REMATCH[1]}"; fi
    # Explicit environment variables win over .env.
    [[ -n "${!key+x}" ]] || export "$key=$val"
  done <.env
}
load_env

# Names that end up in SQL (database, user) are restricted to plain identifiers.
[[ "${MARIADB_DATABASE:-}" =~ ^[A-Za-z0-9_]{1,57}$ ]] || die "MARIADB_DATABASE must be 1-57 letters, digits or _"
[[ "${MARIADB_USER:-}" =~ ^[A-Za-z0-9_]{1,80}$ ]] || die "MARIADB_USER must be 1-80 letters, digits or _"

# confirm_word asks for a word (RESTORE) before replacing data; --yes skips it.
# Without a terminal it fails (the caller cleans up), unless --yes was given.
confirm_word() {
  [[ $ASSUME_YES -eq 1 ]] && return 0
  if [[ ! -t 0 ]]; then
    echo "$2 Re-run with --yes to confirm non-interactively." >&2
    return 1
  fi
  read -r -p "$2 Type '$1' to continue: " answer
  [[ "$answer" == "$1" ]]
}

confirm() {
  [[ $ASSUME_YES -eq 1 ]] && return 0
  [[ -t 0 ]] || die "$1 — re-run with --yes to confirm non-interactively"
  read -r -p "$1 Type 'delete' to continue: " answer
  [[ "$answer" == delete ]] || die "aborted"
}

ssh_run() { ssh -o BatchMode=yes -o ConnectTimeout=15 "$SSH_HOST" "$@"; }

# Release version (VERSION file) and build number (commit count) shown in the app.
export CACTUSINV_VERSION CACTUSINV_BUILD
CACTUSINV_VERSION="$(tr -d '[:space:]' <"$ROOT/VERSION" 2>/dev/null || echo dev)"
CACTUSINV_BUILD="$(git -C "$ROOT" rev-list --count HEAD 2>/dev/null || echo 0)"

image_tag() {
  local v
  v="$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || date +%Y%m%d%H%M%S)"
  # A dirty tree gets a new tag on every build, so the pods roll to the new image.
  [[ "$v" == *-dirty ]] && v="${v}-$(date +%Y%m%d%H%M%S)"
  echo "${CACTUSINV_IMAGE%%:*}:${v}"
}

# ---------------------------------------------------------------------------------------
# Docker (Docker Desktop / Docker Engine)
# ---------------------------------------------------------------------------------------
compose() { docker compose --project-directory "$ROOT" -f "$ROOT/docker-compose.yml" "$@"; }

# 32 random bytes as 64 hex characters (the AES-256 app_key).
random_key() { od -An -N32 -tx1 /dev/urandom | tr -d ' \n'; }

random_secret() { od -An -N24 -tx1 /dev/urandom | tr -d ' \n'; }

ensure_secrets_dir() {
  mkdir -p "$SECRETS_DIR"
  chmod 700 "$SECRETS_DIR"
  local s
  for s in db_password db_root_password; do
    if [[ ! -s "$SECRETS_DIR/$s" ]]; then
      random_secret >"$SECRETS_DIR/$s"
      ok "generated $SECRETS_DIR/$s"
    fi
  done
  [[ -f "$SECRETS_DIR/smtp_password" ]] || : >"$SECRETS_DIR/smtp_password"
  if [[ ! -s "$SECRETS_DIR/app_key" ]]; then
    random_key >"$SECRETS_DIR/app_key"
    ok "generated $SECRETS_DIR/app_key (encrypts the SMTP password stored in the app)"
  fi
  # Placeholder until the image can generate the key (ensure_backup_key).
  [[ -f "$SECRETS_DIR/backup_key" ]] || : >"$SECRETS_DIR/backup_key"
  # MariaDB and the app run as other users and must read the files.
  chmod 644 "$SECRETS_DIR"/*
}

backup_key_notice() {
  echo "    backup_key:    keep a copy OFF this machine, apart from the backups - without it the"
  echo "                   stored backups cannot be restored (age -d -i backup_key FILE opens one)"
}

# ensure_backup_key generates the age identity stored backups are encrypted to (ADR-006),
# with the image just built, on new and older installations alike.
ensure_backup_key() {
  [[ -s "$SECRETS_DIR/backup_key" ]] && return 0
  local tmp="$SECRETS_DIR/.backup_key.new"
  docker run --rm --entrypoint /cactusinv "$CACTUSINV_IMAGE" backup keygen >"$tmp"
  mv "$tmp" "$SECRETS_DIR/backup_key"
  chmod 644 "$SECRETS_DIR/backup_key"
  ok "generated $SECRETS_DIR/backup_key (encrypts the stored backups)"
  backup_key_notice
}

# The privileges the restore stager needs on the staging database (migrations, loading,
# LOCK TABLES … READ during the swap) — no more. Kept identical to
# deploy/mariadb/initdb/10-restore-db.sh.
STAGING_PRIVILEGES='SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, INDEX, REFERENCES, LOCK TABLES, TRIGGER'

# The restore staging database and its grant (ADR-010), as MariaDB root. Idempotent: an
# older, wider grant (ALL) is narrowed to the list. The root password goes through the
# environment of the client, never its command line.
staging_grant_sql() {
  local db="${MARIADB_DATABASE}_restore" to="'${MARIADB_USER}'@'%'"
  printf 'CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;\n' "$db"
  printf 'GRANT %s ON `%s`.* TO %s;\n' "$STAGING_PRIVILEGES" "$db" "$to"
  printf 'REVOKE ALL PRIVILEGES ON `%s`.* FROM %s;\n' "$db" "$to"
  printf 'GRANT %s ON `%s`.* TO %s;\n' "$STAGING_PRIVILEGES" "$db" "$to"
}
# Run inside the MariaDB container: as root (only to drop and recreate the database), and
# as the application user (everything a dump does).
MARIADB_ROOT_SQL='MYSQL_PWD="$(cat /run/secrets/db_root_password)" exec mariadb -uroot'
MARIADB_APP_SQL="MYSQL_PWD=\"\$(cat /run/secrets/db_password)\" exec mariadb -u'${MARIADB_USER}' '${MARIADB_DATABASE}'"

docker_preflight() {
  command -v docker >/dev/null || die "docker is not installed"
  docker info >/dev/null 2>&1 || die "the Docker daemon is not running (start Docker Desktop)"
  docker compose version >/dev/null 2>&1 || die "docker compose v2 is required"
}

docker_install() {
  docker_preflight
  mkdir -p "$MARIADB_DATA_DIR" "$INVOICES_DIR" "$BACKUP_DIR"
  ensure_secrets_dir
  info "building cactusInv ${CACTUSINV_VERSION} (build ${CACTUSINV_BUILD})"
  compose build app
  ensure_backup_key
  info "starting cactusInv"
  compose up -d --wait --remove-orphans mariadb
  info "granting the restore staging database ${MARIADB_DATABASE}_restore"
  staging_grant_sql | compose exec -T mariadb sh -c "$MARIADB_ROOT_SQL"
  compose up -d --remove-orphans
  info "waiting for the app to become healthy"
  for _ in $(seq 1 90); do
    if curl -fsS "http://localhost:${HTTP_PORT}/healthz" >/dev/null 2>&1; then
      ok "cactusInv is running at http://localhost:${HTTP_PORT}"
      echo "    first sign-in: ${CACTUSINV_DEFAULT_ADMIN_USER} / ${CACTUSINV_DEFAULT_ADMIN_PASSWORD} (change it after signing in)"
      [[ ",${COMPOSE_PROFILES:-}," == *",mailpit,"* ]] && echo "    test mailbox:  http://localhost:${MAILPIT_PORT}"
      echo "    invoices:      $INVOICES_DIR    backups: $BACKUP_DIR (encrypted, daily at ${BACKUP_TIMES})"
      backup_key_notice
      return 0
    fi
    sleep 2
  done
  compose logs --tail 50 app
  die "cactusInv did not become healthy"
}

docker_uninstall() {
  docker_preflight
  info "stopping and removing containers (data is kept)"
  compose down --remove-orphans
  ok "uninstalled; data kept in $DATA_DIR"
}

docker_delete_uninstall() {
  docker_preflight
  confirm "This deletes the database, all exported invoices, all backups and the secrets."
  compose down --volumes --remove-orphans --rmi local || true
  # Files are owned by container users; delete them from inside a container.
  local d
  for d in "$MARIADB_DATA_DIR" "$INVOICES_DIR" "$BACKUP_DIR"; do
    if [[ -d "$d" ]]; then
      docker run --rm -v "$(cd "$d" && pwd):/target" busybox:1.37 sh -c 'rm -rf /target/* /target/.[!.]* 2>/dev/null; true'
      rmdir "$d" 2>/dev/null || true
    fi
  done
  rm -rf "$SECRETS_DIR"
  ok "cactusInv and all of its data were removed"
}

docker_status() { docker_preflight; compose ps; }

docker_backup_now() {
  docker_preflight
  compose exec -T app /cactusinv backup create </dev/null
}

# restore_source resolves RESTORE_FILE: a local path, or the name of a file in the backups
# volume ("stored:NAME"; read through the app, which owns that volume).
restore_source() {
  if [[ -f "$RESTORE_FILE" ]]; then echo "$RESTORE_FILE"; return; fi
  if [[ "$RESTORE_FILE" != */* ]]; then echo "stored:$RESTORE_FILE"; return; fi
  die "no such backup: $RESTORE_FILE"
}

legacy_warning() {
  printf '\033[33mwarning:\033[0m %s is a legacy plaintext mariadb-dump backup. Restoring it drops and\n' "$1"
  echo "         recreates the whole database, the audit trail included (it is replaced by the one in"
  echo "         the dump); the restore is recorded in the new trail afterwards. Prefer an .age backup."
  echo "         The current data was saved first as $2 (restore that to undo)."
}

# legacy_reset_sql drops and recreates the live database (a legacy dump replaces it whole).
legacy_reset_sql() {
  printf 'DROP DATABASE IF EXISTS `%s`; CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;\n' "$MARIADB_DATABASE" "$MARIADB_DATABASE"
}

# check_dump refuses anything but an intact gzip of a mariadb-dump/mysqldump file.
check_dump() {
  gzip -t "$1" 2>/dev/null || die "$2 is not an intact .gz file; nothing was changed"
  local start
  # (gunzip is cut off by head on purpose: under pipefail that is not a failure here)
  start="$(gunzip -c "$1" 2>/dev/null | head -c 1024 | tr -d '\0' || true)"
  grep -Eq '(MariaDB|MySQL) dump' <<<"$start" || die "$2 is not a mariadb-dump backup; nothing was changed"
}

# with_legacy_dump fetches the dump into a private temporary file, checks it, and runs
# "$1" with the file and its name. Nothing is changed before the checks pass.
LEGACY_TMP=""
with_legacy_dump() {
  local fetch="$1" apply="$2" src name file
  src="$(restore_source)"
  if [[ "$src" == stored:* ]]; then
    name="${src#stored:}"
    LEGACY_TMP="$(umask 077 && mktemp "${TMPDIR:-/tmp}/cactusinv-legacy.XXXXXX")"
    trap 'rm -f "$LEGACY_TMP"' EXIT
    "$fetch" "$name" >"$LEGACY_TMP" || die "cannot read $name from the backups volume"
    file="$LEGACY_TMP"
  else
    name="$(basename "$src")" file="$src"
  fi
  check_dump "$file" "$name"
  "$apply" "$file" "$name"
}

# restore_age stages the backup (the app prints the preview), asks, then applies it.
restore_age() {
  local run="$1" src out id
  src="$(restore_source)"
  if [[ "$src" == stored:* ]]; then
    out="$("$run" stage "${src#stored:}" </dev/null)" || { echo "$out"; die "the backup cannot be restored"; }
  else
    out="$("$run" stage - <"$src")" || { echo "$out"; die "the backup cannot be restored"; }
  fi
  echo "$out" | grep -v '^restore-id:'
  id="$(echo "$out" | sed -n 's/^restore-id: //p')"
  [[ -n "$id" ]] || die "no restore was staged"
  if ! confirm_word RESTORE "Restoring replaces the current data."; then
    "$run" discard "$id" </dev/null >/dev/null
    die "aborted; nothing was changed"
  fi
  "$run" apply "$id" --yes </dev/null
}

docker_app_backup() { compose exec -T -e CACTUSINV_BACKUP_PASSPHRASE app /cactusinv backup "$@"; }
docker_cat_legacy() { compose exec -T app /cactusinv backup cat-legacy "$1" </dev/null; }

docker_load_legacy() {
  local file="$1" name="$2" safety
  info "taking a safety backup of the current data"
  safety="$(docker_app_backup create </dev/null | awk '{print $1}')" || true
  [[ "$safety" == cactusinv-*.age ]] || die "could not take a safety backup first (is backup_key set up?); nothing was changed"
  legacy_warning "$name" "$safety"
  confirm_word RESTORE "Restoring $name replaces the whole database." || die "aborted; nothing was changed"
  info "replacing database ${MARIADB_DATABASE} with $name"
  compose stop app </dev/null
  legacy_reset_sql | compose exec -T mariadb sh -c "$MARIADB_ROOT_SQL"
  gunzip -c "$file" | compose exec -T mariadb sh -c "$MARIADB_APP_SQL"
  compose up -d --wait app </dev/null
  compose exec -T app /cactusinv backup note-legacy-restore - --name "$name" <"$file"
  ok "restored $name (migrated to this version on start); the data before is in $safety"
}

docker_restore() {
  docker_preflight
  if [[ "$RESTORE_FILE" == *.sql.gz ]]; then
    with_legacy_dump docker_cat_legacy docker_load_legacy
  else
    restore_age docker_app_backup
  fi
}

# ---------------------------------------------------------------------------------------
# k3s / Kubernetes
# ---------------------------------------------------------------------------------------
# kc runs kubectl locally or, with --ssh-access, on the remote host (stdin is forwarded).
kc() {
  local cmd
  if [[ "$TARGET" == k3s ]]; then cmd="k3s kubectl"; else cmd="kubectl"; fi
  if [[ -n "$SSH_HOST" ]]; then
    if [[ "$TARGET" == k3s && -z "${KC_PREFIX+x}" ]]; then
      KC_PREFIX=""; [[ "$(ssh_run id -u)" == 0 ]] || KC_PREFIX="sudo -n "
    fi
    ssh_run "${KC_PREFIX:-}$cmd $(printf '%q ' "$@")"
  else
    if [[ "$TARGET" == k3s ]] && ! command -v k3s >/dev/null; then cmd=kubectl; fi
    if [[ "$cmd" == "k3s kubectl" && $EUID -ne 0 ]]; then sudo k3s kubectl "$@"; else $cmd "$@"; fi
  fi
}

# rollout waits for a deployment and, if it does not become ready in time, prints why
# (pods, the pod's events, the container's last log lines, the volumes) before failing.
rollout() {
  local name="$1"
  if kc -n "$K8S_NAMESPACE" rollout status "deploy/$name" --timeout=300s </dev/null; then return 0; fi
  warn "deploy/$name is not ready after 5 minutes; what the cluster says:"
  kc -n "$K8S_NAMESPACE" get pods,pvc -o wide </dev/null || true
  kc -n "$K8S_NAMESPACE" describe pods -l "app.kubernetes.io/name=$name" </dev/null | sed -n '/^Events:/,$p' | tail -25 || true
  kc -n "$K8S_NAMESPACE" logs "deploy/$name" --all-containers --tail=40 </dev/null || true
  die "deploy/$name did not become ready (see above: ImagePullBackOff/ErrImageNeverPull = image not imported; Pending = volume or resources; CrashLoopBackOff = see its log)"
}

# render substitutes ${VAR} in the manifest template from the environment.
render() {
  local tpl="$ROOT/deploy/k8s/cactusinv.yaml"
  if [[ -z "${K8S_STORAGE_CLASS:-}" ]]; then
    grep -v 'storageClassName: \${K8S_STORAGE_CLASS}' "$tpl"
  else
    cat "$tpl"
  fi | perl -pe 's/\$\{([A-Za-z_][A-Za-z0-9_]*)\}/exists $ENV{$1} ? $ENV{$1} : die "unset variable $1\n"/ge'
}

target_arch() {
  local m
  if [[ -n "$SSH_HOST" ]]; then m="$(ssh_run uname -m)"; else m="$(uname -m)"; fi
  case "$m" in x86_64 | amd64) echo amd64 ;; aarch64 | arm64) echo arm64 ;; *) die "unsupported architecture $m" ;; esac
}

build_image() {
  command -v docker >/dev/null || die "docker is needed to build the image"
  local arch; arch="$(target_arch)"
  info "building ${IMAGE} for linux/${arch}"
  TARGET_ARCH="$arch"
  # No provenance/SBOM manifests: with Docker's containerd image store they make
  # "docker save" an index that "k3s ctr images import" cannot unpack.
  docker buildx build --platform "linux/${arch}" --provenance=false --sbom=false \
    --build-arg "VERSION=${CACTUSINV_VERSION}" --build-arg "BUILD=${CACTUSINV_BUILD}" -t "$IMAGE" --load "$ROOT"
}

publish_image() {
  if [[ "$TARGET" == k3s ]]; then
    info "importing the image into k3s"
    local ctr="k3s ctr"
    if [[ -n "$SSH_HOST" ]]; then
      [[ "$(ssh_run id -u)" == 0 ]] || ctr="sudo -n k3s ctr"
    elif [[ $EUID -ne 0 ]]; then
      ctr="sudo k3s ctr"
    fi
    local save=(docker save --platform "linux/${TARGET_ARCH}" "$IMAGE")
    if [[ -n "$SSH_HOST" ]]; then
      "${save[@]}" | ssh_run "$ctr images import -" || die "importing the image on ${SSH_HOST} failed (the SSH user needs root or passwordless sudo for k3s)"
      ssh_run "$ctr images ls -q" | grep -qF "/${IMAGE}" || die "k3s on ${SSH_HOST} does not list ${IMAGE} after the import"
    else
      "${save[@]}" | $ctr images import - || die "importing the image into k3s failed"
      $ctr images ls -q | grep -qF "/${IMAGE}" || die "k3s does not list ${IMAGE} after the import"
    fi
    ok "image ${IMAGE} is on the node"
  else
    [[ -n "${K8S_REGISTRY:-}" ]] || die "set K8S_REGISTRY in .env to push the image for --k8s"
    local remote="${K8S_REGISTRY%/}/${IMAGE}"
    docker tag "$IMAGE" "$remote"
    info "pushing $remote"
    docker push "$remote"
    IMAGE="$remote"
  fi
}

k8s_install() {
  IMAGE="$(image_tag)"
  # k3s runs the imported image only: a missing one fails at once (ErrImageNeverPull)
  # instead of retrying a pull from Docker Hub until the rollout times out.
  if [[ "$TARGET" == k3s ]]; then IMAGE_PULL_POLICY=Never; else IMAGE_PULL_POLICY=IfNotPresent; fi
  export IMAGE IMAGE_PULL_POLICY
  build_image
  publish_image
  export IMAGE
  info "applying manifests to namespace ${K8S_NAMESPACE}"
  kc create namespace "$K8S_NAMESPACE" --dry-run=client -o yaml | kc apply -f -
  # Passwords are generated once, inside the cluster, and never written locally.
  if ! kc -n "$K8S_NAMESPACE" get secret cactusinv-secrets >/dev/null 2>&1; then
    # Sent as a manifest on stdin so the values never appear on a command line.
    kc apply -f - <<YAML
apiVersion: v1
kind: Secret
metadata:
  name: cactusinv-secrets
  namespace: ${K8S_NAMESPACE}
type: Opaque
stringData:
  db_password: "$(random_secret)"
  db_root_password: "$(random_secret)"
  smtp_password: "${CACTUSINV_SMTP_PASSWORD:-}"
  app_key: "$(random_key)"
YAML
    kc -n "$K8S_NAMESPACE" label secret cactusinv-secrets app.kubernetes.io/part-of=cactusinv
    ok "generated database passwords in secret cactusinv-secrets"
  fi
  # Older installs: add the app_key that encrypts the in-app SMTP password.
  if ! kc -n "$K8S_NAMESPACE" get secret cactusinv-secrets -o jsonpath='{.data.app_key}' | grep -q .; then
    printf '{"stringData":{"app_key":"%s"}}' "$(random_key)" | kc -n "$K8S_NAMESPACE" patch secret cactusinv-secrets --type merge --patch-file /dev/stdin
  fi
  # The mariadb-dump CronJob and its script are retired: the app takes the backups now.
  kc -n "$K8S_NAMESPACE" delete cronjob cactusinv-backup --ignore-not-found
  kc -n "$K8S_NAMESPACE" delete configmap cactusinv-backup-script --ignore-not-found
  render | kc apply -f -
  info "waiting for the rollout"
  rollout mariadb
  info "granting the restore staging database ${MARIADB_DATABASE}_restore"
  staging_grant_sql | kc -n "$K8S_NAMESPACE" exec -i deploy/mariadb -- sh -c "$MARIADB_ROOT_SQL"
  rollout cactusinv
  # backup_key: generated by the deployed image, straight into the Secret (never on disk).
  if ! kc -n "$K8S_NAMESPACE" get secret cactusinv-secrets -o jsonpath='{.data.backup_key}' | grep -q .; then
    local key
    key="$(kc -n "$K8S_NAMESPACE" exec deploy/cactusinv -- /cactusinv backup keygen </dev/null | base64 | tr -d '\n')"
    printf '{"data":{"backup_key":"%s"}}' "$key" | kc -n "$K8S_NAMESPACE" patch secret cactusinv-secrets --type merge --patch-file /dev/stdin
    unset key
    kc -n "$K8S_NAMESPACE" rollout restart deploy/cactusinv
    rollout cactusinv
    ok "generated backup_key in secret cactusinv-secrets"
  fi
  local host="${SSH_HOST#*@}"; [[ -n "$host" ]] || host="<node-ip>"
  ok "cactusInv is running at http://${host}:${K8S_NODE_PORT}"
  echo "    first sign-in: ${CACTUSINV_DEFAULT_ADMIN_USER} / ${CACTUSINV_DEFAULT_ADMIN_PASSWORD} (change it after signing in)"
  echo "    test mailbox:  http://${host}:${K8S_MAILPIT_NODE_PORT}    backups: encrypted, daily at ${BACKUP_TIMES}"
  echo "    backup_key:    copy it OFF the cluster: kubectl -n ${K8S_NAMESPACE} get secret cactusinv-secrets -o jsonpath='{.data.backup_key}' | base64 -d"
}

k8s_uninstall() {
  info "removing cactusInv workloads (volumes and secrets are kept)"
  kc -n "$K8S_NAMESPACE" delete deploy,svc,cronjob,job,configmap -l app.kubernetes.io/part-of=cactusinv --ignore-not-found
  ok "uninstalled; persistent volumes kept in namespace ${K8S_NAMESPACE}"
}

k8s_delete_uninstall() {
  confirm "This deletes namespace ${K8S_NAMESPACE} with the database, invoices, backups and secrets."
  kc delete namespace "$K8S_NAMESPACE" --ignore-not-found --wait=true
  ok "cactusInv and all of its data were removed"
}

k8s_status() { kc -n "$K8S_NAMESPACE" get deploy,pods,svc,pvc; }

k8s_backup_now() {
  kc -n "$K8S_NAMESPACE" exec deploy/cactusinv -- /cactusinv backup create </dev/null
}

k8s_app_backup() { kc -n "$K8S_NAMESPACE" exec -i deploy/cactusinv -- /cactusinv backup "$@"; }
k8s_cat_legacy() { kc -n "$K8S_NAMESPACE" exec deploy/cactusinv -- /cactusinv backup cat-legacy "$1" </dev/null; }

k8s_load_legacy() {
  local file="$1" name="$2" safety
  info "taking a safety backup of the current data"
  safety="$(k8s_app_backup create </dev/null | awk '{print $1}')" || true
  [[ "$safety" == cactusinv-*.age ]] || die "could not take a safety backup first (is backup_key set up?); nothing was changed"
  legacy_warning "$name" "$safety"
  confirm_word RESTORE "Restoring $name replaces the whole database." || die "aborted; nothing was changed"
  kc -n "$K8S_NAMESPACE" scale deploy/cactusinv --replicas=0 </dev/null
  legacy_reset_sql | kc -n "$K8S_NAMESPACE" exec -i deploy/mariadb -- sh -c "$MARIADB_ROOT_SQL"
  gunzip -c "$file" | kc -n "$K8S_NAMESPACE" exec -i deploy/mariadb -- sh -c "$MARIADB_APP_SQL"
  kc -n "$K8S_NAMESPACE" scale deploy/cactusinv --replicas=1 </dev/null
  rollout cactusinv
  kc -n "$K8S_NAMESPACE" exec -i deploy/cactusinv -- /cactusinv backup note-legacy-restore - --name "$name" <"$file"
  ok "restored $name; the data before is in $safety"
}

k8s_restore() {
  if [[ "$RESTORE_FILE" == *.sql.gz ]]; then
    with_legacy_dump k8s_cat_legacy k8s_load_legacy
  else
    [[ -z "${CACTUSINV_BACKUP_PASSPHRASE:-}" ]] || die "on k3s/k8s restore passphrase-encrypted backups in the app (Settings > Backup & restore)"
    restore_age k8s_app_backup
  fi
}

# ---------------------------------------------------------------------------------------
# Remote Docker: ship the project and run this script on the host.
# ---------------------------------------------------------------------------------------
remote_docker() {
  info "copying cactusInv to ${SSH_HOST}:${REMOTE_DIR}"
  ssh_run "mkdir -p '$REMOTE_DIR'"
  tar -C "$ROOT" --exclude=./data --exclude=./secrets --exclude=./.git --exclude=./web/node_modules \
    --exclude=./web/dist --exclude=./.deploy -czf - . | ssh_run "tar -C '$REMOTE_DIR' -xzf -"
  local args=("--$ACTION" --dockerdesktop) pre=""
  if [[ "$ACTION" == restore ]]; then
    local file="$RESTORE_FILE"
    if [[ -f "$RESTORE_FILE" ]]; then
      # A backup file on this machine is copied to a private place on the host first.
      file="$REMOTE_DIR/.deploy/restore-$(basename "$RESTORE_FILE")"
      info "copying $(basename "$RESTORE_FILE") to ${SSH_HOST}"
      ssh_run "mkdir -p '$REMOTE_DIR/.deploy' && chmod 700 '$REMOTE_DIR/.deploy' && cat > $(printf '%q' "$file") && chmod 600 $(printf '%q' "$file")" <"$RESTORE_FILE"
    fi
    args=(--restore "$file" --dockerdesktop)
    if [[ -n "${CACTUSINV_BACKUP_PASSPHRASE:-}" ]]; then
      # The passphrase goes over stdin into a private file, never on a command line.
      printf '%s' "$CACTUSINV_BACKUP_PASSPHRASE" | ssh_run "mkdir -p '$REMOTE_DIR/.deploy' && umask 077 && cat > '$REMOTE_DIR/.deploy/passphrase'"
      pre="CACTUSINV_BACKUP_PASSPHRASE=\"\$(cat .deploy/passphrase)\"; rm -f .deploy/passphrase; export CACTUSINV_BACKUP_PASSPHRASE; "
    fi
  fi
  [[ $ASSUME_YES -eq 1 ]] && args+=(--yes)
  ssh -t "$SSH_HOST" "cd '$REMOTE_DIR' && ${pre}./deploy.sh $(printf '%q ' "${args[@]}")"
}

# ---------------------------------------------------------------------------------------
main() {
  if [[ "$TARGET" == dockerdesktop ]]; then
    if [[ -n "$SSH_HOST" ]]; then remote_docker; return; fi
    case "$ACTION" in
      install) docker_install ;;
      uninstall) docker_uninstall ;;
      delete-uninstall) docker_delete_uninstall ;;
      status) docker_status ;;
      backup-now) docker_backup_now ;;
      restore) docker_restore ;;
    esac
  else
    case "$ACTION" in
      install) k8s_install ;;
      uninstall) k8s_uninstall ;;
      delete-uninstall) k8s_delete_uninstall ;;
      status) k8s_status ;;
      backup-now) k8s_backup_now ;;
      restore) k8s_restore ;;
    esac
  fi
}
main
