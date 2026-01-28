#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
SQL_FILE="$ROOT_DIR/sql/truncate_tables.sql"
DB_USER="postgres"
DB_NAME="postgres"


if [[ -z "${SUPABASE_DB_URL:-}" ]]; then
  echo "SUPABASE_DB_URL is required (Postgres connection string)." >&2
  exit 1
fi

if command -v psql >/dev/null 2>&1; then
  psql -h "${SUPABASE_DB_URL}" -p 5432 -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 -f "$SQL_FILE"
  exit 0
fi

if command -v docker >/dev/null 2>&1; then
  docker run --rm -i postgres:18-alpine psql -h "${SUPABASE_DB_URL}" -p 5432 -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 -f - < "$SQL_FILE"
  exit 0
fi

echo "Neither 'psql' nor 'docker' is available to run truncate." >&2
exit 1
