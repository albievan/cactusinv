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
#   --backup-now         take a database backup immediately
#   --restore FILE       restore a database backup (a cactusinv-*.sql.gz from the backups volume)
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
  v="$(git describe --tags --always --dirty 2>/dev/null || date +%Y%m%d%H%M%S)"
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
  # MariaDB and the backup container run as other users and must read the files.
  chmod 644 "$SECRETS_DIR"/*
}

docker_preflight() {
  command -v docker >/dev/null || die "docker is not installed"
  docker info >/dev/null 2>&1 || die "the Docker daemon is not running (start Docker Desktop)"
  docker compose version >/dev/null 2>&1 || die "docker compose v2 is required"
}

docker_install() {
  docker_preflight
  mkdir -p "$MARIADB_DATA_DIR" "$INVOICES_DIR" "$BACKUP_DIR"
  ensure_secrets_dir
  info "building and starting cactusInv ${CACTUSINV_VERSION} (build ${CACTUSINV_BUILD})"
  compose up -d --build --remove-orphans
  info "waiting for the app to become healthy"
  for _ in $(seq 1 90); do
    if curl -fsS "http://localhost:${HTTP_PORT}/healthz" >/dev/null 2>&1; then
      ok "cactusInv is running at http://localhost:${HTTP_PORT}"
      echo "    first sign-in: ${CACTUSINV_DEFAULT_ADMIN_USER} / ${CACTUSINV_DEFAULT_ADMIN_PASSWORD} (change it after signing in)"
      [[ ",${COMPOSE_PROFILES:-}," == *",mailpit,"* ]] && echo "    test mailbox:  http://localhost:${MAILPIT_PORT}"
      echo "    invoices:      $INVOICES_DIR    backups: $BACKUP_DIR (daily at ${BACKUP_TIMES})"
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
  compose run --rm --entrypoint bash backup /scripts/backup.sh once
}

docker_restore() {
  docker_preflight
  confirm "Restoring $RESTORE_FILE replaces the current database contents."
  compose run --rm --entrypoint bash backup /scripts/backup.sh restore "$RESTORE_FILE"
}

# ---------------------------------------------------------------------------------------
# k3s / Kubernetes
# ---------------------------------------------------------------------------------------
# kc runs kubectl locally or, with --ssh-access, on the remote host (stdin is forwarded).
kc() {
  local cmd
  if [[ "$TARGET" == k3s ]]; then cmd="k3s kubectl"; else cmd="kubectl"; fi
  if [[ -n "$SSH_HOST" ]]; then
    ssh_run "$cmd $(printf '%q ' "$@")"
  else
    if [[ "$TARGET" == k3s ]] && ! command -v k3s >/dev/null; then cmd=kubectl; fi
    if [[ "$cmd" == "k3s kubectl" && $EUID -ne 0 ]]; then sudo k3s kubectl "$@"; else $cmd "$@"; fi
  fi
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
  docker buildx build --platform "linux/${arch}" --build-arg "VERSION=${CACTUSINV_VERSION}" \
    --build-arg "BUILD=${CACTUSINV_BUILD}" -t "$IMAGE" --load "$ROOT"
}

publish_image() {
  if [[ "$TARGET" == k3s ]]; then
    info "importing the image into k3s"
    if [[ -n "$SSH_HOST" ]]; then
      docker save "$IMAGE" | ssh_run "k3s ctr images import -"
    elif [[ $EUID -eq 0 ]]; then
      docker save "$IMAGE" | k3s ctr images import -
    else
      docker save "$IMAGE" | sudo k3s ctr images import -
    fi
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
  export IMAGE
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
  kc -n "$K8S_NAMESPACE" create configmap cactusinv-backup-script --from-literal=backup.sh="$(cat "$ROOT/deploy/backup/backup.sh")" \
    --dry-run=client -o yaml | kc apply -f -
  render | kc apply -f -
  info "waiting for the rollout"
  kc -n "$K8S_NAMESPACE" rollout status deploy/mariadb --timeout=300s
  kc -n "$K8S_NAMESPACE" rollout status deploy/cactusinv --timeout=300s
  local host="${SSH_HOST#*@}"; [[ -n "$host" ]] || host="<node-ip>"
  ok "cactusInv is running at http://${host}:${K8S_NODE_PORT}"
  echo "    first sign-in: ${CACTUSINV_DEFAULT_ADMIN_USER} / ${CACTUSINV_DEFAULT_ADMIN_PASSWORD} (change it after signing in)"
  echo "    test mailbox:  http://${host}:${K8S_MAILPIT_NODE_PORT}    backups: CronJob '${BACKUP_CRON}'"
}

k8s_uninstall() {
  info "removing cactusInv workloads (volumes and secrets are kept)"
  kc -n "$K8S_NAMESPACE" delete deploy,svc,cronjob,job,configmap -l app.kubernetes.io/part-of=cactusinv --ignore-not-found
  kc -n "$K8S_NAMESPACE" delete configmap cactusinv-backup-script --ignore-not-found
  ok "uninstalled; persistent volumes kept in namespace ${K8S_NAMESPACE}"
}

k8s_delete_uninstall() {
  confirm "This deletes namespace ${K8S_NAMESPACE} with the database, invoices, backups and secrets."
  kc delete namespace "$K8S_NAMESPACE" --ignore-not-found --wait=true
  ok "cactusInv and all of its data were removed"
}

k8s_status() { kc -n "$K8S_NAMESPACE" get deploy,pods,svc,pvc,cronjob,jobs; }

k8s_backup_now() {
  local job
  job="cactusinv-backup-manual-$(date +%s)"
  kc -n "$K8S_NAMESPACE" create job "$job" --from=cronjob/cactusinv-backup
  kc -n "$K8S_NAMESPACE" wait --for=condition=complete "job/$job" --timeout=600s
  kc -n "$K8S_NAMESPACE" logs "job/$job"
}

k8s_restore() {
  confirm "Restoring $RESTORE_FILE replaces the current database contents."
  local job
  job="cactusinv-restore-$(date +%s)"
  kc apply -f - <<YAML
apiVersion: batch/v1
kind: Job
metadata:
  name: ${job}
  namespace: ${K8S_NAMESPACE}
  labels: { app.kubernetes.io/part-of: cactusinv }
spec:
  backoffLimit: 0
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: restore
          image: mariadb:11
          command: [bash, /scripts/backup.sh, restore, "${RESTORE_FILE}"]
          env:
            - { name: DB_HOST, value: mariadb }
            - { name: DB_NAME, value: "${MARIADB_DATABASE}" }
            - { name: DB_USER, value: "${MARIADB_USER}" }
            - { name: DB_PASSWORD_FILE, value: /run/secrets/db_password }
          volumeMounts:
            - { name: backups, mountPath: /backups }
            - { name: scripts, mountPath: /scripts, readOnly: true }
            - { name: secrets, mountPath: /run/secrets, readOnly: true }
      volumes:
        - { name: backups, persistentVolumeClaim: { claimName: backups } }
        - { name: scripts, configMap: { name: cactusinv-backup-script } }
        - { name: secrets, secret: { secretName: cactusinv-secrets } }
YAML
  kc -n "$K8S_NAMESPACE" wait --for=condition=complete "job/$job" --timeout=900s
  kc -n "$K8S_NAMESPACE" logs "job/$job"
}

# ---------------------------------------------------------------------------------------
# Remote Docker: ship the project and run this script on the host.
# ---------------------------------------------------------------------------------------
remote_docker() {
  info "copying cactusInv to ${SSH_HOST}:${REMOTE_DIR}"
  ssh_run "mkdir -p '$REMOTE_DIR'"
  tar -C "$ROOT" --exclude=./data --exclude=./secrets --exclude=./.git --exclude=./web/node_modules \
    --exclude=./web/dist --exclude=./.deploy -czf - . | ssh_run "tar -C '$REMOTE_DIR' -xzf -"
  local args=("--$ACTION" --dockerdesktop)
  [[ "$ACTION" == restore ]] && args=(--restore "$RESTORE_FILE" --dockerdesktop)
  [[ $ASSUME_YES -eq 1 ]] && args+=(--yes)
  ssh -t "$SSH_HOST" "cd '$REMOTE_DIR' && ./deploy.sh $(printf '%q ' "${args[@]}")"
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
