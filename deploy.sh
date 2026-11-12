#!/usr/bin/env bash
# cactusInv deployment — one script for Docker Desktop / Docker Engine, k3s and Kubernetes.
#
#   ./deploy.sh ACTION TARGET [--ssh-access user@host] [--yes]
#
# ACTION
#   --install            build and start (or reinstall) cactusInv from this folder
#   --upgrade [--to REF] upgrade to the newest version: fetch it from git (the branch's
#                        upstream, or REF: a tag, branch or commit), show what changes, take a
#                        backup, install it and check it runs; if not, go back to this version
#   --version            show the running version and database schema
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
#   --yes                    do not ask for confirmation (--delete-uninstall, --restore, --upgrade)
#   -h, --help               show this help
#
# Settings and default volume mappings come from .env next to this script.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

ACTION="" TARGET="" SSH_HOST="" ASSUME_YES=0 RESTORE_FILE="" UPGRADE_TO="" DOWN_TO=""
REMOTE_DIR="${CACTUSINV_REMOTE_DIR:-/opt/cactusinv}"

die() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
info() { printf '\033[36m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[33mwarning:\033[0m %s\n' "$*" >&2; }
ok() { printf '\033[32m✔\033[0m %s\n' "$*"; }
usage() { sed -n '2,/^set -euo/p' "$0" | sed '$d; s/^# \{0,1\}//'; exit "${1:-0}"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --install | --uninstall | --delete-uninstall | --status | --backup-now | --upgrade | --version)
      [[ -z "$ACTION" ]] || die "choose one action"; ACTION="${1#--}" ;;
    --to) UPGRADE_TO="${2:-}"; shift; [[ -n "$UPGRADE_TO" && "$UPGRADE_TO" != --* ]] || die "--to needs a tag, branch or commit" ;;
    --migrate-down-to)
      # Used by --upgrade to undo a failed upgrade's migrations (with the newer version).
      [[ -z "$ACTION" ]] || die "choose one action"; ACTION=migrate-down-to; DOWN_TO="${2:-}"; shift
      [[ "$DOWN_TO" =~ ^[1-9][0-9]*$ ]] || die "--migrate-down-to needs a schema version" ;;
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
[[ -z "$UPGRADE_TO" || "$ACTION" == upgrade ]] || die "--to goes with --upgrade"
export TARGET # the k8s manifest names it (System Settings)

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

# ---------------------------------------------------------------------------------------
# --upgrade (M27): every step is this script run again (--version, --backup-now, --install,
# --migrate-down-to), so the install is the NEW version's deploy.sh and each step reads .env
# afresh. It runs before .env is read, so those runs see only the caller's environment.
# ---------------------------------------------------------------------------------------
# self runs this script (as it is on disk now) for the same target.
self() {
  local ssh=()
  [[ -z "$SSH_HOST" ]] || ssh=(--ssh-access "$SSH_HOST")
  "$ROOT/deploy.sh" "$@" "--$TARGET" ${ssh[@]+"${ssh[@]}"}
}

# running_version prints "1.12.1 (build 29)" and the schema line of the running install.
running_version() { self --version </dev/null 2>/dev/null | tr -d '\r'; }

# schema_head is the newest migration number in commit $1.
schema_head() {
  git -C "$ROOT" ls-tree --name-only "$1" migrations/ | sed -n 's#^migrations/0*\([0-9][0-9]*\)_.*#\1#p' | sort -n | tail -1
}

upgrade() {
  command -v git >/dev/null || die "--upgrade needs git"
  git -C "$ROOT" rev-parse --git-dir >/dev/null 2>&1 || die "--upgrade needs the git checkout cactusInv is installed from"
  local branch remote target old running run_line run_schema new_version new_build cur_version cur_build
  local old_head new_head changes count backup_out backup_name answer
  branch="$(git -C "$ROOT" symbolic-ref --quiet --short HEAD)" || die "this checkout is not on a branch; check out main first (nothing was changed)"
  git -C "$ROOT" diff --quiet HEAD -- || die "tracked files have uncommitted changes; commit or stash them first (nothing was changed)"

  info "reading the running version"
  running="$(running_version)" || true
  run_line="$(printf '%s\n' "$running" | grep '(build ' | tail -1 || true)"
  run_schema="$(printf '%s\n' "$running" | sed -n 's/^schema \([0-9][0-9]*\),.*/\1/p' | tail -1)"
  [[ -n "$run_line" ]] || die "cactusInv is not running on this target: install it with --install first (nothing was changed)"

  remote="$(git -C "$ROOT" config "branch.${branch}.remote" || echo origin)"
  info "fetching the newest version from ${remote}"
  git -C "$ROOT" fetch --quiet --tags "$remote" || die "cannot fetch from ${remote} (nothing was changed)"
  if [[ -n "$UPGRADE_TO" ]]; then
    target="$(git -C "$ROOT" rev-parse --verify --quiet "${UPGRADE_TO}^{commit}" ||
      git -C "$ROOT" rev-parse --verify --quiet "${remote}/${UPGRADE_TO}^{commit}")" || die "no such version: $UPGRADE_TO (nothing was changed)"
  else
    target="$(git -C "$ROOT" rev-parse --verify --quiet '@{upstream}^{commit}')" || die "branch $branch follows no remote branch; name the version with --to (nothing was changed)"
  fi
  old="$(git -C "$ROOT" rev-parse HEAD)"
  git -C "$ROOT" merge-base --is-ancestor "$old" "$target" ||
    die "$(git -C "$ROOT" rev-parse --short "$target") is not newer than this checkout: going back needs a restore, and local commits must be pushed or dropped first (nothing was changed)"

  cur_version="$(tr -d '[:space:]' <"$ROOT/VERSION")" cur_build="$(git -C "$ROOT" rev-list --count HEAD)"
  new_version="$(git -C "$ROOT" show "${target}:VERSION" | tr -d '[:space:]')" new_build="$(git -C "$ROOT" rev-list --count "$target")"
  if [[ "$old" == "$target" && "$run_line" == "$cur_version (build $cur_build)" ]]; then
    ok "already the newest version: $run_line"
    return 0
  fi
  old_head="$(schema_head "$old")" new_head="$(schema_head "$target")"
  count="$(git -C "$ROOT" rev-list --count "${old}..${target}")"
  # (head stops reading early: under pipefail that is not a failure)
  changes="$(git -C "$ROOT" log --no-decorate --format='    %h %s' "${old}..${target}" | head -25 || true)"
  echo
  echo "  running now:  ${run_line}${run_schema:+, database schema $run_schema}"
  echo "  upgrade to:   ${new_version} (build ${new_build}), commit $(git -C "$ROOT" rev-parse --short "$target")"
  if [[ "$count" -gt 0 ]]; then
    echo "  changes (${count}):"
    echo "$changes"
    [[ "$count" -le 25 ]] || echo "    … and $((count - 25)) more"
  else
    echo "  (this checkout is already at that version; it is installed now)"
  fi
  if [[ "${new_head:-0}" -gt "${old_head:-0}" ]]; then
    echo "  database:     schema ${old_head} → ${new_head} (changed when the new version starts)"
  else
    echo "  database:     no changes"
  fi
  echo
  if [[ $ASSUME_YES -ne 1 ]]; then
    [[ -t 0 ]] || die "re-run with --yes to upgrade non-interactively (nothing was changed)"
    read -r -p "A backup is taken first. Type 'upgrade' to continue: " answer
    [[ "$answer" == upgrade ]] || die "aborted; nothing was changed"
  fi

  info "taking a backup first"
  backup_out="$(self --backup-now </dev/null 2>&1 | tr -d '\r')" || { echo "$backup_out"; die "the backup failed, so nothing was changed"; }
  backup_name="$(printf '%s\n' "$backup_out" | grep -o 'cactusinv-[A-Za-z0-9._-]*\.age' | head -1 || true)"
  [[ -n "$backup_name" ]] || { echo "$backup_out"; die "the backup's name could not be read, so nothing was changed"; }
  ok "backup ${backup_name}"

  if [[ "$old" != "$target" ]]; then
    git -C "$ROOT" merge --ff-only --quiet "$target" || die "git could not move to $(git -C "$ROOT" rev-parse --short "$target") (nothing was changed; backup ${backup_name})"
  fi
  info "installing ${new_version} (build ${new_build})"
  if self --install </dev/null && upgrade_verify "${new_version} (build ${new_build})"; then
    ok "upgraded: ${run_line} → ${new_version} (build ${new_build}); the backup taken first is ${backup_name}"
    return 0
  fi
  upgrade_rollback "$old" "$old_head" "$new_head" "$run_line" "$backup_name"
}

# upgrade_verify waits (up to a minute) for the running install to report version $1.
upgrade_verify() {
  local got
  for _ in 1 2 3 4 5 6 7 8 9 10 11 12; do
    got="$(running_version | grep '(build ' | tail -1 || true)"
    [[ "$got" == "$1" ]] && return 0
    sleep 5
  done
  warn "the running version is '${got:-unknown}', not $1"
  return 1
}

# upgrade_rollback goes back to commit $1 after a failed upgrade: the new version's
# migrations are undone with the new version first (an older one cannot, and refuses a newer
# schema), then the old version is installed again.
upgrade_rollback() {
  local old="$1" old_head="$2" new_head="$3" was="$4" backup="$5" undo=""
  warn "the upgrade did not complete; going back to ${was}"
  if [[ "${new_head:-0}" -gt "${old_head:-0}" ]]; then
    info "undoing the new version's database changes (schema back to ${old_head})"
    if ! self --migrate-down-to "$old_head" </dev/null; then
      undo="failed"
      warn "the database changes could not be undone automatically"
    fi
  fi
  git -C "$ROOT" reset --keep --quiet "$old" || die "git could not go back to $(git -C "$ROOT" rev-parse --short "$old"); run: git reset --keep $old && ./deploy.sh --install --$TARGET (backup ${backup})"
  if [[ -z "$undo" ]] && self --install </dev/null && upgrade_verify "$was"; then
    die "the upgrade failed and ${was} is running again with the data as before (the backup taken first is ${backup}); see above for why"
  fi
  die "the upgrade failed and could not be rolled back completely. This checkout is back at ${was}. To recover: ./deploy.sh --install --${TARGET}, and if it does not start (database schema newer), ./deploy.sh --restore ${backup} --${TARGET} after the install, or ask for help with the output above"
}

if [[ "$ACTION" == upgrade ]]; then upgrade; exit $?; fi

# Folders set in the environment win over System Settings, as over .env.
EXPLICIT_BACKUP_DIR="${BACKUP_DIR+x}" EXPLICIT_INVOICES_DIR="${INVOICES_DIR+x}"
load_env

# Names that end up in SQL (database, user) are restricted to plain identifiers.
[[ "${MARIADB_DATABASE:-}" =~ ^[A-Za-z0-9_]{1,57}$ ]] || die "MARIADB_DATABASE must be 1-57 letters, digits or _"
[[ "${MARIADB_USER:-}" =~ ^[A-Za-z0-9_]{1,80}$ ]] || die "MARIADB_USER must be 1-80 letters, digits or _"
export CACTUSINV_UPDATE_URL="${CACTUSINV_UPDATE_URL:-}" # (an older .env has none; the manifest names it)


# Release version (VERSION file) and build number (commit count) shown in the app.
export CACTUSINV_VERSION CACTUSINV_BUILD
CACTUSINV_VERSION="$(tr -d '[:space:]' <"$ROOT/VERSION" 2>/dev/null || echo dev)"
CACTUSINV_BUILD="${CACTUSINV_BUILD:-$(git -C "$ROOT" rev-list --count HEAD 2>/dev/null || echo 0)}"

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

# abs_dir resolves a folder relative to the project folder.
abs_dir() { case "$1" in /*) printf '%s' "${1%/}" ;; *) printf '%s/%s' "$ROOT" "${1#./}" ;; esac; }

# mounted_dir prints the host folder the running app has mounted at a container path.
mounted_dir() {
  local id
  id="$(compose ps -q app 2>/dev/null || true)"
  [[ -n "$id" ]] || return 0
  docker inspect -f "{{range .Mounts}}{{if eq .Destination \"$1\"}}{{.Source}}{{end}}{{end}}" "$id" 2>/dev/null || true
}

# move_folder copies a folder's files to its new host folder when that is empty, so the
# backups and invoice PDFs move with the mapping. The old folder is left as it is.
move_folder() {
  local old="$1" new="$2"
  [[ -n "$old" && "$old" != "$new" && -d "$old" ]] || return 0
  mkdir -p "$new"
  if [[ -n "$(ls -A "$new" 2>/dev/null)" ]]; then
    info "$new already holds files: nothing copied from $old (left as it is)"
  elif [[ -n "$(ls -A "$old" 2>/dev/null)" ]]; then
    info "copying $old to $new (the old folder is left as it is)"
    cp -Rp "$old/." "$new/" || warn "could not copy $old to $new (permissions?): copy it yourself, e.g. with sudo cp -Rp"
  fi
}

# folder_safe says whether a host folder may be mapped: it may not be, contain or lie in
# the database or secrets folder, be the project folder or one above it, be the other
# mapping, or hold files cactusInv did not write (the deploy re-owns a mapped folder for
# the app). $1 is BACKUP_DIR or INVOICES_DIR, $2 the absolute folder, $3 the folder
# mounted now (always allowed), $4 the other mapping's absolute folder.
folder_safe() {
  local key="$1" p="$2" now="$3" other="$4" d f name
  [[ "$p" == "$now" ]] && return 0
  for d in "$(abs_dir "$MARIADB_DATA_DIR")" "$(abs_dir "$SECRETS_DIR")"; do
    if [[ "$p" == "$d" || "$p/" == "$d/"* || "$d/" == "$p/"* ]]; then
      warn "$key: $p is or holds the database or secrets folder; not used"
      return 1
    fi
  done
  if [[ "$p" == "$ROOT" || "$ROOT/" == "$p/"* ]]; then
    warn "$key: $p is the project folder or above it; not used"
    return 1
  fi
  if [[ -n "$other" && ( "$p" == "$other" || "$p/" == "$other/"* || "$other/" == "$p/"* ) ]]; then
    warn "$key: $p overlaps the other mapped folder $other; not used"
    return 1
  fi
  [[ -d "$p" ]] || return 0
  for f in "$p"/* "$p"/.[!.]*; do
    [[ -e "$f" ]] || continue
    name="${f##*/}"
    case "$key:$name" in
      BACKUP_DIR:cactusinv-* | BACKUP_DIR:.* | INVOICES_DIR:[0-9][0-9][0-9][0-9] | INVOICES_DIR:.*) ;;
      *)
        warn "$key: $p holds files cactusInv did not write ($name); choose an empty folder. Not used"
        return 1
        ;;
    esac
  done
}

# read_folder_mappings prints System Settings' folder mappings (cactusinv system folders):
# from the running app, else from a one-off app container (the database is up).
read_folder_mappings() {
  if [[ -n "$(compose ps -q app 2>/dev/null || true)" ]]; then
    compose exec -T app /cactusinv system folders </dev/null 2>/dev/null && return 0
  fi
  # (the image's entrypoint is /cactusinv: the arguments follow it)
  compose run --rm --no-deps -T app system folders </dev/null 2>/dev/null
}

# apply_folder_mappings uses the host folders set in System Settings for BACKUP_DIR and
# INVOICES_DIR, unless they are set in the environment or are unsafe (folder_safe); .env
# applies otherwise. A folder that changes (mapped, changed or cleared) gets the files of
# the folder mounted until now when it is empty.
apply_folder_mappings() {
  local lines line key val backup="" invoices="" now_b now_i
  if ! lines="$(read_folder_mappings)"; then
    warn "could not read the folder mappings from System Settings (first install of this version?); using .env"
    lines=""
  fi
  while IFS= read -r line; do
    # The same rule as the app (domain.ValidateHostDir): absolute or ./relative, no
    # characters Compose or a shell reads specially, no "..".
    [[ "$line" =~ ^(BACKUP_DIR|INVOICES_DIR)=((/|\./)[^:\$\"\'\`\\]*)$ ]] || continue
    key="${BASH_REMATCH[1]}" val="${BASH_REMATCH[2]}"
    [[ "/$val/" == */../* ]] && continue
    if [[ "$key" == BACKUP_DIR && -n "$EXPLICIT_BACKUP_DIR" ]] || [[ "$key" == INVOICES_DIR && -n "$EXPLICIT_INVOICES_DIR" ]]; then
      info "$key is set in the environment; System Settings' $val is not used"
      continue
    fi
    if [[ "$key" == BACKUP_DIR ]]; then backup="$val"; else invoices="$val"; fi
  done <<<"$lines"
  now_b="$(mounted_dir /data/backups)" now_i="$(mounted_dir /data/invoices)"
  if [[ -n "$backup" ]] && folder_safe BACKUP_DIR "$(abs_dir "$backup")" "$now_b" "$(abs_dir "${invoices:-$INVOICES_DIR}")"; then
    info "System Settings map /data/backups to $backup"
    BACKUP_DIR="$backup"
  fi
  if [[ -n "$invoices" ]] && folder_safe INVOICES_DIR "$(abs_dir "$invoices")" "$now_i" "$(abs_dir "$BACKUP_DIR")"; then
    info "System Settings map /data/invoices to $invoices"
    INVOICES_DIR="$invoices"
  fi
  export BACKUP_DIR INVOICES_DIR
  move_folder "$now_b" "$(abs_dir "$BACKUP_DIR")"
  move_folder "$now_i" "$(abs_dir "$INVOICES_DIR")"
}

docker_install() {
  docker_preflight
  mkdir -p "$MARIADB_DATA_DIR"
  ensure_secrets_dir
  info "building cactusInv ${CACTUSINV_VERSION} (build ${CACTUSINV_BUILD})"
  compose build app
  ensure_backup_key
  info "starting cactusInv"
  compose up -d --wait --remove-orphans mariadb
  info "granting the restore staging database ${MARIADB_DATABASE}_restore"
  staging_grant_sql | compose exec -T mariadb sh -c "$MARIADB_ROOT_SQL"
  # As the user (a one-off container would create them as root on Linux).
  mkdir -p "$INVOICES_DIR" "$BACKUP_DIR"
  apply_folder_mappings
  # The app shows the folders mapped now (System Settings).
  export CACTUSINV_DEPLOY_TARGET=dockerdesktop CACTUSINV_HOST_PROJECT_DIR="$ROOT"
  CACTUSINV_HOST_BACKUP_DIR="$(abs_dir "$BACKUP_DIR")" CACTUSINV_HOST_INVOICES_DIR="$(abs_dir "$INVOICES_DIR")"
  export CACTUSINV_HOST_BACKUP_DIR CACTUSINV_HOST_INVOICES_DIR
  mkdir -p "$INVOICES_DIR" "$BACKUP_DIR"
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
  local mb mi
  mb="$(mounted_dir /data/backups)" mi="$(mounted_dir /data/invoices)"
  confirm "This deletes the database, all exported invoices, all backups and the secrets."
  # Folders mapped in System Settings are the user's own (a NAS share): kept.
  [[ -n "$mb" && "$mb" != "$(abs_dir "$BACKUP_DIR")" ]] && warn "the mapped backups folder $mb is kept; delete it yourself if you want"
  [[ -n "$mi" && "$mi" != "$(abs_dir "$INVOICES_DIR")" ]] && warn "the mapped invoices folder $mi is kept; delete it yourself if you want"
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

docker_version() {
  docker_preflight
  compose exec -T app /cactusinv version </dev/null || die "cactusInv is not running here"
  compose exec -T app /cactusinv migrate status </dev/null
}

# docker_migrate_down_to stops the app and rolls the database back to schema DOWN_TO with the
# image installed now (--upgrade's rollback).
docker_migrate_down_to() {
  docker_preflight
  compose stop app </dev/null
  compose run --rm --no-deps -T app migrate down-to "$DOWN_TO" </dev/null
}

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
  kc -n "$K8S_NAMESPACE" logs "deploy/$name" --all-containers --previous --tail=40 </dev/null 2>/dev/null || true
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

# node_sh runs a shell command as root on the k3s node (over SSH, or here).
node_sh() {
  if [[ -n "$SSH_HOST" ]]; then ssh_run "${KC_PREFIX:-}sh -c '$1'" </dev/null
  elif [[ $EUID -eq 0 ]]; then sh -c "$1"; else sudo sh -c "$1"; fi
}

# volume_dir_ok accepts K8S_VOLUME_DIR only as a plain absolute folder (no .., no trailing /).
volume_dir_ok() { [[ "$1" =~ ^/[A-Za-z0-9._/-]*[A-Za-z0-9._-]$ && ! "$1" =~ (^|/)\.\.?(/|$) ]]; }

VOLUME_CLAIMS="mariadb-data:mariadb:999:K8S_MARIADB_STORAGE invoices:invoices:65532:K8S_INVOICES_STORAGE backups:backups:65532:K8S_BACKUP_STORAGE"

# k8s_volumes makes the volume claims bindable before anything is applied, so a wrong
# storage class fails here with a reason instead of leaving the pods Pending for 5 minutes.
# With K8S_VOLUME_DIR (k3s) it creates a no-provisioner class (if missing), the folders on
# the node owned by the pods' users, and one volume per claim in them, pinned by claimRef.
k8s_volumes() {
  local dir="${K8S_VOLUME_DIR:-}" c claim sub uid var have node pv phase
  if [[ -n "$dir" ]]; then
    [[ "$TARGET" == k3s ]] || die "K8S_VOLUME_DIR is for --k3s only (it creates the folders on the node)"
    volume_dir_ok "$dir" ||
      die "K8S_VOLUME_DIR must be an absolute folder of letters, digits, . _ - and / (got '$dir')"
    : "${K8S_STORAGE_CLASS:=local-storage}"
    export K8S_STORAGE_CLASS
  fi
  # A claim's storage class cannot change once it exists.
  if [[ -n "${K8S_STORAGE_CLASS:-}" ]]; then
    for c in $VOLUME_CLAIMS; do
      claim="${c%%:*}"
      have="$(kc -n "$K8S_NAMESPACE" get pvc "$claim" -o jsonpath='{.spec.storageClassName}' 2>/dev/null </dev/null || true)"
      [[ -z "$have" || "$have" == "$K8S_STORAGE_CLASS" ]] ||
        die "volume claim ${claim} already uses storage class ${have}; a claim's class cannot change (K8S_STORAGE_CLASS=${K8S_STORAGE_CLASS}). Set it back, or move the data: README, \"Moving an existing install\""
    done
    if ! kc get storageclass "$K8S_STORAGE_CLASS" >/dev/null 2>&1 </dev/null; then
      if [[ -z "$dir" ]]; then
        die "storage class ${K8S_STORAGE_CLASS} does not exist in this cluster (it has: $(kc get storageclass -o jsonpath='{.items[*].metadata.name}' </dev/null)). Set K8S_STORAGE_CLASS in .env to one of those or empty (the default), or set K8S_VOLUME_DIR to keep the volumes in a folder on the node"
      fi
      info "creating storage class ${K8S_STORAGE_CLASS} (volumes are folders on the node)"
      kc apply -f - <<YAML
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata: { name: ${K8S_STORAGE_CLASS} }
provisioner: kubernetes.io/no-provisioner
volumeBindingMode: WaitForFirstConsumer
reclaimPolicy: Retain
YAML
    fi
  fi
  [[ -n "$dir" ]] || return 0
  [[ "$(kc get storageclass "$K8S_STORAGE_CLASS" -o jsonpath='{.provisioner}' </dev/null)" == kubernetes.io/no-provisioner ]] ||
    die "with K8S_VOLUME_DIR, K8S_STORAGE_CLASS must be a kubernetes.io/no-provisioner class (${K8S_STORAGE_CLASS} creates its own volumes); leave it empty to use local-storage"
  node="$(kc get nodes -o jsonpath='{.items[*].metadata.name}' </dev/null)"
  [[ -n "$node" && "$node" != *" "* ]] || die "K8S_VOLUME_DIR needs a single-node k3s (the folders are on one node); nodes: ${node}"
  info "volumes in ${dir} on node ${node}"
  for c in $VOLUME_CLAIMS; do
    IFS=: read -r claim sub uid var <<<"$c"
    node_sh "mkdir -p $dir/$sub && chown $uid:$uid $dir/$sub && chmod 700 $dir/$sub"
    pv="${K8S_NAMESPACE}-${claim}"
    # After --uninstall + a deleted claim the volume is Released, still naming the old
    # claim's uid: clear it so the new claim binds to the same folder.
    phase="$(kc get pv "$pv" -o jsonpath='{.status.phase}' 2>/dev/null </dev/null || true)"
    if [[ "$phase" == Released ]]; then
      kc patch pv "$pv" --type merge -p '{"spec":{"claimRef":{"uid":null,"resourceVersion":null}}}' </dev/null >/dev/null
    fi
    kc apply -f - <<YAML
apiVersion: v1
kind: PersistentVolume
metadata:
  name: ${pv}
  labels: { app.kubernetes.io/part-of: cactusinv }
spec:
  capacity: { storage: ${!var} }
  accessModes: [ReadWriteOnce]
  persistentVolumeReclaimPolicy: Retain
  storageClassName: ${K8S_STORAGE_CLASS}
  claimRef: { namespace: ${K8S_NAMESPACE}, name: ${claim} }
  local: { path: ${dir}/${sub} }
  nodeAffinity:
    required:
      nodeSelectorTerms:
        - matchExpressions: [{ key: kubernetes.io/hostname, operator: In, values: [${node}] }]
YAML
  done
}

# k8s_https serves the app over HTTPS through the cluster's ingress controller (k3s: Traefik
# on ports 80/443), so the HTTPS-only sign-in cookie (CACTUSINV_COOKIE_SECURE) works.
# K8S_TLS_CERT/K8S_TLS_KEY (PEM files) go into secret cactusinv-tls; without them the
# controller's own self-signed certificate is used. K8S_HTTPS=false removes the Ingress.
k8s_https() {
  if [[ "${K8S_HTTPS:-false}" != true ]]; then
    kc -n "$K8S_NAMESPACE" delete ingress cactusinv --ignore-not-found </dev/null >/dev/null
    return 0
  fi
  local host="${K8S_HTTPS_HOST:-}" cert="${K8S_TLS_CERT:-}" key="${K8S_TLS_KEY:-}"
  [[ -z "$host" || "$host" =~ ^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$ ]] ||
    die "K8S_HTTPS_HOST must be a DNS name such as invoices.example.com, or empty (got '$host')"
  if [[ -n "$cert" || -n "$key" ]]; then
    [[ -f "$cert" && -f "$key" ]] || die "K8S_TLS_CERT and K8S_TLS_KEY must both name PEM files (got '$cert' and '$key')"
    # Sent as a manifest on stdin: kubectl may run on the node, where these files are not.
    kc apply -f - >/dev/null <<YAML
apiVersion: v1
kind: Secret
metadata:
  name: cactusinv-tls
  namespace: ${K8S_NAMESPACE}
  labels: { app.kubernetes.io/part-of: cactusinv }
type: kubernetes.io/tls
data:
  tls.crt: $(base64 <"$cert" | tr -d '\n')
  tls.key: $(base64 <"$key" | tr -d '\n')
YAML
    ok "certificate ${cert} is in secret cactusinv-tls"
  else
    kc -n "$K8S_NAMESPACE" delete secret cactusinv-tls --ignore-not-found </dev/null >/dev/null
  fi
  local tls="{}" rule="{"
  if [[ -n "$cert" ]]; then
    tls="{ secretName: cactusinv-tls$([[ -n "$host" ]] && echo ", hosts: [${host}]") }"
  fi
  [[ -n "$host" ]] && rule="{ host: ${host},"
  local node="${SSH_HOST#*@}"
  info "serving https://${host:-${node:-<node-ip>}}/ through the ingress controller"
  kc apply -f - <<YAML
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: cactusinv
  namespace: ${K8S_NAMESPACE}
  labels: { app.kubernetes.io/part-of: cactusinv }
  annotations:
    # Traefik (k3s): HTTPS only; other controllers ignore these and use spec.tls.
    traefik.ingress.kubernetes.io/router.entrypoints: websecure
    traefik.ingress.kubernetes.io/router.tls: "true"
spec:
  tls: [${tls}]
  rules:
    - ${rule} http: { paths: [{ path: /, pathType: Prefix, backend: { service: { name: cactusinv, port: { number: 80 } } } }] } }
YAML
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
  app_key: "$(random_key)"
data:
  # base64, so a password with quotes or backslashes arrives as typed
  smtp_password: "$(printf '%s' "${CACTUSINV_SMTP_PASSWORD:-}" | base64 | tr -d '\n')"
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
  # A volume claim cannot shrink: one that exists keeps its size (it may have been created
  # larger, by hand or by an earlier .env).
  local claim var size
  for claim in mariadb-data:K8S_MARIADB_STORAGE invoices:K8S_INVOICES_STORAGE backups:K8S_BACKUP_STORAGE; do
    var="${claim#*:}"; claim="${claim%%:*}"
    size="$(kc -n "$K8S_NAMESPACE" get pvc "$claim" -o jsonpath='{.spec.resources.requests.storage}' 2>/dev/null </dev/null || true)"
    if [[ -n "$size" && "$size" != "${!var}" ]]; then
      info "volume ${claim} exists with ${size}: keeping it (${var}=${!var} applies to new installs)"
      export "$var=$size"
    fi
  done
  k8s_volumes
  render | kc apply -f -
  k8s_https
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
  if [[ "${K8S_HTTPS:-false}" == true ]]; then
    ok "cactusInv is running at https://${K8S_HTTPS_HOST:-$host}/ (and http://${host}:${K8S_NODE_PORT}, where sign-in needs CACTUSINV_COOKIE_SECURE=false)"
  else
    ok "cactusInv is running at http://${host}:${K8S_NODE_PORT}"
  fi
  echo "    first sign-in: ${CACTUSINV_DEFAULT_ADMIN_USER} / ${CACTUSINV_DEFAULT_ADMIN_PASSWORD} (change it after signing in)"
  echo "    test mailbox:  http://${host}:${K8S_MAILPIT_NODE_PORT}    backups: encrypted, daily at ${BACKUP_TIMES}"
  echo "    backup_key:    copy it OFF the cluster: kubectl -n ${K8S_NAMESPACE} get secret cactusinv-secrets -o jsonpath='{.data.backup_key}' | base64 -d"
}

k8s_uninstall() {
  info "removing cactusInv workloads (volumes and secrets are kept)"
  kc -n "$K8S_NAMESPACE" delete deploy,svc,ingress,cronjob,job,configmap -l app.kubernetes.io/part-of=cactusinv --ignore-not-found
  ok "uninstalled; persistent volumes kept in namespace ${K8S_NAMESPACE}"
}

k8s_delete_uninstall() {
  confirm "This deletes namespace ${K8S_NAMESPACE} with the database, invoices, backups and secrets."
  kc delete namespace "$K8S_NAMESPACE" --ignore-not-found --wait=true
  # Volumes in K8S_VOLUME_DIR are kept by the cluster (Retain): delete them and their folders.
  if [[ -n "${K8S_VOLUME_DIR:-}" && "$TARGET" == k3s ]]; then
    local c claim sub rest
    for c in $VOLUME_CLAIMS; do
      IFS=: read -r claim sub rest <<<"$c"
      kc delete pv "${K8S_NAMESPACE}-${claim}" --ignore-not-found </dev/null
      if volume_dir_ok "$K8S_VOLUME_DIR"; then node_sh "rm -rf $K8S_VOLUME_DIR/$sub"; fi
    done
  fi
  ok "cactusInv and all of its data were removed"
}

k8s_status() { kc -n "$K8S_NAMESPACE" get deploy,pods,svc,pvc; }

k8s_version() {
  kc -n "$K8S_NAMESPACE" exec deploy/cactusinv -- /cactusinv version </dev/null || die "cactusInv is not running in namespace ${K8S_NAMESPACE}"
  kc -n "$K8S_NAMESPACE" exec deploy/cactusinv -- /cactusinv migrate status </dev/null
}

# k8s_migrate_down_to rolls the database back to schema DOWN_TO in the running pod (it must be
# running: a pod that does not start cannot be used; --upgrade then says how to recover).
k8s_migrate_down_to() {
  kc -n "$K8S_NAMESPACE" exec deploy/cactusinv -- /cactusinv migrate down-to "$DOWN_TO" </dev/null
}

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
    --exclude=./web/dist --exclude=./dist --exclude=./.deploy -czf - . | ssh_run "tar -C '$REMOTE_DIR' -xzf -"
  local args=("--$ACTION" --dockerdesktop) pre="CACTUSINV_BUILD=${CACTUSINV_BUILD}; export CACTUSINV_BUILD; "
  [[ "$ACTION" == migrate-down-to ]] && args=(--migrate-down-to "$DOWN_TO" --dockerdesktop)
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
      pre="${pre}CACTUSINV_BACKUP_PASSPHRASE=\"\$(cat .deploy/passphrase)\"; rm -f .deploy/passphrase; export CACTUSINV_BACKUP_PASSPHRASE; "
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
      version) docker_version ;;
      migrate-down-to) docker_migrate_down_to ;;
    esac
  else
    case "$ACTION" in
      install) k8s_install ;;
      uninstall) k8s_uninstall ;;
      delete-uninstall) k8s_delete_uninstall ;;
      status) k8s_status ;;
      backup-now) k8s_backup_now ;;
      restore) k8s_restore ;;
      version) k8s_version ;;
      migrate-down-to) k8s_migrate_down_to ;;
    esac
  fi
}
main
