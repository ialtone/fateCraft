#!/bin/sh
set -eu

role=${1:-backend}
if [ "$#" -gt 0 ]; then
  shift
fi

case "$role" in
  all)
    exec /usr/bin/supervisord -n -c /etc/supervisord.conf
    ;;
  backend)
    exec su-exec postgres /app/backend/fatecraft "$@"
    ;;
  migrate)
    exec su-exec postgres /app/backend/fatecraft-migrate "$@"
    ;;
  gameplay-check)
    exec su-exec postgres /app/backend/fatecraft-gameplay-check "$@"
    ;;
  frontend)
    exec nginx -g 'daemon off;'
    ;;
  backup)
    if [ "${1:-backup-now}" = "daemon" ]; then
      exec /usr/local/bin/postgres-backup-scheduler
    fi
    exec /usr/local/bin/postgres-backup-now
    ;;
  *)
    echo "unknown fateCraft container role: $role" >&2
    exit 2
    ;;
esac
