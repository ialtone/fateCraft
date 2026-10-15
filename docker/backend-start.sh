#!/bin/sh
set -eu

timeout_seconds=${DATABASE_START_TIMEOUT_SECONDS:-120}
elapsed=0

until pg_isready -h 127.0.0.1 -p "${POSTGRES_PORT:-5432}" -U "${POSTGRES_USER:-fatecraft}" -d "${POSTGRES_DB:-fatecraft}" >/dev/null 2>&1; do
  if [ "$elapsed" -ge "$timeout_seconds" ]; then
    echo "PostgreSQL did not become ready within ${timeout_seconds}s" >&2
    exit 1
  fi
  sleep 2
  elapsed=$((elapsed + 2))
done

echo "Applying database migrations"
su-exec postgres /app/backend/fatecraft-migrate up
echo "Starting fateCraft backend"
exec su-exec postgres /app/backend/fatecraft
