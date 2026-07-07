# OpenJourney product decisions

Status: accepted  
Recorded: 2026-07-06  
Applies to: initial stable self-hosted release

## Product and audience

- The first production release is a reliable messaging core, not complete Braze or Mautic parity.
- Primary users are technical growth teams: engineers and lifecycle marketers comfortable with SDKs, schemas, providers, and infrastructure.
- The first release includes a core visual UI for profiles, audiences, templates, campaigns, journeys, and operations. Advanced administration may remain API-driven.
- The release bar is a stable self-hosted v1 with upgrade compatibility, backup/restore procedures, security controls, migration tooling, and published SLO and capacity limits.

## Initial scope

- First channels: email and webhook.
- First email provider: Amazon SES.
- Initial compatibility: migrate Mautic data and configuration; do not emulate Braze or Mautic APIs and do not support Mautic plugins.
- Companies, B2B relationships, forms, landing pages, assets, and lead scoring follow the reliable messaging core.
- Native iOS and Android SDKs follow the core journey release. The initial release uses web and server-side ingestion.

## Architecture and deployment

- Self-hosting is the first deployment priority.
- The implementation remains strictly cloud-neutral; AWS, GCP, and Azure deployment support must use portable service contracts.
- Small installations support a reduced batch profile using PostgreSQL and S3-compatible object storage without requiring Kafka, ClickHouse, or Temporal.
- PostgreSQL-backed durable jobs are implemented first. Temporal remains behind the same workflow interface and is qualified for higher-scale deployments.
- The tenancy model supports multiple isolated tenants in workload cells, with dedicated cells available for large or regulated tenants.
- Default behavioral-event retention is 13 months, with tenant-configurable shorter retention and cold archival.

## Capacity target

The first stable release must prove the startup workload tier:

- 10 million profiles.
- 2,000 sustained accepted events per second.
- 10 million messages per day.
- A one-million-recipient broadcast.

Load tests must also cover burst traffic, provider throttling, queue recovery, tenant fairness, and worker termination at persistence/provider boundaries.

## AI

- Support both hosted OpenAI-compatible providers and self-hosted/local model endpoints through one governed gateway.
- AI may draft, validate, simulate, explain, and create draft resources.
- A permitted human must approve publication and bulk sends.
- AI cannot bypass RBAC, consent, budget, policy, or audit controls.

## Security, privacy, and licensing

- Project license: Apache-2.0.
- The first production architecture is GDPR-ready and HIPAA-ready at the technical-control level.
- Required controls include data classification, PHI/PII handling, encryption, access controls, immutable audit records, consent evidence, retention, DSAR, deletion propagation, backup/restore, and deployment guidance.
- A hosted BAA program, certifications, and legal assurances are separate operational milestones; technical readiness does not claim certification.
- Mautic remains a GPL-3.0 behavioral reference. Its source must not be copied into OpenJourney without explicit legal review.

## Consequences

- Correctness, replay, policy enforcement, observability, and upgrade safety take priority over channel breadth.
- Cloud-specific managed-service optimizations cannot become required runtime dependencies.
- Scale-to-zero applies to stateless compute, not durable state.
- Later features must use the same identity, consent, event, workflow, delivery, and audit contracts established by the messaging core.

