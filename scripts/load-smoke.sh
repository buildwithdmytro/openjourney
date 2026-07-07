#!/bin/sh
set -eu

api_url="${OPENJOURNEY_API_URL:-http://localhost:8080}"
api_key="${OPENJOURNEY_DEV_API_KEY:-local-development-key}"
k6_image="${OPENJOURNEY_K6_IMAGE:-grafana/k6:0.54.0}"
rate="${OPENJOURNEY_LOAD_RATE:-20}"
duration="${OPENJOURNEY_LOAD_DURATION:-30s}"
batch_size="${OPENJOURNEY_LOAD_BATCH_SIZE:-10}"
vus="${OPENJOURNEY_LOAD_VUS:-50}"
max_vus="${OPENJOURNEY_LOAD_MAX_VUS:-200}"
p99_ms="${OPENJOURNEY_LOAD_P99_MS:-500}"
max_failure_rate="${OPENJOURNEY_LOAD_MAX_FAILURE_RATE:-0.001}"

curl --fail --silent --show-error "${api_url}/health/ready" >/dev/null

docker run --rm --network host \
  -v "$(pwd)/tests/load:/scripts:ro" \
  -e ENDPOINT="${api_url}" \
  -e API_KEY="${api_key}" \
  -e RATE="${rate}" \
  -e DURATION="${duration}" \
  -e BATCH_SIZE="${batch_size}" \
  -e VUS="${vus}" \
  -e MAX_VUS="${max_vus}" \
  -e P99_MS="${p99_ms}" \
  -e MAX_FAILURE_RATE="${max_failure_rate}" \
  "${k6_image}" run /scripts/ingestion.js

printf 'Load smoke passed: %s batches/s for %s, batch size %s.\n' "${rate}" "${duration}" "${batch_size}"
