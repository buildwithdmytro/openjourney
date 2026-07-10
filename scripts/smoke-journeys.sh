#!/bin/sh
set -eu

compose_files="${OPENJOURNEY_COMPOSE_FILES:--f compose.yaml}"
api_url="${OPENJOURNEY_API_URL:-http://localhost:8080}"

tenant_id="550e8400-e29b-41d4-a716-446655441000"
workspace_id="550e8400-e29b-41d4-a716-446655441001"
app_id="550e8400-e29b-41d4-a716-446655441002"
iden_id="550e8400-e29b-41d4-a716-446655441003"
template_id="550e8400-e29b-41d4-a716-446655441004"
journey_id="550e8400-e29b-41d4-a716-446655441005"
journey_version_id="550e8400-e29b-41d4-a716-446655441006"

worker_container="openjourney-journeys-worker-smoke"

cleanup() {
  docker rm -f "${worker_container}" >/dev/null 2>&1 || true
  docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -v ON_ERROR_STOP=1 <<EOF >/dev/null 2>&1 || true
DELETE FROM journey_transitions WHERE tenant_id = '${tenant_id}';
DELETE FROM journey_message_intents WHERE tenant_id = '${tenant_id}';
DELETE FROM journey_steps WHERE tenant_id = '${tenant_id}';
DELETE FROM journey_runs WHERE tenant_id = '${tenant_id}';
DELETE FROM journey_versions WHERE tenant_id = '${tenant_id}';
DELETE FROM journeys WHERE tenant_id = '${tenant_id}';
DELETE FROM templates WHERE tenant_id = '${tenant_id}';
DELETE FROM sending_identities WHERE tenant_id = '${tenant_id}';
DELETE FROM consent_ledger WHERE tenant_id = '${tenant_id}';
DELETE FROM identity_aliases WHERE tenant_id = '${tenant_id}';
DELETE FROM identity_merges WHERE tenant_id = '${tenant_id}';
DELETE FROM profiles WHERE tenant_id = '${tenant_id}';
DELETE FROM projection_jobs WHERE tenant_id = '${tenant_id}';
DELETE FROM accepted_events WHERE tenant_id = '${tenant_id}';
DELETE FROM applications WHERE tenant_id = '${tenant_id}';
DELETE FROM workspaces WHERE tenant_id = '${tenant_id}';
DELETE FROM tenants WHERE id = '${tenant_id}';
EOF
}
trap cleanup EXIT

# 1. Stop journeys-worker and worker (projection worker)
docker rm -f "${worker_container}" >/dev/null 2>&1 || true
docker compose ${compose_files} stop journeys-worker worker >/dev/null 2>&1 || true

# 2. Build and start postgres & api & object-store
docker compose ${compose_files} build postgres api worker journeys-worker >/dev/null
docker compose ${compose_files} up -d postgres api >/dev/null

# 3. Wait for postgres to be ready
attempt=0
while [ "${attempt}" -lt 60 ]; do
  if docker compose ${compose_files} exec -T postgres pg_isready -U openjourney >/dev/null 2>&1; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done

# 4. Populate metadata, 2,000 profiles, and consent
docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -v ON_ERROR_STOP=1 <<EOF
DELETE FROM journey_transitions WHERE tenant_id = '${tenant_id}';
DELETE FROM journey_message_intents WHERE tenant_id = '${tenant_id}';
DELETE FROM journey_steps WHERE tenant_id = '${tenant_id}';
DELETE FROM journey_runs WHERE tenant_id = '${tenant_id}';
DELETE FROM journey_versions WHERE tenant_id = '${tenant_id}';
DELETE FROM journeys WHERE tenant_id = '${tenant_id}';
DELETE FROM templates WHERE tenant_id = '${tenant_id}';
DELETE FROM sending_identities WHERE tenant_id = '${tenant_id}';
DELETE FROM consent_ledger WHERE tenant_id = '${tenant_id}';
DELETE FROM identity_aliases WHERE tenant_id = '${tenant_id}';
DELETE FROM identity_merges WHERE tenant_id = '${tenant_id}';
DELETE FROM profiles WHERE tenant_id = '${tenant_id}';
DELETE FROM projection_jobs WHERE tenant_id = '${tenant_id}';
DELETE FROM accepted_events WHERE tenant_id = '${tenant_id}';
DELETE FROM applications WHERE tenant_id = '${tenant_id}';
DELETE FROM workspaces WHERE tenant_id = '${tenant_id}';
DELETE FROM tenants WHERE id = '${tenant_id}';

INSERT INTO tenants (id, name) VALUES ('${tenant_id}', 'Smoke Tenant') ON CONFLICT DO NOTHING;
INSERT INTO workspaces (id, tenant_id, name) VALUES ('${workspace_id}', '${tenant_id}', 'Smoke Workspace') ON CONFLICT DO NOTHING;
INSERT INTO applications (id, tenant_id, workspace_id, name) VALUES ('${app_id}', '${tenant_id}', '${workspace_id}', 'Smoke App') ON CONFLICT DO NOTHING;
INSERT INTO sending_identities (id, tenant_id, workspace_id, channel, provider, from_address, max_send_rate, verified) VALUES ('${iden_id}', '${tenant_id}', '${workspace_id}', 'email', 'ses', 'sender@example.com', 1000, true) ON CONFLICT DO NOTHING;
INSERT INTO templates (id, tenant_id, workspace_id, name, channel, html_template, sending_identity_id, version) VALUES ('${template_id}', '${tenant_id}', '${workspace_id}', 'Smoke Template', 'email', 'Hello {{ name }}', '${iden_id}', 1) ON CONFLICT DO NOTHING;

INSERT INTO profiles (id, tenant_id, workspace_id, app_id, external_id, attributes)
SELECT ('550e8400-e29b-41d4-a716-' || lpad(to_hex(i), 12, '0'))::uuid, '${tenant_id}', '${workspace_id}', '${app_id}', 'cust-' || i, jsonb_build_object('email', 'cust-' || i || '@example.com', 'country', 'US')
FROM generate_series(1, 500) i;

INSERT INTO consent_ledger (id, tenant_id, workspace_id, app_id, profile_id, channel, topic, state, occurred_at)
SELECT gen_random_uuid(), '${tenant_id}', '${workspace_id}', '${app_id}', p.id, 'email', 'marketing', 'subscribed', now()
FROM profiles p
WHERE p.tenant_id = '${tenant_id}';
EOF

# 5. Insert journey and version definition
docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -v ON_ERROR_STOP=1 <<EOF
INSERT INTO journeys (id, tenant_id, workspace_id, name, description, status, graph, latest_version, current_version_id)
VALUES (
  '${journey_id}',
  '${tenant_id}',
  '${workspace_id}',
  'Smoke Journey',
  'Smoke Test Journey',
  'published',
  '{
    "entry_node_id": "n1",
    "nodes": [
      { "id": "n1", "type": "entry", "config": { "trigger": "event", "event_type": "user.signup" } },
      { "id": "n2", "type": "split", "config": { "mode": "random", "branches": [ { "label": "a", "weight": 50 }, { "label": "b", "weight": 50 } ] } },
      { "id": "n3", "type": "message", "config": { "template_id": "${template_id}", "transactional": true } },
      { "id": "n4", "type": "message", "config": { "template_id": "${template_id}", "transactional": true } },
      { "id": "n5", "type": "exit", "config": { "reason": "completed_a" } },
      { "id": "n6", "type": "exit", "config": { "reason": "completed_b" } }
    ],
    "edges": [
      { "from": "n1", "to": "n2" },
      { "from": "n2", "to": "n3", "branch": "a" },
      { "from": "n2", "to": "n4", "branch": "b" },
      { "from": "n3", "to": "n5" },
      { "from": "n4", "to": "n6" }
    ]
  }'::jsonb,
  1,
  '${journey_version_id}'
);

INSERT INTO journey_versions (id, journey_id, tenant_id, workspace_id, version, graph, entry_kind, entry_event_type, reentry_policy, status)
VALUES (
  '${journey_version_id}',
  '${journey_id}',
  '${tenant_id}',
  '${workspace_id}',
  1,
  '{
    "entry_node_id": "n1",
    "nodes": [
      { "id": "n1", "type": "entry", "config": { "trigger": "event", "event_type": "user.signup" } },
      { "id": "n2", "type": "split", "config": { "mode": "random", "branches": [ { "label": "a", "weight": 50 }, { "label": "b", "weight": 50 } ] } },
      { "id": "n3", "type": "message", "config": { "template_id": "${template_id}", "transactional": true } },
      { "id": "n4", "type": "message", "config": { "template_id": "${template_id}", "transactional": true } },
      { "id": "n5", "type": "exit", "config": { "reason": "completed_a" } },
      { "id": "n6", "type": "exit", "config": { "reason": "completed_b" } }
    ],
    "edges": [
      { "from": "n1", "to": "n2" },
      { "from": "n2", "to": "n3", "branch": "a" },
      { "from": "n2", "to": "n4", "branch": "b" },
      { "from": "n3", "to": "n5" },
      { "from": "n4", "to": "n6" }
    ]
  }'::jsonb,
  'event',
  'user.signup',
  'once',
  'active'
);
EOF

# 6. Insert 500 accepted events and their projection jobs
echo "Ingesting 500 entry events..."
docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -v ON_ERROR_STOP=1 <<EOF
WITH inserted_events AS (
  INSERT INTO accepted_events (id, tenant_id, workspace_id, app_id, event_type, schema_version, external_id, payload, occurred_at, idempotency_key)
  SELECT gen_random_uuid(), '${tenant_id}', '${workspace_id}', '${app_id}', 'user.signup', 1, p.external_id, '{}'::jsonb, now(), p.external_id || '-signup'
  FROM profiles p
  WHERE p.tenant_id = '${tenant_id}'
  RETURNING id, tenant_id, external_id
)
INSERT INTO projection_jobs (event_id, tenant_id, partition_key)
SELECT id, tenant_id, external_id
FROM inserted_events;
EOF

# 7. Start projection worker to process enrollment in parallel
echo "Starting 10 projection workers..."
docker compose ${compose_files} up -d --scale worker=10 worker

echo "Waiting for enrollment to complete..."
attempt=0
while [ "${attempt}" -lt 60 ]; do
  done_jobs="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
    "SELECT count(*) FROM projection_jobs WHERE tenant_id='${tenant_id}' AND status='done'")"
  echo "Wait loop - attempt=${attempt}: done_jobs=${done_jobs}"
  if [ "${done_jobs}" -eq 500 ]; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done

# Stop projection workers as enrollment is complete
docker compose ${compose_files} stop worker >/dev/null 2>&1 || true

# Verify enrollment count
runs_count="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
  "SELECT count(*) FROM journey_runs WHERE tenant_id='${tenant_id}'")"
if [ "${runs_count}" -ne 500 ]; then
  echo "FAILURE: Expected 500 runs enrolled, got ${runs_count}"
  exit 1
fi
echo "All 500 runs successfully enrolled!"

# 8. Start journeys-worker and run mid-flight
echo "Starting journeys-worker..."
docker compose ${compose_files} run -d --name "${worker_container}" --no-deps -e OPENJOURNEY_MOCK_SES=true journeys-worker -watch -max-duration=1h >/dev/null
sleep 2

# 9. Kill the worker mid-flight
echo "Killing journeys-worker mid-flight..."
docker kill "${worker_container}" >/dev/null
docker rm "${worker_container}" >/dev/null

# 10. Unlock steps and message intents that were being processed
echo "Resetting locks on processing steps and intents..."
docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -c \
  "UPDATE journey_steps SET locked_until=now()-interval '1 second' WHERE status='processing'" >/dev/null
docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -c \
  "UPDATE journey_message_intents SET locked_until=now()-interval '1 second' WHERE status='processing'" >/dev/null

# 11. Restart worker to complete processing
echo "Restarting journeys-worker to complete execution..."
docker compose ${compose_files} run -d --name "${worker_container}" --no-deps -e OPENJOURNEY_MOCK_SES=true journeys-worker -watch -max-duration=1h >/dev/null

# 12. Wait for everything to drain and terminate
echo "Waiting for all steps and messages to process..."
attempt=0
while [ "${attempt}" -lt 60 ]; do
  active_runs="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
    "SELECT count(*) FROM journey_runs WHERE tenant_id='${tenant_id}' AND status IN ('active', 'waiting')")"
  pending_steps="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
    "SELECT count(*) FROM journey_steps WHERE tenant_id='${tenant_id}' AND status IN ('pending', 'processing')")"
  pending_intents="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
    "SELECT count(*) FROM journey_message_intents WHERE tenant_id='${tenant_id}' AND status IN ('pending', 'processing')")"
  
  echo "Current State: active_runs=${active_runs}, pending_steps=${pending_steps}, pending_intents=${pending_intents}"
  if [ "${active_runs}" -eq 0 ] && [ "${pending_steps}" -eq 0 ] && [ "${pending_intents}" -eq 0 ]; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 2
done

# Ensure all 500 runs reached status = 'completed'
completed_runs="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
  "SELECT count(*) FROM journey_runs WHERE tenant_id='${tenant_id}' AND status='completed'")"
if [ "${completed_runs}" -ne 500 ]; then
  echo "FAILURE: Expected 500 completed runs, got ${completed_runs}"
  exit 1
fi
echo "All 500 runs successfully completed!"

# Assert no duplicate intents (effectively-once execution)
duplicates="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
  "SELECT count(*) FROM (
     SELECT run_id, node_id FROM journey_message_intents WHERE tenant_id='${tenant_id}' GROUP BY run_id, node_id HAVING count(*) > 1
   ) dup")"
if [ "${duplicates}" -ne 0 ]; then
  echo "FAILURE: Found duplicate message intents!"
  exit 1
fi

# Assert exactly 500 successful sends
total_sent="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
  "SELECT count(*) FROM journey_message_intents WHERE tenant_id='${tenant_id}' AND status='completed' AND decision='sent'")"
if [ "${total_sent}" -ne 500 ]; then
  echo "FAILURE: Expected 500 sent messages, got ${total_sent}!"
  exit 1
fi
echo "Effectively-once delivery asserted: exactly 500 sends, 0 duplicates."

# Assert exact total transitions (4 per run)
total_transitions="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
  "SELECT count(*) FROM journey_transitions WHERE tenant_id='${tenant_id}'")"
if [ "${total_transitions}" -ne 2000 ]; then
  echo "FAILURE: Expected exactly 2,000 transitions, got ${total_transitions}"
  exit 1
fi
echo "All transitions fully traced!"

# Capture branch counts to assert determinism
branch_a="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
  "SELECT count(*) FROM journey_transitions WHERE tenant_id='${tenant_id}' AND from_node='n2' AND to_node='n3'")"
branch_b="$(docker compose ${compose_files} exec -T postgres psql -U openjourney -d openjourney -Atc \
  "SELECT count(*) FROM journey_transitions WHERE tenant_id='${tenant_id}' AND from_node='n2' AND to_node='n4'")"

echo "Branch A assigned count: ${branch_a}"
echo "Branch B assigned count: ${branch_b}"

# Assert branch counts sum to 500
sum_branches=$((branch_a + branch_b))
if [ "${sum_branches}" -ne 500 ]; then
  echo "FAILURE: Branch counts do not sum to 500"
  exit 1
fi

# Hardcode/Assert determinism of the hashes across runs
# Note: For the first run, let's print them out and dynamically match them so that subsequent runs of this test on identical seed profiles assert the exact same counts.
expected_a=238
expected_b=262

if [ -n "${expected_a}" ]; then
  if [ "${branch_a}" -ne "${expected_a}" ] || [ "${branch_b}" -ne "${expected_b}" ]; then
    echo "FAILURE: Deterministic split counts changed! Expected A=${expected_a}, B=${expected_b}, got A=${branch_a}, B=${branch_b}"
    exit 1
  fi
fi
echo "SUCCESS: Split counts are deterministic!"

echo "SUCCESS: Worker-kill load smoke test passed!"
