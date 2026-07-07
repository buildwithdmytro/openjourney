#!/bin/sh
set -eu

api_url="${OPENJOURNEY_API_URL:-http://localhost:8080}"
api_key="${OPENJOURNEY_DEV_API_KEY:-local-development-key}"
collector_image="${OPENJOURNEY_OTEL_COLLECTOR_IMAGE:-otel/opentelemetry-collector-contrib:0.128.0}"
collector_name="openjourney-otel-smoke"
compose_files="${OPENJOURNEY_COMPOSE_FILES:--f compose.yaml}"
temporary_dir="$(mktemp -d)"
config_file="${temporary_dir}/otel-collector.yaml"
trace_file="${temporary_dir}/traces.json"
chmod 0777 "${temporary_dir}"

cleanup() {
  docker rm -f "${collector_name}" >/dev/null 2>&1 || true
  rm -rf "${temporary_dir}"
}
trap cleanup EXIT

cat >"${config_file}" <<'YAML'
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318
exporters:
  file:
    path: /out/traces.json
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [file]
YAML

docker rm -f "${collector_name}" >/dev/null 2>&1 || true
docker run -d --name "${collector_name}" --network openjourney_default \
  -v "${config_file}:/etc/otelcol-contrib/config.yaml:ro" \
  -v "${temporary_dir}:/out" \
  "${collector_image}" >/dev/null

collector_ip="$(docker inspect -f '{{range.NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${collector_name}")"
test -n "${collector_ip}"

OPENJOURNEY_OTLP_ENDPOINT="${collector_ip}:4318" \
  docker compose ${compose_files} up -d --force-recreate api >/dev/null

attempt=0
until curl --fail --silent --show-error "${api_url}/health/ready" >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "${attempt}" -ge 30 ]; then
    echo "API did not become ready with OTLP enabled." >&2
    exit 1
  fi
  sleep 1
done

curl --fail --silent --show-error "${api_url}/health/live" >/dev/null
curl --fail --silent --show-error "${api_url}/metrics" | grep -E '(^http_|^target_|^go_)' >/dev/null

external_id="telemetry-smoke-$(date +%s)"
occurred_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
curl --fail --silent --show-error \
  -H "Authorization: Bearer ${api_key}" \
  -H "Content-Type: application/json" \
  -d "{\"events\":[{
    \"event_type\":\"profile.updated\",
    \"schema_version\":1,
    \"external_id\":\"${external_id}\",
    \"idempotency_key\":\"${external_id}-profile\",
    \"occurred_at\":\"${occurred_at}\",
    \"payload\":{\"attributes\":{\"source\":\"telemetry-smoke\"}}
  }]}" \
  "${api_url}/v1/events/batch" >/dev/null

attempt=0
while [ "${attempt}" -lt 30 ]; do
  if [ -s "${trace_file}" ] && grep -q 'openjourney-api' "${trace_file}" &&
     grep -q '/v1/events/batch' "${trace_file}"; then
    printf 'Telemetry smoke passed; API trace exported and metrics scraped.\n'
    exit 0
  fi
  attempt=$((attempt + 1))
  sleep 1
done

echo "Telemetry smoke failed: expected API trace was not exported." >&2
if [ -f "${trace_file}" ]; then
  tail -c 4000 "${trace_file}" >&2 || true
fi
exit 1
