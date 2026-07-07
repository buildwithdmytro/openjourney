# Phase 1 operations runbook

## Deployment profiles

Reduced profile:

```bash
docker compose up -d --build
```

Production data profile:

```bash
docker compose -f compose.yaml -f compose.production-data.yaml up -d --build
```

The production profile adds Redpanda, ClickHouse, transactional-outbox dispatch, and independent archive/analytics consumers.

## Health and telemetry

- Liveness: `GET /health/live`
- Readiness: `GET /health/ready`
- Prometheus metrics: `GET /metrics`
- OTLP traces: configure `OPENJOURNEY_OTLP_ENDPOINT`
- Queue health: `GET /v1/operations/queues`
- Replay drift: `POST /v1/operations/replay/verify`

Alert on oldest pending job age, dead jobs, projection lag, outbox lag, consumer lag, API error ratio, and database saturation.

Built-in local operator login is enabled by `OPENJOURNEY_ADMIN_EMAIL` and `OPENJOURNEY_ADMIN_PASSWORD`; configure them through secret files or a secret manager outside local development. OIDC provider validation is optional and documented in [oidc-provider-validation.md](oidc-provider-validation.md) for deployments that require external SSO.

Run local telemetry validation:

```bash
./scripts/smoke-telemetry.sh
```

The smoke starts a temporary OpenTelemetry Collector on the Compose network, recreates the API with `OPENJOURNEY_OTLP_ENDPOINT`, scrapes `/metrics`, submits an authenticated event batch, and verifies that an API trace is exported.

Run a bounded ingestion load smoke:

```bash
./scripts/load-smoke.sh
```

Defaults are intentionally below the per-tenant quota: 20 batches/s, 10 events/batch, 30 seconds, with `p99 < 500 ms` and failure rate below 0.1%. Override `OPENJOURNEY_LOAD_RATE`, `OPENJOURNEY_LOAD_BATCH_SIZE`, `OPENJOURNEY_LOAD_DURATION`, and `OPENJOURNEY_LOAD_P99_MS` for environment-specific capacity checks.

## Bounded execution

```bash
openjourney-worker -max-items=1000 -max-duration=30s
openjourney-dispatcher -max-items=10000 -max-duration=2m
openjourney-operations -max-items=100 -max-duration=5m
```

Use `-watch` for continuously warm consumers. Expired leases are reclaimable and all handlers are idempotent at their persistence boundary.

## Backup and restore

```bash
OPENJOURNEY_DATABASE_URL='postgres://...' ./scripts/backup.sh backup.sql.gz
OPENJOURNEY_CONFIRM_RESTORE=yes OPENJOURNEY_DATABASE_URL='postgres://...' \
  ./scripts/restore.sh backup.sql.gz
```

Set `OPENJOURNEY_POSTGRES_CLIENT_IMAGE=postgres:17-alpine` when the host PostgreSQL client does not match the server major version. The scripts stage uncompressed data and reject empty dumps, so a failed producer cannot masquerade as a valid gzip backup.

Run a reduced-mode restore drill:

```bash
OPENJOURNEY_POSTGRES_CLIENT_IMAGE=postgres:17-alpine \
  ./scripts/rehearse-backup-restore.sh
```

The rehearsal creates a temporary PostgreSQL database, restores the latest dump into it, compares core table counts, runs a read query, verifies gzip integrity, and then drops the temporary database. Use `OPENJOURNEY_REHEARSAL_ADMIN_DATABASE_URL` when the administrative database differs from the default local Compose database.

Object storage and ClickHouse require provider-native snapshots/lifecycle configuration. PostgreSQL and object storage are authoritative; ClickHouse is rebuildable from the event stream/archive.

## Incident controls

- Revoke a compromised API key with `DELETE /v1/api-keys/{id}`.
- Stop a queue consumer without losing accepted work; restart after dependency recovery.
- Inspect queue dead counts and database `last_error` before replay.
- Verify profiles with the replay endpoint after projection incidents.
- Submit privacy deletion through the API rather than deleting individual tables.
