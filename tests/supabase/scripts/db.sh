#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

usage() {
  cat <<'EOF'
Usage: bash tests/supabase/scripts/db.sh <create|destroy|reset|get> <iot|webshop>

Required environment variables:
  SUPABASE_DB_HOST
  SUPABASE_DB_PASSWORD

Optional environment variables:
  SUPABASE_DB_USER   (default: postgres)
  SUPABASE_DB_PORT   (default: 5432)
  SUPABASE_DB_NAME   (default: postgres)
EOF
}

if [[ $# -ne 2 ]]; then
  usage >&2
  exit 1
fi

ACTION="$1"
TARGET="$2"

if [[ -z "${SUPABASE_DB_HOST:-}" ]]; then
  echo "SUPABASE_DB_HOST is required." >&2
  exit 1
fi

if [[ -z "${SUPABASE_DB_PASSWORD:-}" ]]; then
  echo "SUPABASE_DB_PASSWORD is required." >&2
  exit 1
fi

DB_USER="${SUPABASE_DB_USER:-postgres}"
DB_PORT="${SUPABASE_DB_PORT:-5432}"
DB_NAME="${SUPABASE_DB_NAME:-postgres}"

case "$ACTION:$TARGET" in
  create:iot)
    SQL_FILE="$ROOT_DIR/sql/create_iot_tables.sql"
    ;;
  destroy:iot)
    SQL_FILE="$ROOT_DIR/sql/drop_iot_tables.sql"
    ;;
  reset:iot)
    SQL_FILE="$ROOT_DIR/sql/truncate_iot_tables.sql"
    ;;
  get:iot)
    SQL_FILE="$ROOT_DIR/sql/select_iot_tables.sql"
    ;;
  create:webshop)
    SQL_FILE="$ROOT_DIR/sql/create_webshop_tables.sql"
    ;;
  destroy:webshop)
    SQL_FILE="$ROOT_DIR/sql/drop_webshop_tables.sql"
    ;;
  reset:webshop)
    SQL_FILE="$ROOT_DIR/sql/truncate_webshop_tables.sql"
    ;;
  get:webshop)
    SQL_FILE="$ROOT_DIR/sql/select_webshop_tables.sql"
    ;;
  *)
    usage >&2
    exit 1
    ;;
esac

run_psql() {
  PGPASSWORD="$SUPABASE_DB_PASSWORD" \
    psql \
      -h "$SUPABASE_DB_HOST" \
      -p "$DB_PORT" \
      -U "$DB_USER" \
      -d "$DB_NAME" \
      -v ON_ERROR_STOP=1 \
      -f "$SQL_FILE"
}

run_docker_psql() {
  docker run --rm -i \
    -e PGPASSWORD="$SUPABASE_DB_PASSWORD" \
    postgres:18-alpine \
    psql \
      -h "$SUPABASE_DB_HOST" \
      -p "$DB_PORT" \
      -U "$DB_USER" \
      -d "$DB_NAME" \
      -v ON_ERROR_STOP=1 \
      -f - < "$SQL_FILE"
}

if command -v psql >/dev/null 2>&1; then
  run_psql
  exit 0
fi

if command -v docker >/dev/null 2>&1; then
  run_docker_psql
  exit 0
fi

echo "Neither 'psql' nor 'docker' is available to run the Supabase admin script." >&2
exit 1
