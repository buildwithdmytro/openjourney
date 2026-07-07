# V1 milestone 1 completion audit

Authoritative scope: Phase 1 in [`plan.md`](../../plan.md).

| Requirement | Evidence required | Current status |
|---|---|---|
| Tenant/workspace/application model | Migrations, tenant-scoped repositories, isolation tests | Complete for kernel; live PostgreSQL integration covers cross-tenant isolation |
| Built-in auth, optional OIDC, RBAC, service accounts, API keys | Local operator sessions, token verification, role bindings, scoped keys, authorization tests and UI | Complete for kernel; local self-hosted admin bootstrap/session login/session revocation, optional OIDC verifier, live HTTP/store integration tests, scoped key expiry/revocation/last-use metadata, and admin UI exist; real-provider OIDC sign-in validation is optional deployment evidence |
| Canonical event envelope | Contract and persistence of identity, timing, source, correlation, classification | Complete for kernel event fields |
| Schema registry | CRUD, compatibility and ingestion validation tests | Complete; backend validation and control-plane schema administration exist |
| Batch ingestion, quotas, dedupe | API plus integration tests for limits and idempotency conflicts | Complete for backend |
| Identity graph | Alias/identify/merge commands, audit, deterministic tests | Complete for kernel replay/projection semantics; anonymous-to-known, explicit merge, alias non-forking, target precedence, latest consent, and equivalent-order checksum tests exist |
| Profile projection | Typed projection and replay checksum | Complete for kernel event types |
| Consent ledger | Immutable facts and current-state tests | Complete for kernel, including retention-safe source references |
| PostgreSQL | Migrations, backup/restore rehearsal | Complete for reduced mode; migrations and automated restore rehearsal are covered |
| ClickHouse | Production profile and analytics sink integration test | Complete for production-data smoke; event reaches ClickHouse behavior_events |
| Object storage | Immutable raw archive and integration test | Complete for production-data smoke; event archive object is verified in S3-compatible storage |
| Event stream | Transactional outbox, Kafka publication, idempotent consumer test | Complete for production-data smoke; outbox publishes through Redpanda and consumers commit |
| Audit | Auth, configuration, privacy, identity, and ingestion audit coverage | Complete for kernel mutations; audit log UI exists and live PostgreSQL integration asserts schema, API-key, role, user, ingestion, privacy export, privacy delete audit entries, and OIDC-user attribution for ingestion |
| OpenTelemetry | Trace/metric exporter wiring and smoke evidence | Complete for API; telemetry smoke scrapes metrics and verifies exported OTLP traces |
| Operations API | Queue, replay, DLQ, audit, health and privacy operations | Complete for kernel; queue/replay/audit/health/privacy plus DLQ list/retry/discard APIs exist, and OpenAPI contract tests guard critical runtime error responses |
| Local Docker Compose | Reduced and production profiles | Complete; reduced services and production-data profile build/start, production-data smoke passes |
| CI/release pipeline | Unit, integration, schema, security, container and artifact checks | Complete for milestone gates; CI includes serialized Go race/vet/gofmt/govulncheck with live PostgreSQL integration tests, web/SDK typecheck/test/build/audit, OpenAPI lint, script checks, container build, reduced Compose smoke, worker termination smoke, telemetry smoke, production-data smoke, load smoke, and restore rehearsal; release publishes app/web images with SBOM and provenance |
| Load smoke | Bounded ingestion load test with latency/failure thresholds | Complete for milestone smoke; CI runs `scripts/load-smoke.sh`, whose defaults validate 600 batches / 6,000 events with p99 < 500 ms |
| JavaScript SDK | Sessions, identify, attributes, events, consent, batching, offline retry | Complete for browser kernel; SDK covers sessions, events, identify, attributes, consent, alias, merge, reset, durable retry queue, batching, and auto-flush with typecheck/test/build gates |
| Replay equivalence | Clean rebuild checksum equals live projection | Complete for kernel projection model |
| Cross-tenant isolation | Database/API integration suite | Complete for kernel; live PostgreSQL repository tests cover data isolation, and live HTTP API tests cover credential-derived tenant context for ingestion idempotency, OIDC/RBAC, and schema visibility |
| Worker failure safety | Lease expiry and process-termination test | Complete for projection worker; integration test covers lease reclaim and smoke kills a worker after claim then verifies restart recovery |
| Bounded-job deployment | Documented and smoke-tested drain invocation | Complete |
| Retention enforcement | Expired raw event deletion without losing current projections | Complete for PostgreSQL operation job path |

The milestone is complete only when every row is supported by direct current-state evidence.

Import/migration tooling is intentionally deferred from this Milestone 1 audit per current product direction.
