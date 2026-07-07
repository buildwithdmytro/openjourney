#!/bin/sh
set -eu

compose_files="${OPENJOURNEY_COMPOSE_FILES:--f compose.yaml}"
api_url="${OPENJOURNEY_API_URL:-http://localhost:8080}"
api_key="${OPENJOURNEY_DEV_API_KEY:-local-development-key}"
external_id="worker-termination-$(date +%s)"
occurred_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
delayed_worker="openjourney-worker-termination-${external_id}"

cleanup() {
  docker rm -f "${delayed_worker}" >/dev/null 2>&1 || true
  docker compose ${compose_files} up -d worker >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker compose ${compose_files} build api worker >/dev/null
docker compose ${compose_files} up -d api >/dev/null

attempt=0
while [ "${attempt}" -lt 60 ]; do
  if curl --fail --silent --show-error "${api_url}/health/ready" >/dev/null 2>&1; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
curl --fail --silent --show-error "${api_url}/health/ready" >/dev/null

docker compose ${compose_files} stop worker >/dev/null 2>&1 || true

curl --fail --silent --show-error \
  -H "Authorization: Bearer ${api_key}" \
  -H "Content-Type: application/json" \
  -d "{\"events\":[{\"event_type\":\"profile.updated\",\"schema_version\":1,\"external_id\":\"${external_id}\",
       \"idempotency_key\":\"${external_id}-profile\",\"occurred_at\":\"${occurred_at}\",
       \"payload\":{\"attributes\":{\"termination_smoke\":true}}}]}" \
  "${api_url}/v1/events/batch" >/dev/null

event_id=""
attempt=0
while [ "${attempt}" -lt 30 ]; do
  event_id="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
    "select id from accepted_events where external_id='${external_id}' order by received_at desc limit 1")"
  if [ -n "${event_id}" ]; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
test -n "${event_id}"

docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
  "with floor as (select coalesce(min(sequence),0)-1 as sequence from projection_jobs)
   update projection_jobs set sequence=floor.sequence from floor where event_id='${event_id}'" >/dev/null

status="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
  "select status from projection_jobs where event_id='${event_id}'")"
test "${status}" = "pending"

docker rm -f "${delayed_worker}" >/dev/null 2>&1 || true
docker compose ${compose_files} run -d --name "${delayed_worker}" --no-deps worker \
  -watch=false -max-items=1 -max-duration=30s -after-claim-delay=20s >/dev/null

attempt=0
while [ "${attempt}" -lt 30 ]; do
  status="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
    "select status from projection_jobs where event_id='${event_id}'")"
  if [ "${status}" = "processing" ]; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
test "${status}" = "processing"

docker kill "${delayed_worker}" >/dev/null
docker rm "${delayed_worker}" >/dev/null

docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
  "update projection_jobs set locked_until=now()-interval '1 second' where event_id='${event_id}'" >/dev/null

docker compose ${compose_files} up -d worker >/dev/null

attempt=0
while [ "${attempt}" -lt 30 ]; do
  status="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
    "select status from projection_jobs where event_id='${event_id}'")"
  if [ "${status}" = "done" ]; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
test "${status}" = "done"

curl --fail --silent --show-error \
  -H "Authorization: Bearer ${api_key}" \
  "${api_url}/v1/profiles/${external_id}" >/dev/null

printf 'Worker termination smoke passed for event %s\n' "${event_id}"
