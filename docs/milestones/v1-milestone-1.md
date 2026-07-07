# V1 milestone 1: platform kernel

Status: in progress  
Initial vertical slice: 2026-07-06

## Delivered

- Go API and bounded worker binaries.
- PostgreSQL migrations for tenants, workspaces, applications, hashed/scoped API keys, immutable events, leased projection jobs, profiles, consent ledger, and audit events.
- Authenticated batch ingestion with per-application idempotency and strict payload validation.
- Asynchronous profile and consent projections with reclaimable leases, retries, and dead-letter state.
- Schema registry, built-in local operator auth, optional OIDC-backed users, role bindings, scoped API-key lifecycle, queue status, replay verification, audit listing, and privacy request APIs.
- Dead-letter queue listing, tenant-scoped retry, and tenant-scoped discard operations.
- DSAR export/delete operations and bounded retention enforcement jobs.
- Tenant-scoped profile lookup API.
- OpenAPI 3.1 contract and structured error responses.
- React control-plane shell with API readiness, profile/consent inspection, schemas, API keys, privacy requests, access administration, operations/DLQ, replay, and audit views.
- Cloud-neutral Docker Compose profile using PostgreSQL.
- Unit/API tests, end-to-end smoke test, container builds, and CI.
- Apache-2.0 project declaration and explicit Mautic GPL isolation.

## Acceptance evidence

- Go unit tests and static analysis pass using Go 1.24.
- React type checking, unit tests, and production build pass using Node 22.
- React auth tests verify local login sessions and restored OIDC ID tokens remain session-only and are not written to `localStorage`, while manually entered local API keys can persist for development.
- API-key lifecycle implementation exposes `expires_at`, `revoked_at`, `last_used_at`, and one-time secrets through the API contract and control plane, with expiry creation and lifecycle timestamp display.
- API, worker, web, and PostgreSQL container images build.
- The Compose smoke test accepts profile and consent events, lets the bounded worker project them, and reads the resulting profile.
- Repeating the same idempotency key resolves to the same accepted event and does not create another projection job.
- Worker state is persisted; abandoned processing leases can be reclaimed after expiry, including a smoke test that kills a worker after claim and verifies recovery.
- Live PostgreSQL integration test covers tenant isolation, scoped local-session and OIDC/RBAC authorization, schema compatibility validation, idempotency conflicts, identity projection, replay equivalence, lease recovery, ordered projection, DSAR export/delete, and retention deletion.
- Live PostgreSQL integration test covers API-key `last_used_at` updates and OIDC user attribution for ingestion audit records.
- Live HTTP API integration test covers credential-derived tenant context for event idempotency, OIDC/RBAC authorization, and tenant-scoped schema visibility.
- OpenAPI contract test covers critical runtime error responses for ingestion, profile lookup, schema operations, and replay verification, including authorization, idempotency conflict, quota, validation, and internal-error outcomes.
- Live PostgreSQL integration test covers DLQ listing, retry, discard, cross-tenant DLQ isolation, and audit coverage for schema, API-key, role, user, ingestion, and privacy mutations.
- Replay identity tests cover anonymous-to-known linking, explicit merge semantics, target-attribute precedence, latest-consent preservation, alias non-forking, and equivalent-order checksum stability.
- Retention enforcement removes expired raw accepted events while preserving current profile/consent projections and nulling expired source-event references.
- Backup/restore rehearsal creates a temporary PostgreSQL database, restores a gzip dump into it, compares core table counts, verifies a read query, and drops the temporary database.
- Production-data smoke validates transactional outbox dispatch through Redpanda, archive object creation in S3-compatible storage, ClickHouse analytics ingestion, and published outbox state.
- Telemetry smoke validates Prometheus metrics scraping and OTLP trace export for an authenticated event ingestion request.
- Worker termination smoke validates that a killed worker leaves the job recoverable and a restarted worker projects it successfully.
- Load smoke validates bounded ingestion at 20 batches/s with 10 events/batch for 30 seconds, 0 failed requests, and p99 latency below 500 ms.
- CI gates serialized Go race/vet/gofmt/govulncheck with live PostgreSQL integration coverage, web and SDK typecheck/test/build/audit, OpenAPI linting, shell script syntax/executability, container builds, reduced Compose smoke, worker termination smoke, telemetry smoke, production-data smoke, load smoke, and restore rehearsal.
- Release workflow publishes app and web images with provenance attestations and per-image SBOM artifacts.
- JavaScript SDK typecheck/test/build covers batching, durable retry queue, identity reset, profile attributes, consent, alias, merge, and auto-flush behavior.
- Built-in local operator auth bootstraps a self-hosted administrator, stores bcrypt password hashes, issues expiring bearer sessions, reuses tenant RBAC scopes, updates session last-use metadata, and supports server-side session revocation.
- Automated OIDC verifier tests validate provider discovery, JWKS signature verification, issuer/audience checks, required tenant/workspace/application claims, and rejection of malformed tenant context.
- Optional OIDC provider validation has a repeatable `make validate-oidc-provider` gate that checks discovery metadata and uses a fresh real-provider ID token against the API without storing secrets.

## Remaining Phase 1 work

- None for the platform kernel. Real-provider OIDC sign-in validation is optional deployment evidence for teams that choose external SSO.

Import/migration tooling is intentionally deferred from the current Milestone 1 execution scope.

Audience delivery, email, templates, campaigns, and journeys remain Phase 2 or later and are not Phase 1 requirements.

The security and compliance controls in the initial slice are foundations, not a certification or production-readiness claim by themselves.
