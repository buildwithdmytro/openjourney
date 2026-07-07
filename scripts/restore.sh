#!/bin/sh
set -eu

backup="${1:?usage: restore.sh BACKUP.sql.gz}"
database_url="${OPENJOURNEY_DATABASE_URL:-postgres://openjourney:openjourney@localhost:5432/openjourney?sslmode=disable}"

if [ "${OPENJOURNEY_CONFIRM_RESTORE:-}" != "yes" ]; then
  echo "Set OPENJOURNEY_CONFIRM_RESTORE=yes to acknowledge that restore mutates the target database." >&2
  exit 2
fi

gzip -t "${backup}"
temporary="$(mktemp)"
trap 'rm -f "${temporary}"' EXIT
gzip -dc "${backup}" >"${temporary}"
test -s "${temporary}"
if [ -n "${OPENJOURNEY_POSTGRES_CLIENT_IMAGE:-}" ]; then
  docker run --rm --network host -i "${OPENJOURNEY_POSTGRES_CLIENT_IMAGE}" \
    psql "${database_url}" --set ON_ERROR_STOP=on <"${temporary}"
else
  psql "${database_url}" --set ON_ERROR_STOP=on <"${temporary}"
fi
printf 'Restore completed from %s\n' "${backup}"
