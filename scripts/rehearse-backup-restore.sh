#!/bin/sh
set -eu

source_database_url="${OPENJOURNEY_DATABASE_URL:-postgres://openjourney:openjourney@localhost:5432/openjourney?sslmode=disable}"
admin_database_url="${OPENJOURNEY_REHEARSAL_ADMIN_DATABASE_URL:-postgres://openjourney:openjourney@localhost:5432/postgres?sslmode=disable}"
client_image="${OPENJOURNEY_POSTGRES_CLIENT_IMAGE:-postgres:17-alpine}"
temporary_dir="$(mktemp -d)"
restore_database="openjourney_restore_$(date -u +%Y%m%d%H%M%S)_$$"
backup_path="${temporary_dir}/backup.sql.gz"

cleanup() {
  OPENJOURNEY_POSTGRES_CLIENT_IMAGE="${client_image}" run_psql "${admin_database_url}" \
    "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='${restore_database}'" >/dev/null 2>&1 || true
  OPENJOURNEY_POSTGRES_CLIENT_IMAGE="${client_image}" run_psql "${admin_database_url}" \
    "DROP DATABASE IF EXISTS ${restore_database}" >/dev/null 2>&1 || true
  rm -rf "${temporary_dir}"
}
trap cleanup EXIT

run_psql() {
  database_url="$1"
  sql="$2"
  if [ -n "${OPENJOURNEY_REHEARSAL_USE_HOST_CLIENT:-}" ]; then
    psql "${database_url}" --set ON_ERROR_STOP=on --tuples-only --no-align --command "${sql}"
  else
    docker run --rm --network host "${client_image}" \
      psql "${database_url}" --set ON_ERROR_STOP=on --tuples-only --no-align --command "${sql}"
  fi
}

derive_restore_url() {
  url="$1"
  database="$2"
  query=""
  without_query="${url}"
  case "${url}" in
    *\?*)
      query="?${url#*\?}"
      without_query="${url%%\?*}"
      ;;
  esac
  printf '%s/%s%s' "${without_query%/*}" "${database}" "${query}"
}

table_counts() {
  database_url="$1"
  run_psql "${database_url}" "
    SELECT 'accepted_events=' || count(*) FROM accepted_events
    UNION ALL SELECT 'profiles=' || count(*) FROM profiles
    UNION ALL SELECT 'consent_ledger=' || count(*) FROM consent_ledger
    UNION ALL SELECT 'api_keys=' || count(*) FROM api_keys
    UNION ALL SELECT 'schema_migrations=' || count(*) FROM schema_migrations
    ORDER BY 1"
}

restore_database_url="$(derive_restore_url "${source_database_url}" "${restore_database}")"

OPENJOURNEY_POSTGRES_CLIENT_IMAGE="${client_image}" \
  OPENJOURNEY_DATABASE_URL="${source_database_url}" \
  ./scripts/backup.sh "${backup_path}" >/dev/null

run_psql "${admin_database_url}" "CREATE DATABASE ${restore_database}" >/dev/null

OPENJOURNEY_CONFIRM_RESTORE=yes \
  OPENJOURNEY_POSTGRES_CLIENT_IMAGE="${client_image}" \
  OPENJOURNEY_DATABASE_URL="${restore_database_url}" \
  ./scripts/restore.sh "${backup_path}" >/dev/null

source_counts="${temporary_dir}/source.counts"
restore_counts="${temporary_dir}/restore.counts"
table_counts "${source_database_url}" >"${source_counts}"
table_counts "${restore_database_url}" >"${restore_counts}"

if ! diff -u "${source_counts}" "${restore_counts}"; then
  echo "Backup/restore rehearsal failed: restored core table counts differ." >&2
  exit 1
fi

run_psql "${restore_database_url}" "SELECT count(*) FROM tenants" >/dev/null
gzip -t "${backup_path}"

printf 'Backup/restore rehearsal passed using temporary database %s\n' "${restore_database}"
