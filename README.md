# OpenJourney

<p align="center">
  <strong>Open-source customer engagement infrastructure you can run on your own stack.</strong>
</p>

<p align="center">
  Real-time customer data · Visual journey orchestration · Campaign delivery · Experiments · Product analytics
</p>

<p align="center">
  <a href="https://github.com/buildwithdmytro/openjourney/actions"><img alt="CI" src="https://github.com/buildwithdmytro/openjourney/actions/workflows/ci.yml/badge.svg"></a>
  <a href="LICENSE"><img alt="Apache 2.0 license" src="https://img.shields.io/badge/license-Apache--2.0-blue.svg"></a>
  <a href="https://go.dev/"><img alt="Go" src="https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white"></a>
  <a href="web/package.json"><img alt="React" src="https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black"></a>
  <a href="compose.yaml"><img alt="Docker Compose" src="https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white"></a>
</p>

OpenJourney is a self-hosted customer engagement platform for teams that want the building
blocks of a CDP, marketing automation suite, and experimentation system without handing their
customer data to another SaaS vendor.

Ingest immutable behavioral events, build audiences, launch campaigns, orchestrate durable
multi-step journeys, run deterministic A/B tests, and inspect conversion funnels from one
Apache-2.0 codebase.

> **Project status:** OpenJourney is under active development. The platform kernel, campaigns,
> journeys, experimentation, and PostgreSQL-backed analytics milestones are implemented. Review
> the [roadmap](plan.md) before evaluating it for production use.

## Why OpenJourney?

- **Own the data plane.** Run locally or in your infrastructure with PostgreSQL and S3-compatible
  object storage; Kafka-compatible streaming and ClickHouse are optional.
- **Build reliable journeys.** Durable leases, idempotent projections, immutable versions,
  deterministic splits, replay verification, and dead-letter recovery are core primitives.
- **Experiment without unstable bucketing.** A/B and multivariate assignments are deterministic,
  persisted, and support controls and holdouts.
- **Measure without scanning hot event tables.** Engagement and conversion facts are projected
  into indexed PostgreSQL tables for funnel, deliverability, uplift, and attribution reports.
- **Keep humans in control.** Publishing, backfills, and experiment rollout use explicit
  human-actor approval gates.
- **Start observable and secure.** Scoped API keys, RBAC, optional OIDC, audit trails, privacy
  operations, OpenTelemetry metrics, an OpenAPI contract, and signed release provenance ship
  with the platform.

## What is included

| Area | Capabilities |
|---|---|
| Customer data | Idempotent event ingestion, profiles, identity, consent, schema registry, browser SDK |
| Audiences | Profile, consent, and behavioral filters; suppression and fatigue policy checks |
| Campaigns | Immutable audience manifests, email/webhook delivery, retries, callbacks, tracking |
| Journeys | Visual DAG builder, schedules and event triggers, waits, conditions, splits, messages, goals |
| Experiments | A/B and multivariate variants, controls, holdouts, stable assignment, approved rollout |
| Analytics | Campaign and journey funnels, deliverability, conversion attribution, uplift and significance |
| Operations | DLQs, replay, privacy workflows, audit logs, metrics, smoke tests, containerized services |

## Architecture at a glance

```text
Browser SDK / API clients
          |
          v
  Event ingestion API -----> PostgreSQL (source of truth)
          |                         |
          v                         +--> projections, facts, reports
  Kafka-compatible stream          +--> leased campaign/journey workers
          |
          +--> ClickHouse (optional analytical scale path)

  S3-compatible storage <---- immutable manifests, graphs, and archives
          |
          v
  React control plane <---- scoped Go API ----> OpenTelemetry
```

The reduced deployment profile requires only PostgreSQL and S3-compatible storage. See
[product decisions](product-decisions.md) for the locked architectural constraints and
[the milestone plans](docs/milestones/) for implementation evidence.

## Quick start

### Requirements

- Docker with Compose
- Enough local capacity to build the Go services and React control plane

```bash
git clone https://github.com/buildwithdmytro/openjourney.git
cd openjourney
cp .env.example .env
docker compose up --build
```

Open:

- Control plane: <http://localhost:3000>
- API: <http://localhost:8080>

For local development, Compose defaults to:

```text
Email:    admin@example.test
Password: local-development-password
API key:  local-development-key
```

Change these credentials in `.env` anywhere beyond an isolated local environment.

Run the end-to-end health check after services start:

```bash
./scripts/smoke.sh
```

To include the production data profile:

```bash
docker compose -f compose.yaml -f compose.production-data.yaml up --build
```

## Try the API

Accept an immutable profile event:

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

Explore the complete public contract in [api/openapi.yaml](api/openapi.yaml).

## Repository guide

| Path | Purpose |
|---|---|
| [`cmd/`](cmd/) | API, projection, campaign, journey, analytics, and operations binaries |
| [`internal/`](internal/) | Domain logic, stores, workers, policy, rendering, telemetry, and adapters |
| [`web/`](web/) | React control plane for data, campaigns, journeys, experiments, and reports |
| [`sdk/javascript/`](sdk/javascript/) | Browser event, identity, consent, session, batching, and retry SDK |
| [`api/openapi.yaml`](api/openapi.yaml) | Public HTTP API contract |
| [`docs/`](docs/) | Architecture decisions, milestone evidence, security, and operations guidance |
| [`scripts/`](scripts/) | End-to-end, recovery, and smoke-test automation |

## Development

Run the standard checks:

```bash
make test
docker compose build
```

Go checks run in a container when Go is unavailable on the host. Frontend checks use Node 22.
Individual workers support bounded execution so they are easy to operate and test:

```bash
openjourney-worker -watch -max-duration=1h -max-items=1000000
openjourney-worker -max-duration=30s -max-items=1000
```

Jobs are leased in PostgreSQL. If a worker terminates, accepted events remain durable and an
expired lease makes unfinished work reclaimable.

## Documentation

- [Architecture and roadmap](plan.md)
- [Product decisions](product-decisions.md)
- [Operations runbook](docs/operations/runbook.md)
- [Threat model](docs/security/threat-model.md)
- [Compliance control mapping](docs/security/compliance-controls.md)
- [Milestone plans and completion audits](docs/milestones/)

## Security and compliance

OpenJourney supplies GDPR/HIPAA-oriented technical controls but does not claim certification or
a hosted BAA. Please report vulnerabilities privately through GitHub's security reporting flow
rather than opening a public issue.

## Contributing

Issues, architecture discussions, documentation fixes, and focused pull requests are welcome.
Please read the roadmap and product decisions first so proposed changes preserve the project's
data-ownership, determinism, isolation, and human-approval guarantees.

## License

OpenJourney is available under the [Apache License 2.0](LICENSE).
