#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

usage() {
  cat <<'EOF'
Usage: bash tests/supabase/scripts/db.sh <create|destroy|reset|get> <iot|webshop> [all|faasd|tinyfaas]

Required environment variables:
  SUPABASE_DB_HOST
  SUPABASE_DB_PASSWORD

Optional environment variables:
  SUPABASE_DB_USER   (default: postgres)
  SUPABASE_DB_PORT   (default: 5432)
  SUPABASE_DB_NAME   (default: postgres)
Optional platform selector:
  all               (default; runs for both faasd and tinyfaas)
  faasd             (runs only for faasd-prefixed tables)
  tinyfaas          (runs only for tinyfaas-prefixed tables)
EOF
}

if [[ $# -lt 2 || $# -gt 3 ]]; then
  usage >&2
  exit 1
fi

ACTION="$1"
TARGET="$2"
PLATFORM_SCOPE="${3:-all}"

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
SQL_FILE=""

case "$PLATFORM_SCOPE" in
  all | faasd | tinyfaas)
    ;;
  *)
    usage >&2
    exit 1
    ;;
esac

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

build_sql_vars() {
  local platform="$1"

  case "$TARGET" in
    iot)
      SQL_VARS=(
        -v "sensor_data_table=${platform}_sensor_data"
        -v "use_case_table=${platform}_use_case"
        -v "sensor_data_updated_at_idx=${platform}_sensor_data_updated_at_idx"
        -v "use_case_updated_at_idx=${platform}_use_case_updated_at_idx"
        -v "sensor_data_updated_at_trigger=trg_${platform}_sensor_data_updated_at"
        -v "use_case_updated_at_trigger=trg_${platform}_use_case_updated_at"
      )
      ;;
    webshop)
      SQL_VARS=(
        -v "cart_table=${platform}_webshop_cart"
        -v "cart_user_id_idx=${platform}_webshop_cart_user_id_idx"
        -v "cart_updated_at_idx=${platform}_webshop_cart_updated_at_idx"
        -v "cart_updated_at_trigger=trg_${platform}_webshop_cart_updated_at"
      )
      ;;
  esac
}

selected_platforms() {
  case "$PLATFORM_SCOPE" in
    all)
      printf '%s\n' faasd tinyfaas
      ;;
    *)
      printf '%s\n' "$PLATFORM_SCOPE"
      ;;
  esac
}

run_psql() {
  local platform="$1"
  build_sql_vars "$platform"
  printf 'Running %s %s for %s\n' "$ACTION" "$TARGET" "$platform" >&2
  PGPASSWORD="$SUPABASE_DB_PASSWORD" \
    psql \
      -h "$SUPABASE_DB_HOST" \
      -p "$DB_PORT" \
      -U "$DB_USER" \
      -d "$DB_NAME" \
      -v ON_ERROR_STOP=1 \
      "${SQL_VARS[@]}" \
      -f "$SQL_FILE"
}

run_docker_psql() {
  local platform="$1"
  build_sql_vars "$platform"
  printf 'Running %s %s for %s\n' "$ACTION" "$TARGET" "$platform" >&2
  docker run --rm -i \
    -e PGPASSWORD="$SUPABASE_DB_PASSWORD" \
    postgres:18-alpine \
    psql \
      -h "$SUPABASE_DB_HOST" \
      -p "$DB_PORT" \
      -U "$DB_USER" \
      -d "$DB_NAME" \
      -v ON_ERROR_STOP=1 \
      "${SQL_VARS[@]}" \
      -f - < "$SQL_FILE"
}

if command -v psql >/dev/null 2>&1; then
  while IFS= read -r platform; do
    run_psql "$platform"
  done < <(selected_platforms)
  exit 0
fi

if command -v docker >/dev/null 2>&1; then
  while IFS= read -r platform; do
    run_docker_psql "$platform"
  done < <(selected_platforms)
  exit 0
fi

echo "Neither 'psql' nor 'docker' is available to run the Supabase admin script." >&2
exit 1
