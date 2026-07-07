# OpenJourney

OpenJourney is an Apache-2.0 customer engagement platform combining real-time customer data and journey orchestration with self-hosted marketing automation.

This repository contains the v1 Phase 1 platform kernel:

- Tenant/workspace/application isolation and scoped development API keys.
- Immutable, idempotent event ingestion.
- Bounded, leased profile and consent projection jobs.
- Profile lookup API and a React control-plane shell.
- PostgreSQL/object-storage reduced deployment mode.
- Optional Kafka-compatible event stream, ClickHouse analytics, and immutable archive profile.
- Built-in self-hosted operator login, optional OIDC, RBAC, schema registry, API-key lifecycle, privacy operations, replay verification, audit, and OpenTelemetry.
- Browser SDK with sessions, identity, profile, consent, batching, persistence, and retry.
- OpenAPI contract, integration tests, containers, SBOM/provenance release workflow, and CI.

See [plan.md](plan.md) for the architecture and roadmap and [product-decisions.md](product-decisions.md) for accepted constraints.

## Run locally

Requirements: Docker with Compose.

```bash
cp .env.example .env
docker compose up --build
```

The UI is available at <http://localhost:3000> and the API at <http://localhost:8080>. Compose bootstraps a local admin account from `OPENJOURNEY_ADMIN_EMAIL` and `OPENJOURNEY_ADMIN_PASSWORD`; the default is `admin@example.test` / `local-development-password` for local development only. The default Compose API key is `local-development-key`; replace both credentials through `.env` outside local development.

To run the production data profile:

```bash
docker compose -f compose.yaml -f compose.production-data.yaml up --build
```

Run the end-to-end check after the services start:

```bash
./scripts/smoke.sh
```

The smoke test accepts a profile update and consent event, waits for the bounded worker, and reads the resulting profile projection.

## API examples

Accept immutable events:

```bash
curl -H 'Authorization: Bearer local-development-key' \
  -H 'Content-Type: application/json' \
  -d '{"events":[{
    "event_type":"profile.updated",
    "schema_version":1,
    "external_id":"customer-123",
    "idempotency_key":"profile-update-1",
    "occurred_at":"2026-07-06T12:00:00Z",
    "payload":{"attributes":{"email":"ada@example.com","first_name":"Ada"}}
  }]}' \
  http://localhost:8080/v1/events/batch
```

Read the asynchronously projected profile:

```bash
curl -H 'Authorization: Bearer local-development-key' \
  http://localhost:8080/v1/profiles/customer-123
```

The public contract is in [api/openapi.yaml](api/openapi.yaml).

## Worker operation

The same worker binary supports continuous and bounded invocation:

```bash
openjourney-worker -watch -max-duration=1h -max-items=1000000
openjourney-worker -max-duration=30s -max-items=1000
```

Jobs are leased in PostgreSQL. A terminated worker does not lose accepted events; an expired lease makes work reclaimable.

## Development checks

```bash
make test
docker compose build
```

Go checks run in a container when Go is not installed on the host. UI checks use Node 22.

## Security status

Phase 1 supplies GDPR/HIPAA-oriented technical controls but does not claim certification or a hosted BAA. See the [threat model](docs/security/threat-model.md), [control mapping](docs/security/compliance-controls.md), and [operations runbook](docs/operations/runbook.md).
