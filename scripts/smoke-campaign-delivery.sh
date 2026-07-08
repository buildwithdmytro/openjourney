#!/bin/sh
set -eu

compose_files="${OPENJOURNEY_COMPOSE_FILES:--f compose.yaml}"
api_url="${OPENJOURNEY_API_URL:-http://localhost:8080}"

tenant_id="550e8400-e29b-41d4-a716-446655440000"
workspace_id="550e8400-e29b-41d4-a716-446655440001"
app_id="550e8400-e29b-41d4-a716-446655440002"
segment_id="550e8400-e29b-41d4-a716-446655440003"
iden_id="550e8400-e29b-41d4-a716-446655440004"
template_id="550e8400-e29b-41d4-a716-446655440005"
campaign_id="550e8400-e29b-41d4-a716-446655440006"
worker_container="openjourney-campaigns-delivery-smoke"

cleanup() {
  docker rm -f "${worker_container}" >/dev/null 2>&1 || true
  docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -c "DELETE FROM tenants WHERE id='${tenant_id}'" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# 1. Stop campaigns-delivery worker
docker rm -f "${worker_container}" >/dev/null 2>&1 || true
docker compose ${compose_files} stop campaigns-delivery >/dev/null 2>&1 || true

# 2. Build and start postgres & object-store & campaigns-dispatcher
docker compose ${compose_files} build postgres object-store api campaigns-dispatcher campaigns-delivery >/dev/null
docker compose ${compose_files} up -d postgres object-store api campaigns-dispatcher >/dev/null

# 3. Wait for postgres to be ready
attempt=0
while [ "${attempt}" -lt 60 ]; do
  if docker compose ${compose_files} exec -T postgres pg_isready -U openjourney >/dev/null 2>&1; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done

# 4. Populate test data: tenant, workspace, dynamic segment, template, sending identity
docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -v ON_ERROR_STOP=1 <<EOF
INSERT INTO tenants (id, name) VALUES ('${tenant_id}', 'Smoke Tenant') ON CONFLICT DO NOTHING;
INSERT INTO workspaces (id, tenant_id, name) VALUES ('${workspace_id}', '${tenant_id}', 'Smoke Workspace') ON CONFLICT DO NOTHING;
INSERT INTO applications (id, tenant_id, workspace_id, name) VALUES ('${app_id}', '${tenant_id}', '${workspace_id}', 'Smoke App') ON CONFLICT DO NOTHING;
INSERT INTO segments (id, tenant_id, workspace_id, name, type, dsl, version) VALUES ('${segment_id}', '${tenant_id}', '${workspace_id}', 'Smoke Segment', 'dynamic', '{"type":"profile_attribute","field":"country","operator":"equals","value":"US"}', 1) ON CONFLICT DO NOTHING;
INSERT INTO sending_identities (id, tenant_id, workspace_id, channel, provider, from_address, max_send_rate, verified) VALUES ('${iden_id}', '${tenant_id}', '${workspace_id}', 'email', 'ses', 'sender@example.com', 1000, true) ON CONFLICT DO NOTHING;
INSERT INTO templates (id, tenant_id, workspace_id, name, channel, html_template, sending_identity_id, version) VALUES ('${template_id}', '${tenant_id}', '${workspace_id}', 'Smoke Template', 'email', 'Hello {{ name }}', '${iden_id}', 1) ON CONFLICT DO NOTHING;

-- Insert 10,000 profiles with attributes (country=US)
INSERT INTO profiles (id, tenant_id, app_id, external_id, attributes)
SELECT gen_random_uuid(), '${tenant_id}', '${app_id}', 'cust-' || i, jsonb_build_object('email', 'cust-' || i || '@example.com', 'country', 'US')
FROM generate_series(1, 10000) i;

-- Insert consent for all 10,000 profiles
INSERT INTO consent_ledger (id, tenant_id, app_id, profile_id, channel, topic, state, occurred_at)
SELECT gen_random_uuid(), '${tenant_id}', '${app_id}', p.id, 'email', 'marketing', 'subscribed', now()
FROM profiles p
WHERE p.tenant_id = '${tenant_id}';
EOF

# 5. Insert scheduled campaign
docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -v ON_ERROR_STOP=1 <<EOF
INSERT INTO campaigns (id, tenant_id, workspace_id, name, segment_id, template_id, status, segment_version, template_version, scheduled_at)
VALUES ('${campaign_id}', '${tenant_id}', '${workspace_id}', 'Smoke Campaign', '${segment_id}', '${template_id}', 'scheduled', 1, 1, now() - interval '1 minute');
EOF

echo "Waiting for dispatcher to dispatch campaign..."
# 6. Wait for dispatcher to dispatch (status becomes 'sending')
attempt=0
status=""
while [ "${attempt}" -lt 60 ]; do
  status="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
    "SELECT status FROM campaigns WHERE id='${campaign_id}'")"
  if [ "${status}" = "sending" ]; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
if [ "${status}" != "sending" ]; then
  echo "Campaign dispatch failed or timed out: status is ${status}"
  exit 1
fi
echo "Campaign successfully dispatched!"

# 7. Start campaigns-delivery and let it run for a bit, then kill it
echo "Starting delivery worker..."
docker rm -f "${worker_container}" >/dev/null 2>&1 || true
docker compose ${compose_files} run -d --name "${worker_container}" --no-deps -e OPENJOURNEY_MOCK_SES=true campaigns-delivery -watch -max-duration=1h >/dev/null
sleep 2

echo "Killing delivery worker mid-shard..."
docker kill "${worker_container}" >/dev/null
docker rm "${worker_container}" >/dev/null

# 8. Set locked jobs' lock TTL to past so they can be reclaimed
docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -c \
  "UPDATE delivery_jobs SET locked_until=now()-interval '1 second' WHERE status='processing'" >/dev/null

# 9. Start campaigns-delivery again and let it run to completion
echo "Restarting delivery worker to complete delivery..."
docker rm -f "${worker_container}" >/dev/null 2>&1 || true
docker compose ${compose_files} run -d --name "${worker_container}" --no-deps -e OPENJOURNEY_MOCK_SES=true campaigns-delivery -watch -max-duration=1h >/dev/null

# 10. Wait for the campaign to reach 'completed' status
echo "Waiting for campaign to complete..."
attempt=0
while [ "${attempt}" -lt 60 ]; do
  status="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
    "SELECT status FROM campaigns WHERE id='${campaign_id}'")"
  if [ "${status}" = "completed" ] || [ "${status}" = "failed" ]; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 2
done

if [ "${status}" != "completed" ]; then
  echo "Campaign failed or timed out: status is ${status}"
  exit 1
fi
echo "Campaign successfully completed!"

# 11. Assert NO duplicate attempts for any profile
duplicates="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
  "SELECT count(*) FROM (
     SELECT profile_id FROM delivery_attempts WHERE campaign_id='${campaign_id}' GROUP BY profile_id HAVING count(*) > 1
   ) dup")"

if [ "${duplicates}" -ne 0 ]; then
  echo "FAILURE: Found ${duplicates} profiles with duplicate delivery attempts!"
  exit 1
fi

# 12. Assert exactly 10,000 sent rows
total_sent="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
  "SELECT count(*) FROM delivery_attempts WHERE campaign_id='${campaign_id}' AND decision='sent'")"

if [ "${total_sent}" -ne 10000 ]; then
  echo "FAILURE: Expected 10,000 sent attempts, got ${total_sent}!"
  exit 1
fi

echo "SUCCESS: Load / effectively-once delivery smoke test passed!"
