#!/bin/sh
set -eu

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing command: $1" >&2
    exit 1
  fi
}

require_command crond
require_command dirname
require_command ln
require_command mkdir

BACKUP_DIR=${BACKUP_DIR:-/backups}
CRON_SCHEDULE=${BACKUP_CRON_SCHEDULE:-0 2 * * *}
TZ_NAME=${TZ:-Asia/Shanghai}
CRON_LOG_FILE=${BACKUP_LOG_FILE:-$BACKUP_DIR/backup.log}

setup_timezone() {
  if [ -f "/usr/share/zoneinfo/$TZ_NAME" ]; then
    ln -snf "/usr/share/zoneinfo/$TZ_NAME" /etc/localtime
    echo "$TZ_NAME" >/etc/timezone
  fi
}

write_crontab() {
  mkdir -p "$BACKUP_DIR"
  mkdir -p "$(dirname "$CRON_LOG_FILE")"

  cat > /etc/crontabs/root <<EOF
SHELL=/bin/sh
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
TZ=$TZ_NAME
$CRON_SCHEDULE /usr/local/bin/postgres-backup-now >> $CRON_LOG_FILE 2>&1
EOF
}

setup_timezone

case "${1:-daemon}" in
  daemon)
    write_crontab
    echo "Starting PostgreSQL backup scheduler: $CRON_SCHEDULE ($TZ_NAME)"
    exec crond -f -l 2
    ;;
  backup-now)
    exec /usr/local/bin/postgres-backup-now
    ;;
  *)
    exec "$@"
    ;;
esac
