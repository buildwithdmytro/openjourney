# GDPR and HIPAA technical-control mapping

This document describes technical readiness, not certification, legal advice, a BAA, or an attestation.

| Control objective | Implementation |
|---|---|
| Data minimization | Typed schemas, data-classification field, purpose-specific consent context |
| Lawful communication evidence | Append-only consent ledger with source event, time, channel, topic, state, and evidence |
| Access control | Built-in local operator sessions, optional OIDC verification, tenant/workspace RBAC, scoped/expiring/revocable API keys |
| Auditability | Tenant-scoped immutable audit events and OpenTelemetry traces/metrics |
| Integrity | Immutable accepted events, schema validation, idempotency conflict detection, replay checksum |
| Availability | Durable leases, retries, dead letters, backups, bounded restartable workers |
| Export/access request | Durable privacy export job producing an S3-compatible artifact |
| Erasure | OLTP deletion, object deletion, Kafka tombstone, ClickHouse hash deletion |
| Retention | 396-day default behavioral retention and ClickHouse TTL; object lifecycle must match deployment policy |
| Encryption | TLS-capable dependencies and external secret/KMS integration points; deployment owns at-rest encryption |
| PHI/PII identification | `data_classification` on every event and restricted classification for consent/privacy operations |
| Tenant isolation | Authentication-derived tenant context and cross-tenant integration tests |

Before production handling of PHI, operators must complete vendor BAAs, risk analysis, workforce controls, incident response, backup/restore evidence, and environment-specific encryption validation.
