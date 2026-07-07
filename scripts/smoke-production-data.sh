#!/bin/sh
set -eu

compose_files="-f compose.yaml -f compose.production-data.yaml"
external_id="production-smoke-$(date +%s)"
smoke_output="$(mktemp)"
trap 'rm -f "${smoke_output}"' EXIT

export OPENJOURNEY_EXTERNAL_ID="${external_id}"
./scripts/smoke.sh >"${smoke_output}"

attempt=0
event_id=""
while [ "${attempt}" -lt 30 ]; do
  event_id="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
    "select id from accepted_events where external_id='${external_id}' order by occurred_at limit 1")"
  if [ -n "${event_id}" ]; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
test -n "${event_id}"

tenant_id="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
  "select tenant_id from accepted_events where id='${event_id}'")"
event_date="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
  "select to_char(occurred_at at time zone 'UTC','YYYY/MM/DD') from accepted_events where id='${event_id}'")"
object_key="events/${tenant_id}/${event_date}/${event_id}.json"

attempt=0
while [ "${attempt}" -lt 30 ]; do
  analytics_count="$(docker compose ${compose_files} exec -T clickhouse \
    clickhouse-client --user openjourney --password openjourney-secret \
    --query "select count() from openjourney.behavior_events where event_id='${event_id}'")"
  if [ "${analytics_count}" = "1" ]; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
test "${analytics_count}" = "1"

outbox_status="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
  "select status from outbox_events where event_id='${event_id}'")"
test "${outbox_status}" = "published"

docker run --rm --network openjourney_default --entrypoint /bin/sh \
  minio/mc:RELEASE.2025-04-16T18-13-26Z -c \
  "mc alias set local http://object-store:9000 openjourney openjourney-secret >/dev/null &&
   mc stat 'local/openjourney/${object_key}' >/dev/null"

printf 'Production data smoke passed for event %s\n' "${event_id}"
