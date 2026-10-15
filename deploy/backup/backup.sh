#!/usr/bin/env bash
# Scheduled MariaDB backups for cactusInv.
#   backup.sh once            take one backup now
#   backup.sh loop            take backups daily at each of BACKUP_TIMES (e.g. "02:00,14:00")
#   backup.sh restore FILE    restore a backup (.sql.gz) into the database
# Backups are consistent (--single-transaction), gzip-compressed, written atomically to
# /backups and pruned after BACKUP_RETENTION_DAYS days.
set -euo pipefail

DB_HOST=${DB_HOST:-mariadb}
DB_NAME=${DB_NAME:-cactusinv}
DB_USER=${DB_USER:-cactusinv}
BACKUP_DIR=${BACKUP_DIR:-/backups}
BACKUP_TIMES=${BACKUP_TIMES:-02:00}
BACKUP_RETENTION_DAYS=${BACKUP_RETENTION_DAYS:-14}

password() {
  if [[ -n "${DB_PASSWORD_FILE:-}" ]]; then cat "$DB_PASSWORD_FILE"; else printf '%s' "${DB_PASSWORD:?DB_PASSWORD or DB_PASSWORD_FILE is required}"; fi
}

log() { printf '%s backup: %s\n' "$(date -Iseconds)" "$*"; }

once() {
  mkdir -p "$BACKUP_DIR"
  local name
  name="cactusinv-$(date +%Y%m%d-%H%M%S).sql.gz"
  local tmp="$BACKUP_DIR/.${name}.part"
  MYSQL_PWD="$(password)" mariadb-dump --host="$DB_HOST" --user="$DB_USER" \
    --single-transaction --routines --triggers --events --hex-blob "$DB_NAME" | gzip -9 >"$tmp"
  mv "$tmp" "$BACKUP_DIR/$name"
  log "wrote $name ($(du -h "$BACKUP_DIR/$name" | cut -f1))"
  find "$BACKUP_DIR" -maxdepth 1 -name 'cactusinv-*.sql.gz' -mtime "+$BACKUP_RETENTION_DAYS" -print -delete | sed 's/^/pruned /' || true
}

# seconds_until_next prints the seconds until the next of BACKUP_TIMES.
seconds_until_next() {
  local now best="" t target
  now=$(date +%s)
  IFS=',' read -ra times <<<"$BACKUP_TIMES"
  for t in "${times[@]}"; do
    t=$(echo "$t" | tr -d ' ')
    [[ "$t" =~ ^([01][0-9]|2[0-3]):[0-5][0-9]$ ]] || { log "ignoring invalid time '$t'"; continue; }
    target=$(date -d "today $t" +%s)
    ((target <= now)) && target=$(date -d "tomorrow $t" +%s)
    if [[ -z "$best" ]] || ((target < best)); then best=$target; fi
  done
  [[ -n "$best" ]] || { log "no valid BACKUP_TIMES"; exit 1; }
  echo $((best - now))
}

case "${1:-once}" in
  once) once ;;
  loop)
    log "scheduled daily at $BACKUP_TIMES (${TZ:-UTC}), keeping $BACKUP_RETENTION_DAYS days"
    while true; do
      wait_s=$(seconds_until_next)
      log "next backup in ${wait_s}s"
      sleep "$wait_s"
      once || log "backup FAILED"
      sleep 1
    done
    ;;
  restore)
    file=${2:?usage: backup.sh restore FILE}
    [[ -f "$file" ]] || file="$BACKUP_DIR/$file"
    log "restoring $file into $DB_NAME"
    gunzip -c "$file" | MYSQL_PWD="$(password)" mariadb --host="$DB_HOST" --user="$DB_USER" "$DB_NAME"
    log "restore complete"
    ;;
  *) echo "usage: backup.sh once|loop|restore FILE" >&2; exit 2 ;;
esac
