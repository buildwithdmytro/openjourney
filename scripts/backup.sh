#!/bin/sh
set -eu

destination="${1:-openjourney-backup-$(date -u +%Y%m%dT%H%M%SZ).sql.gz}"
database_url="${OPENJOURNEY_DATABASE_URL:-postgres://openjourney:openjourney@localhost:5432/openjourney?sslmode=disable}"
temporary="$(mktemp)"
trap 'rm -f "${temporary}"' EXIT

if [ -n "${OPENJOURNEY_POSTGRES_CLIENT_IMAGE:-}" ]; then
  docker run --rm --network host "${OPENJOURNEY_POSTGRES_CLIENT_IMAGE}" \
    pg_dump --dbname="${database_url}" --format=plain --no-owner --no-privileges >"${temporary}"
else
  pg_dump --dbname="${database_url}" --format=plain --no-owner --no-privileges >"${temporary}"
fi
test -s "${temporary}"
gzip -9 <"${temporary}" >"${destination}"
gzip -t "${destination}"
printf 'Backup written to %s\n' "${destination}"
