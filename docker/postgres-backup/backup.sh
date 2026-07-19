#!/bin/sh
set -eu

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing command: $1" >&2
    exit 1
  fi
}

require_value() {
  name="$1"
  value="$2"

  if [ -z "$value" ]; then
    echo "missing required value: $name" >&2
    exit 1
  fi
}

RETENTION_DAYS=${BACKUP_RETENTION_DAYS:-7}
BACKUP_DIR=${BACKUP_DIR:-/backups}
db_host=${BACKUP_DB_HOST:-${POSTGRES_HOST:-}}
db_port=${BACKUP_DB_PORT:-${POSTGRES_PORT:-}}
db_name=${POSTGRES_DB:-}
db_user=${POSTGRES_USER:-}
db_password=${POSTGRES_PASSWORD:-}
db_sslmode=${BACKUP_DB_SSLMODE:-${POSTGRES_SSLMODE:-prefer}}

case "$RETENTION_DAYS" in
  '' | *[!0-9]*)
    echo "BACKUP_RETENTION_DAYS must be a non-negative integer" >&2
    exit 1
    ;;
esac

require_command date
require_command find
require_command mkdir
require_command pg_dump

require_value POSTGRES_HOST "$db_host"
require_value POSTGRES_PORT "$db_port"
require_value POSTGRES_DB "$db_name"
require_value POSTGRES_USER "$db_user"
require_value POSTGRES_PASSWORD "$db_password"

mkdir -p "$BACKUP_DIR"

backup_prefix=${BACKUP_NAME_PREFIX:-$db_name}

timestamp=$(date +"%Y%m%d_%H%M%S")
backup_name="${backup_prefix}_${timestamp}.dump"
backup_path="$BACKUP_DIR/$backup_name"

echo "Creating PostgreSQL backup: $backup_path"

PGPASSWORD="$db_password" \
PGSSLMODE="$db_sslmode" \
pg_dump \
  --host="$db_host" \
  --port="$db_port" \
  --username="$db_user" \
  --dbname="$db_name" \
  --format=custom \
  --no-owner \
  --no-privileges \
  --file="$backup_path"

if [ "$RETENTION_DAYS" -gt 0 ]; then
  find "$BACKUP_DIR" -type f -name "${backup_prefix}_*.dump" -mtime "+$RETENTION_DAYS" -delete
fi

echo "Backup completed: $backup_path"
