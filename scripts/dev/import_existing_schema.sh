#!/usr/bin/env bash
set -euo pipefail

print_usage() {
  cat <<'USAGE'
Usage:
  scripts/dev/import_existing_schema.sh --schema <schema.sql> [options]

Options:
  --schema <path>            Path to SQL schema file to import (required)
  --config <path>            goframe config path (default: goframe.yaml)
  --goframe-bin <name/path>  goframe executable (default: goframe)
  --migrations <dir>         migrations directory (default: migrations)
  --baseline-name <name>     baseline migration suffix (default: baseline_existing_schema)
  --models-output <path>     inspectdb output file (default: internal/models/legacy_models.go)
  --models-package <name>    inspectdb package name (default: models)
  --tables <csv>             Optional inspectdb include table list
  --exclude <csv>            Optional inspectdb exclude table list
  --skip-import              Skip schema SQL import step
  --skip-inspectdb           Skip inspectdb model generation step
  --skip-baseline            Skip baseline migration creation step
  --skip-stamp               Skip marking baseline migration as applied
  -h, --help                 Show help

Flow:
  1) Import schema.sql into configured DB (via goframe shell)
  2) Generate Go models with inspectdb
  3) Create baseline migration file pair and copy schema.sql into .up.sql
  4) Mark baseline migration as applied in goframe_schema_migrations

Examples:
  scripts/dev/import_existing_schema.sh --schema db/schema.sql
  scripts/dev/import_existing_schema.sh --schema schema.sql --tables users,orders --exclude audit_logs
  scripts/dev/import_existing_schema.sh --schema schema.sql --skip-import --skip-stamp
USAGE
}

normalize_slug() {
  local raw="$1"
  local out
  out="$(printf '%s' "$raw" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_]+/_/g; s/^_+//; s/_+$//')"
  if [[ -z "$out" ]]; then
    out="baseline_existing_schema"
  fi
  printf '%s' "$out"
}

sql_quote() {
  local raw="$1"
  printf '%s' "$raw" | sed "s/'/''/g"
}

SCHEMA_FILE=""
CONFIG_PATH="goframe.yaml"
GOFRAME_BIN="goframe"
MIGRATIONS_DIR="migrations"
BASELINE_NAME="baseline_existing_schema"
MODELS_OUTPUT="internal/models/legacy_models.go"
MODELS_PACKAGE="models"
TABLES=""
EXCLUDE=""
SKIP_IMPORT=0
SKIP_INSPECTDB=0
SKIP_BASELINE=0
SKIP_STAMP=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --schema)
      SCHEMA_FILE="${2:-}"
      shift 2
      ;;
    --config)
      CONFIG_PATH="${2:-}"
      shift 2
      ;;
    --goframe-bin)
      GOFRAME_BIN="${2:-}"
      shift 2
      ;;
    --migrations)
      MIGRATIONS_DIR="${2:-}"
      shift 2
      ;;
    --baseline-name)
      BASELINE_NAME="${2:-}"
      shift 2
      ;;
    --models-output)
      MODELS_OUTPUT="${2:-}"
      shift 2
      ;;
    --models-package)
      MODELS_PACKAGE="${2:-}"
      shift 2
      ;;
    --tables)
      TABLES="${2:-}"
      shift 2
      ;;
    --exclude)
      EXCLUDE="${2:-}"
      shift 2
      ;;
    --skip-import)
      SKIP_IMPORT=1
      shift
      ;;
    --skip-inspectdb)
      SKIP_INSPECTDB=1
      shift
      ;;
    --skip-baseline)
      SKIP_BASELINE=1
      shift
      ;;
    --skip-stamp)
      SKIP_STAMP=1
      shift
      ;;
    -h|--help)
      print_usage
      exit 0
      ;;
    *)
      echo "error: unknown option: $1" >&2
      print_usage >&2
      exit 1
      ;;
  esac
done

if [[ $SKIP_BASELINE -eq 1 && $SKIP_STAMP -eq 0 ]]; then
  echo "error: --skip-baseline cannot be combined with baseline stamping" >&2
  echo "hint: add --skip-stamp too, or keep baseline creation enabled" >&2
  exit 1
fi

if [[ -z "${SCHEMA_FILE}" ]]; then
  echo "error: --schema is required" >&2
  print_usage >&2
  exit 1
fi

if [[ ! -f "${SCHEMA_FILE}" ]]; then
  echo "error: schema file not found: ${SCHEMA_FILE}" >&2
  exit 1
fi

if [[ ! -f "${CONFIG_PATH}" ]]; then
  echo "error: config file not found: ${CONFIG_PATH}" >&2
  exit 1
fi

if ! command -v "${GOFRAME_BIN}" >/dev/null 2>&1; then
  echo "error: goframe executable not found: ${GOFRAME_BIN}" >&2
  echo "hint: install goframe or pass --goframe-bin <path>" >&2
  exit 1
fi

BASELINE_SLUG="$(normalize_slug "${BASELINE_NAME}")"

echo "==> Existing schema import automation"
echo "    schema:      ${SCHEMA_FILE}"
echo "    config:      ${CONFIG_PATH}"
echo "    goframe:     ${GOFRAME_BIN}"
echo "    migrations:  ${MIGRATIONS_DIR}"
echo "    baseline:    ${BASELINE_SLUG}"
echo "    models out:  ${MODELS_OUTPUT}"

if [[ $SKIP_IMPORT -eq 0 ]]; then
  echo "==> Step 1/4: Import schema SQL into configured database"
  "${GOFRAME_BIN}" shell --config "${CONFIG_PATH}" < "${SCHEMA_FILE}"
else
  echo "==> Step 1/4: Skipped (--skip-import)"
fi

if [[ $SKIP_INSPECTDB -eq 0 ]]; then
  echo "==> Step 2/4: Generate models with inspectdb"
  inspect_args=(inspectdb --config "${CONFIG_PATH}" --package "${MODELS_PACKAGE}" --output "${MODELS_OUTPUT}")
  if [[ -n "${TABLES}" ]]; then
    inspect_args+=(--tables "${TABLES}")
  fi
  if [[ -n "${EXCLUDE}" ]]; then
    inspect_args+=(--exclude "${EXCLUDE}")
  fi
  "${GOFRAME_BIN}" "${inspect_args[@]}"
else
  echo "==> Step 2/4: Skipped (--skip-inspectdb)"
fi

MIGRATION_ID=""
MIGRATION_UP=""
MIGRATION_DOWN=""

if [[ $SKIP_BASELINE -eq 0 ]]; then
  echo "==> Step 3/4: Create baseline migration and copy schema SQL"
  mkdir -p "${MIGRATIONS_DIR}"
  "${GOFRAME_BIN}" migrate --config "${CONFIG_PATH}" --migrations "${MIGRATIONS_DIR}" create "${BASELINE_SLUG}"

  MIGRATION_UP="$(ls -1 "${MIGRATIONS_DIR}"/*_"${BASELINE_SLUG}".up.sql 2>/dev/null | sort | tail -n 1 || true)"
  if [[ -z "${MIGRATION_UP}" ]]; then
    echo "error: could not resolve generated baseline .up.sql for ${BASELINE_SLUG}" >&2
    exit 1
  fi
  MIGRATION_ID="$(basename "${MIGRATION_UP}" .up.sql)"
  MIGRATION_DOWN="${MIGRATIONS_DIR}/${MIGRATION_ID}.down.sql"

  {
    echo "-- Baseline schema imported from ${SCHEMA_FILE}"
    echo "-- Generated at (UTC): $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
    echo "-- NOTE: This baseline is intended for new environments."
    echo
    cat "${SCHEMA_FILE}"
    echo
  } > "${MIGRATION_UP}"

  if [[ -f "${MIGRATION_DOWN}" ]]; then
    {
      echo "-- Baseline down migration for ${MIGRATION_ID}"
      echo "-- WARNING: automatic rollback of imported schema is intentionally not provided."
      echo "-- Add manual rollback SQL for your database engine if your process requires it."
      echo
      echo "-- Write your SQL here"
    } > "${MIGRATION_DOWN}"
  fi

  echo "    baseline up:   ${MIGRATION_UP}"
  echo "    baseline down: ${MIGRATION_DOWN}"
else
  echo "==> Step 3/4: Skipped (--skip-baseline)"
fi

if [[ $SKIP_STAMP -eq 0 ]]; then
  echo "==> Step 4/4: Mark baseline migration as applied"
  if [[ -z "${MIGRATION_ID}" ]]; then
    echo "error: baseline migration id is empty; cannot stamp applied state" >&2
    exit 1
  fi

  migration_id_sql="$(sql_quote "${MIGRATION_ID}")"
  applied_at_sql="$(sql_quote "$(date -u +"%Y-%m-%dT%H:%M:%SZ")")"
  read -r -d '' stamp_sql <<SQL || true
CREATE TABLE IF NOT EXISTS goframe_schema_migrations (
  id VARCHAR(255) PRIMARY KEY,
  applied_at TEXT NOT NULL
);
DELETE FROM goframe_schema_migrations WHERE id = '${migration_id_sql}';
INSERT INTO goframe_schema_migrations (id, applied_at) VALUES ('${migration_id_sql}', '${applied_at_sql}');
SQL
  "${GOFRAME_BIN}" shell --config "${CONFIG_PATH}" --command "${stamp_sql}"
else
  echo "==> Step 4/4: Skipped (--skip-stamp)"
fi

echo "==> Completed"
echo "    models file: ${MODELS_OUTPUT}"
if [[ -n "${MIGRATION_ID}" ]]; then
  echo "    baseline id: ${MIGRATION_ID}"
fi
echo
echo "Next recommended checks:"
echo "  1) go test ./..."
echo "  2) ${GOFRAME_BIN} migrate --config ${CONFIG_PATH} --migrations ${MIGRATIONS_DIR} status"
