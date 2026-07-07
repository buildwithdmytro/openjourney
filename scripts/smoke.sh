#!/bin/sh
set -eu

api_url="${OPENJOURNEY_API_URL:-http://localhost:8080}"
api_key="${OPENJOURNEY_DEV_API_KEY:-local-development-key}"
external_id="${OPENJOURNEY_EXTERNAL_ID:-smoke-$(date +%s)}"
occurred_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

curl --fail --silent --show-error \
  -H "Authorization: Bearer ${api_key}" \
  -H "Content-Type: application/json" \
  -d "{\"events\":[
    {\"event_type\":\"profile.updated\",\"schema_version\":1,\"external_id\":\"${external_id}\",
     \"idempotency_key\":\"${external_id}-profile\",\"occurred_at\":\"${occurred_at}\",
     \"payload\":{\"attributes\":{\"email\":\"${external_id}@example.test\",\"first_name\":\"Ada\"}}},
    {\"event_type\":\"consent.changed\",\"schema_version\":1,\"external_id\":\"${external_id}\",
     \"idempotency_key\":\"${external_id}-consent\",\"occurred_at\":\"${occurred_at}\",
     \"payload\":{\"channel\":\"email\",\"topic\":\"marketing\",\"state\":\"subscribed\",
     \"evidence\":{\"source\":\"smoke-test\"}}}
  ]}" \
  "${api_url}/v1/events/batch"

sleep 2
curl --fail --silent --show-error \
  -H "Authorization: Bearer ${api_key}" \
  "${api_url}/v1/profiles/${external_id}"
printf '\n'
