# Phase 1 threat model

## Assets and boundaries

- Tenant customer profiles, behavioral events, consent evidence, credentials, exports, and audit history are sensitive assets.
- The public trust boundary is the HTTP API. Kafka, PostgreSQL, ClickHouse, and object storage are private data-plane dependencies.
- Tenant, workspace, and application context is derived from the authenticated API key, built-in local operator session, or verified OIDC claims. Request bodies cannot select a tenant.
- API keys are stored as SHA-256 digests and their raw values are returned only once.

## Principal threats and controls

| Threat | Phase 1 control |
|---|---|
| Cross-tenant access | Tenant predicates in every repository operation, authenticated tenant context, role bindings, integration isolation tests |
| Credential theft | Hashed API keys, bcrypt password hashes, expiring/revocable local sessions, expiry/revocation for API keys, minimal scopes, optional OIDC verification, secret-file support, no secrets in events or logs |
| Event spoofing/replay | Scoped ingestion keys, schema validation, tenant quotas, idempotency keys, conflict detection |
| Duplicate side effects | Transactional outbox, stable event IDs, idempotent object keys, ClickHouse replacement keys |
| Lost work after process failure | PostgreSQL leases, bounded workers, retry/dead-letter states, lease-recovery tests |
| Privacy data persistence | Export/delete workflows, PostgreSQL deletion, exact object deletion, ordered Kafka tombstones, ClickHouse subject-hash deletion |
| Malformed or oversized input | 1 MiB body limit, strict JSON decoding, batch limits, typed built-in events and JSON Schema |
| Browser credential exfiltration | Exact-origin CORS, authorization header only, no API secret emitted by list APIs, local session tokens and OIDC ID tokens kept session-only and not persisted to `localStorage` |
| Supply-chain compromise | Locked dependencies, vulnerability checks, SBOM, provenance attestations, minimal non-root runtime image |
| Operational repudiation | Immutable audit records for ingestion, schema, key, role, user, and privacy actions |

## Known deployment responsibilities

- Terminate TLS before the API and use TLS/mTLS for all remote data dependencies.
- Place stateful services on private networks and enable Kafka/ClickHouse/PostgreSQL authentication in production.
- Supply credentials through a secret manager mounted as `*_FILE`; Compose values are local-development credentials only.
- Encrypt disks, object buckets, backups, and transport; rotate keys according to organizational policy.
- For self-hosted deployments, configure `OPENJOURNEY_ADMIN_EMAIL` and `OPENJOURNEY_ADMIN_PASSWORD` through a secret manager and rotate local operator passwords according to policy.
- If external SSO is required, configure an OIDC provider that emits immutable `tenant_id`, `workspace_id`, and `app_id` claims. OIDC users must also be pre-provisioned and role-bound in OpenJourney.
- Restrict audit/export access and treat privacy artifacts as confidential.

## Verification

- API authorization and scope tests.
- PostgreSQL cross-tenant, local-session, and OIDC role integration tests.
- Ingestion idempotency conflict and schema tests.
- Projection lease recovery and replay checksum tests.
- Privacy export/delete and tombstone tests.
- Dependency audit, `govulncheck`, OpenAPI validation, container build, and full data-path smoke tests.
