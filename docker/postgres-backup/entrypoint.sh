#!/bin/sh
set -eu

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing command: $1" >&2
    exit 1
  fi
}

require_command crond
require_command ln
require_command mkdir

BACKUP_DIR=${BACKUP_DIR:-/backups}
CRON_SCHEDULE=${BACKUP_CRON_SCHEDULE:-0 2 * * *}
TZ_NAME=${TZ:-Asia/Shanghai}

setup_timezone() {
  if [ -f "/usr/share/zoneinfo/$TZ_NAME" ]; then
    ln -snf "/usr/share/zoneinfo/$TZ_NAME" /etc/localtime
    echo "$TZ_NAME" >/etc/timezone
  fi
}

write_crontab() {
  mkdir -p "$BACKUP_DIR"

  cat > /etc/crontabs/root <<EOF
SHELL=/bin/sh
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
TZ=$TZ_NAME
$CRON_SCHEDULE /usr/local/bin/postgres-backup-now > /proc/1/fd/1 2> /proc/1/fd/2
EOF
}

setup_timezone

case "${1:-daemon}" in
  daemon)
    write_crontab
    echo "Starting PostgreSQL backup scheduler: $CRON_SCHEDULE ($TZ_NAME)"
    exec crond -f -l 2 -L /dev/stdout
    ;;
  backup-now)
    exec /usr/local/bin/postgres-backup-now
    ;;
  *)
    exec "$@"
    ;;
esac
