# OpenJourney implementation plan

Status: proposed architecture and delivery plan  
Research date: 2026-07-06  

## 1. Objective

OpenJourney will be an open-source, self-hostable customer engagement platform combining:

- Braze-style real-time customer profiles, segmentation, cross-channel messaging, journey orchestration, experimentation, content cards, feature flags, and high-volume event activation.
- Contacts and companies, forms, landing pages, assets, lead scoring, static and dynamic segments, campaign automation, broad integrations, and operator-owned deployment.
- AI-native creation, analysis, decision support, and automation with strict authorization, auditability, evaluation, and human approval controls.

The platform must support both a small installation that runs work periodically and a large installation with continuously warm workers. Application compute will be stateless; durable state will live in databases, event logs, object storage, and a workflow/timer subsystem.

## 2. Non-negotiable design decisions

### 2.1 Primary technology choices

| Area | Choice | Reason |
|---|---|---|
| Backend and workers | Go | Good throughput and memory behavior, fast startup, static binaries, strong networking/concurrency ecosystem, and lower contributor cost than a Rust-first system |
| Web application | TypeScript, React | Strong ecosystem for a visual DAG editor, content tools, and typed API clients |
| ML training/evaluation | Python, isolated from runtime | Access to ML tooling without putting Python on ingestion or delivery hot paths |
| Transactional state | PostgreSQL | Strong consistency for tenants, identities, consent, definitions, audit indexes, deduplication, and job leases |
| Behavioral analytics | ClickHouse | High-rate append and high-cardinality analytical queries without destabilizing OLTP |
| Event transport | Kafka-compatible API; Redpanda is the simple deployment option | Durable replay, partition ordering, backpressure, and independent consumers |
| Assets and cold data | S3-compatible object storage | Imports, exports, Parquet archives, media, templates, model artifacts, and immutable audience manifests |
| Workflow runtime | Internal abstraction with PostgreSQL-backed and Temporal implementations | Small installs need low dependency count; large installs need durable high-scale timers and execution histories |
| Cache | Optional Redis | Rate-limit and rendering acceleration only; never authoritative |
| Public API | REST/JSON with OpenAPI | Broad interoperability and generated SDKs |
| Internal API/events | Connect RPC/gRPC and Protobuf | Typed contracts and efficient service-to-service communication |
| Observability | OpenTelemetry | Vendor-neutral traces, metrics, and logs |
| Extensions | Remote HTTP/Connect adapters and sandboxed WASI/Wasm | Isolation, language neutrality, capability declarations, and safe resource limits |

Go is the default, not a dogma. Rust should only be introduced after profiling identifies a contained hotspot such as an expression evaluator, SDK batching core, or stream transformation runtime.

### 2.2 Architecture shape

Start as a modular control-plane application plus separately scalable data-plane workers. Do not begin with dozens of microservices. Boundaries and event contracts must be explicit from day one, allowing hot subsystems to be extracted without redesign.

```text
SDKs / REST / forms / webhooks / files / warehouses
                         |
                 ingestion gateway
          auth, quota, schema, dedupe, routing
                         |
                durable event stream
          profile | behavior | consent | delivery
             /              |                 \
     profile projector   audience engine    export sinks
        PostgreSQL       ClickHouse/SQL      object stores
             \              |
                 journey runtime
       trigger -> eligibility -> durable state -> intent
                         |
                  delivery router
       policy -> render -> rate limit -> provider adapter
            /       /       |       \          \
         email     SMS     push    webhook    in-product
                         |
                callbacks and engagement
                         |
                 durable event stream
```

Control plane:

- Tenants, workspaces, applications, users, teams, roles, credentials, quotas.
- Schemas, catalogs, templates, campaigns, journey definitions and versions.
- Provider configuration, AI policy, approvals, audits, reports, operations.

Data plane:

- Event ingestion, identity resolution, profile projection, audience evaluation.
- Journey execution and timers, rendering, delivery, callback ingestion.
- Behavioral analytics, exports, imports, and deletion propagation.

No data-plane request may depend synchronously on the UI process.

## 3. Definition of stateless and intermittent operation

“Stateless” must not mean “nothing durable is running.” Scheduled messages, action windows, retries, and incoming events cannot work without durable storage and a trigger mechanism.

The required semantics are:

- API and worker processes contain no authoritative state.
- Every task is leased, idempotent, checkpointed, bounded, and restartable.
- A process can stop after any item without losing accepted work.
- Timers are persisted records, not sleeping goroutines or resident in-memory jobs.
- The system catches up after downtime according to an explicit late-work policy.
- Small installations can invoke bounded jobs from cron, Kubernetes Jobs, container jobs, or functions.
- Large installations can run the identical handler code as continuous consumers.

Three deployment modes:

1. **Dormant/batch:** UI/API and bounded scheduled invocations. Minutes-level freshness is acceptable.
2. **Event-triggered:** queues or managed schedules start workers on demand. Seconds of cold-start latency are acceptable.
3. **Realtime sustained:** ingestion and critical consumers stay warm for predictable latency and lower unit cost.

Each worker supports continuous and bounded forms:

```text
openjourney worker run --queue delivery.email
openjourney worker drain --queue delivery.email --max-items 10000 --max-duration 10m
openjourney reconcile --domain audiences --shard 42 --max-duration 15m
```

Durable infrastructure remains available in all useful modes. Universal scale-to-zero is not a valid goal for Braze-class sustained traffic.

## 4. Findings from Braze and Marketing Automation Platforms

### 4.1 Braze capabilities to reproduce

The primary Braze product loop is:

```text
collect -> resolve identity -> update profile -> classify audience
-> orchestrate journey -> personalize -> deliver -> measure -> optimize
```

Required capability groups:

- Known and anonymous profiles, aliases, devices, attributes, events, purchases, consent, and explicit identity merging.
- SDK, REST, file, webhook, partner, and warehouse ingestion.
- Reusable dynamic segments based on profile, event, purchase, engagement, device, consent, prediction, and catalog data.
- Single-message campaigns and versioned multi-step Canvas-like journeys.
- Email, push, in-app, content cards, SMS/MMS, WhatsApp, webhooks, and extensible channels.
- Liquid-compatible personalization, reusable content blocks, localization, and safely fetched connected content.
- Catalogs, recommendations, feature flags, experiments, holdouts, and conversion goals.
- Currents-style raw event export and near-real-time analytics.
- Fine-grained permissions, approvals, SSO/SCIM, audit, consent, suppression, DSAR, and retention controls.

Public scale figures provide architectural headroom, not MVP acceptance criteria. Braze reports 25.8 trillion data points, 10.2 trillion API calls, 4.5 trillion messages/Canvas actions, about 13 trillion exported events, more than 7.8 billion monthly active users, and 99.99%+ uptime during 2025. This implies tenant-aware partitioning, cells, independent scaling, noisy-neighbor controls, and replayable pipelines from the beginning.

### 4.2 Marketing automation capabilities to preserve

Established self-hosted marketing automation platforms commonly cover contacts, companies,
campaigns, email, SMS, notifications, forms, pages, assets, dynamic content, APIs,
webhooks, integrations, reporting, scoring, and users.

The useful semantics to retain are:

- Contact/custom-field/company model and anonymous web tracking.
- Static and dynamic segments, manual membership overrides, and preference-center visibility.
- Campaign graphs containing actions, decisions, conditions, time schedules, yes/no paths, re-entry, and an execution ledger.
- Forms, pages, tracked links, assets, email, SMS, notifications, scoring, stages, and contact timelines.
- Channel suppression, contact frequency limits, queueing, retries, imports/exports, APIs, webhooks, reports, and plugins.
- Batch operation through independently scheduled commands.

The implementation must avoid:

- A single process and schema coupling UI, ingestion, orchestration, delivery, and analytics.
- Full-population database scans as the normal segment-update path.
- Relational tables acting simultaneously as scheduler, queue, execution ledger, and analytical store.
- Synchronous queue defaults and correctness dependent on manually coordinated cron commands.
- Runtime custom fields implemented as physical columns.
- Mutable local filesystem requirements.
- In-process extensions with unrestricted access to core internals.
- Mixed legacy and new API generations.

Do not copy third-party source into OpenJourney without legal review and license
compatibility approval. Maintain a design-provenance log, and keep compatibility claims
neutral and precise.

## 5. Product model and bounded contexts

### 5.1 Tenancy and access

Hierarchy:

```text
organization/tenant
  -> workspace
    -> application
      -> SDK/API credentials and environments
```

Requirements:

- A workspace is the principal isolation boundary for profiles, campaigns, integrations, API keys, settings, and reports.
- Every persisted row, object, event, cache key, trace, and job includes tenant/workspace identity.
- Tenant context comes from authenticated credentials, never an untrusted request body.
- Support users, teams, service accounts, custom roles, resource scopes, and separation of create/approve/publish.
- Support OIDC initially; SAML and SCIM in the enterprise phase.
- API keys have scopes, expiry, last-use records, rotation, and revocation.
- Tenant-to-cell and tenant-to-region mappings are explicit and audited.

### 5.2 Identity and profiles

Core objects:

- Canonical subject/profile with immutable internal ID.
- External IDs scoped by namespace and application.
- Aliases and anonymous IDs.
- Channel endpoints: email addresses, phone numbers, push tokens, web-push subscriptions.
- Devices/installations with platform, app version, locale, timezone, token state, and activity.
- Standard fields plus typed custom attributes, nested objects, and arrays.
- Companies/accounts and typed subject relationships.
- Purchases/orders and custom behavioral events.

Identity requirements:

- Email and phone are lookup attributes, not implicitly unique canonical identities.
- Identification and merge are explicit commands with deterministic policy.
- Every merge stores source identities, winner, policy/version, actor, timestamp, and reversible provenance where legally permitted.
- Events received before identification remain associated with the anonymous subject and are joined through audited identity edges.
- Concurrent profile updates use optimistic versions and deterministic conflict policy.
- Profile projections are rebuildable from accepted immutable events plus identity commands.

### 5.3 Consent and communication policy

Store an immutable consent ledger and current projection:

- Purpose, channel, topic/subscription group, state, legal basis.
- Source, jurisdiction, policy version, evidence reference, occurred time.
- Global suppression and endpoint-specific suppression.
- Bounce, complaint, invalid endpoint, unsubscribe, and administrator blocks.

The policy engine evaluates immediately before every send:

- Current consent and suppression.
- Workspace and campaign exclusions.
- Quiet hours and recipient timezone.
- Frequency caps and fatigue policy.
- Age, jurisdiction, and channel rules.
- Provider/sender eligibility.
- Transactional versus marketing classification.

Policy decisions are versioned, explainable, and written to the causal audit stream.

### 5.4 Event ingestion

Initial ingestion paths:

- Public batch REST endpoint.
- JavaScript/web SDK.
- Form and tracking endpoints.
- Signed inbound webhooks.
- CSV/JSONL/Parquet imports from object storage.
- Server-side Go, TypeScript, Python, Java, and PHP SDKs generated around the REST contract.

Later:

- iOS and Android SDKs.
- React Native and Flutter wrappers.
- Warehouse and CDP connectors.
- Cloud event streams and zero-copy/federated audience sources.

Canonical envelope:

```text
event_id: UUIDv7/ULID
tenant_id, workspace_id, app_id
canonical_subject_id? / external_id? / anonymous_id? / device_id?
event_type, schema_version
occurred_at, received_at
source, source_event_id
correlation_id, causation_id, traceparent
idempotency_key
consent_context
data_classification
payload
```

Ingress behavior:

- Authenticate, authorize, enforce body and schema-complexity limits, and apply tenant quota.
- Validate each item independently and return partial errors.
- Durably accept before asynchronous projection.
- Deduplicate by tenant/source/idempotency key with a configured window.
- Preserve event time and ingestion time separately.
- Define tenant-configurable late-event and future-clock-skew policies.
- Quarantine invalid data with redacted diagnostics and replay controls.
- Publish immutable correction events; never mutate historical facts.

Delivery guarantee is at-least-once with idempotent consumers. Do not promise global exactly-once behavior across external providers.

### 5.5 Schemas and catalogs

Schema registry:

- Typed profile attribute, event, purchase, and catalog schemas.
- Additive compatibility by default; breaking changes require a new major schema version.
- PII/sensitive classification, retention, residency, and permitted-use metadata at field level.
- Validation, sample payload, owner, status, and usage/dependency graph.

Catalogs:

- Typed tenant-scoped tabular data with stable item IDs.
- UI, API, CSV, and warehouse synchronization.
- Bulk upsert/delete, selections, filters, version/sync status, and item history.
- Template lookup, audience joins, price-drop/back-in-stock triggers, and product blocks.
- Recommendation output references catalog IDs plus model and explanation metadata.

### 5.6 Audiences and segmentation

Audience types:

- Static list.
- Dynamic live audience.
- Scheduled materialized audience.
- Immutable snapshot for a launch.
- Imported or federated warehouse audience.
- Suppression audience.

Typed DSL supports:

- Profile and custom attributes.
- Identity, endpoint, device, locale, timezone, and geography.
- Consent, subscription group, suppression, and frequency state.
- Event/purchase occurrence, recency, frequency, count, aggregate, and property predicates.
- Campaign/journey membership and engagement.
- Company/account relationships.
- Catalog joins and feature-flag exposure.
- Predictions, recommendations, and imported cohort membership.
- Nested AND/OR/NOT, include/exclude groups, relative dates, and safe regex.

Each audience stores:

- Source DSL and normalized typed AST.
- Schema dependencies and version.
- Planner strategy and estimated scan/cost.
- Membership version and freshness contract.
- Count estimate, exact count job, and sample.
- Explain plan and sampled “why included/excluded.”

Execution strategies:

- PostgreSQL for small profile-only predicates.
- ClickHouse for behavior, aggregates, and large scans.
- Incremental stream evaluator for supported live predicates.
- Warehouse SQL adapter for federated audiences.

Segment changes are evaluated from profile/event deltas. Periodic bounded reconciliation verifies correctness. A large launch freezes membership as partitioned manifests in object storage so retries are reproducible.

### 5.7 Campaigns and journeys

A campaign is a one-off, recurring, action-triggered, or API-triggered delivery with:

- Audience and exclusions.
- Variants/control.
- Schedule, timezone policy, quiet hours, rate limit, and frequency policy.
- Conversion goals and attribution windows.
- Test send, approval, draft/scheduled/running/paused/completed/archived state.

A journey is an immutable published DAG. Draft changes create a new version. Active participants remain pinned unless an explicit migration is validated and audited.

Required nodes:

- Entry: scheduled audience, event, API, attribute/audience change, catalog trigger.
- Filter/condition and prioritized audience split.
- Deterministic random/experiment split.
- Delay by duration, date, local time, or calculated value.
- Wait for event/action with success and timeout paths.
- Message or multi-channel message.
- Profile, tag, score, stage, consent-safe subscription, or audience mutation.
- Webhook/integration action.
- Feature-flag action.
- Nested journey entry/exit.
- Goal, conversion, and explicit exit.
- AI decision with strict schema, budget, timeout, and deterministic fallback.

Execution identity:

```text
tenant + journey_version + subject + entry_event + reentry_sequence
```

Runtime requirements:

- Durable per-participant state and full causal transition history.
- Deterministic graph version and experiment assignment.
- Global and branch exit criteria, re-entry limits, and conversion windows.
- Unique node execution and delivery intent keys.
- Atomic state transition plus outbox publication.
- Pause, resume, cancel, replay, migrate, backfill, and operator override.
- Explicit handling for late events and downtime catch-up: run, skip, reschedule, or require approval.
- Retry classes: transient, throttled, permanent, policy-denied.
- Dead-letter inspection and safe operator replay.
- No provider call from deterministic orchestration code; orchestration emits action intents.

For huge one-shot broadcasts, create partitioned audience/send manifests. Do not create millions of heavyweight workflow histories before they are needed.

### 5.8 Content and personalization

Content resources:

- Templates, channel variants, reusable blocks, themes, assets, translations, and brand kits.
- Email visual/HTML/text editor.
- SMS/push/in-app/content-card editors.
- Forms and landing pages after the messaging foundation is stable.

Use a documented Liquid-compatible safe subset:

- Profile, event, purchase, catalog, campaign, and journey contexts.
- Conditions, loops, filters, defaults, localization, and abort.
- Static linting, preview with synthetic/real authorized profiles, and render diagnostics.
- HTML escaping by context, sanitizer, execution budgets, and output-size limits.
- Immutable source/template and rendered snapshots for delivery audit.

Connected content:

- Explicit egress allowlists.
- DNS/IP and redirect validation, private-network denial, timeouts, response-size limits.
- Tenant-scoped auth references, caching, circuit breakers, and deterministic fallbacks.
- No credentials in templates, event streams, logs, workflow histories, or AI prompts.

### 5.9 Channel delivery

Common pipeline:

```text
action intent
 -> current eligibility/policy
 -> reserve frequency/rate capacity
 -> deterministic render
 -> provider selection
 -> provider limiter
 -> send attempt
 -> normalized result/callback
 -> engagement event and analytics
```

Adapter interface:

```text
ValidateConfiguration
ValidateMessage
Capabilities
EstimateCost
Send
QueryStatus (optional)
ParseCallback
ClassifyError
```

Message states:

```text
created -> policy_allowed/denied -> rendered -> queued
-> attempted -> provider_accepted -> delivered
-> opened/clicked/conversion
or throttled/deferred/failed/bounced/complained/unsubscribed
```

Requirements:

- Stable delivery intent key and provider idempotency token where supported.
- Provider credential references backed by KMS/Vault.
- Tenant/provider/channel rate limits and weighted fair scheduling.
- Retry honoring provider `Retry-After`, exponential backoff, jitter, max attempts, and expiry.
- Routing, fallback, circuit breakers, cost accounting, and reconciliation.
- Provider callbacks normalized into canonical immutable events.
- Transactional sends receive reserved capacity ahead of marketing broadcasts.

Channel sequence:

1. Email and webhook.
2. SMS and mobile/web push.
3. In-app messages and content cards.
4. WhatsApp and additional providers.
5. Paid audience sync and regional channels based on demand.

Email additionally requires domain authentication state, unsubscribe headers, bounce/complaint processing, tracked links, open tracking controls, and reputation metrics. Push requires token lifecycle and invalid-token cleanup. Webhooks require HMAC, timestamp/nonce replay defense, destination circuit breakers, and SSRF-safe egress.

### 5.10 In-product messaging and feature flags

Content cards:

- Per-user targeted cards with start/expiry, rank, categories, and arbitrary payload.
- Read, dismissed, impression, and click state.
- SDK sync, cache, offline behavior, and customizable rendering.
- Conversion and revenue metrics.

Feature flags:

- Environment-scoped key, enabled state, typed properties, variants, prerequisites, and kill switch.
- Segment/rule targeting, deterministic percentage rollout, stable bucketing, and exposure events.
- SDK cache/default behavior and rapid rollback.
- Coordinated journey messaging and experiment analysis.

### 5.11 Experimentation and analytics

Experiments:

- Deterministic stable assignment.
- A/B and multivariate variants, control/holdout, allocation percentages.
- Primary/secondary conversion events, windows, revenue metrics, and guardrails.
- Frequentist or Bayesian method documented per report; never mix interpretations.
- Winner recommendation and separately approved rollout.

Canonical funnel:

```text
audience -> eligible -> targeted -> intent -> attempted
-> provider accepted -> delivered -> opened -> clicked
-> converted/revenue
```

Reports:

- Campaign, journey, node, variant, audience, cohort, locale, device, provider, and time dimensions.
- Total and unique metrics with explicit definitions.
- Deliverability, frequency, retention, funnel, audience growth, and cost.
- Late callback reconciliation and metric-definition versioning.
- Near-real-time operational dashboards from ClickHouse.
- Raw immutable event export to streams, warehouses, and object storage.

Attribution model and conversion window must be stored with every published campaign/journey version.

### 5.12 Forms, pages, scoring, and baseline automation

After reliable ingestion, audiences, email, and journeys:

- Form builder with typed fields, validation, consent evidence, spam protection, progressive profiling, and submit actions.
- Landing-page builder with versioned publication, custom domains, asset storage, analytics, and safe forms.
- Redirect/link tracking and UTM attribution.
- Rules-based points, stages, score history, and score-triggered audiences/journeys.
- Company/account profiles and relationship targeting.
- Imports for contacts, custom fields, companies, tags, segments, templates, suppressions, and supported basic campaigns.

Third-party plugins will not be binary-compatible. Migration and documented extension
equivalents are the target.

## 6. AI-first specification

AI-first means every important operation is exposed through safe typed APIs and tools. It does not mean a nondeterministic model controls every send.

### 6.1 AI gateway

- Provider-neutral interface for hosted and local models.
- Structured generation, embeddings, reranking, moderation, and image-reference support.
- Tenant-specific provider choice, budgets, latency, quality, data-use, and fallback policy.
- Prompt/model registry with immutable versions.
- Token, cost, latency, schema-rejection, safety, and outcome telemetry.

### 6.2 Agent/tool surface

Authorized agents can:

- Inspect schemas and aggregate/sample-safe statistics.
- Draft and validate audience DSL.
- Explain membership without exposing unauthorized fields.
- Draft journeys and content variants.
- Validate graph, reach, cost, consent, and provider readiness.
- Simulate against synthetic or authorized historical data.
- Localize and check content against brand and compliance policy.
- Summarize performance and propose a new version.
- Create draft resources.
- Publish only with the same approval token required of a human.

MCP-compatible tools may be exposed, but the domain REST/RPC API remains authoritative.

### 6.3 Governance and safety

- Retrieval applies tenant, workspace, role, purpose, and field-level authorization.
- Sensitive fields are redacted or tokenized before model calls.
- User-generated/catalog/webhook data is untrusted and isolated from system instructions.
- Model outputs conform to JSON Schema and pass deterministic validators.
- High-volume sends, publishing, budget changes, and new data exports require policy-based human approval.
- Every AI action records model/provider/version, prompt version, retrieval references, tool calls, classification, cost, output, policy decision, and approver.
- Realtime AI nodes have strict timeout, budget, schema, and deterministic fallback.
- AI proposes a new immutable resource version; it never self-modifies a live journey.
- Offline evaluation covers hallucination, policy breaches, unauthorized retrieval, latency, cost, and business lift.
- Online optimization uses holdouts and guardrails.

Initial AI deliverables:

1. Content drafting, localization, subject-line variants, and content QA.
2. Natural-language-to-audience DSL with plan preview and explainability.
3. Journey drafting and deterministic validation.
4. Campaign performance summaries and suggested next version.
5. Recommendations and predictive scores.
6. Bounded channel/timing/content decisioning only after evaluation infrastructure exists.

## 7. Data, partitioning, and consistency

### 7.1 PostgreSQL ownership

PostgreSQL is authoritative for:

- Tenancy, access, credentials metadata, and quota policy.
- Identity graph and current profile/consent projections.
- Schemas, templates, campaigns, journey versions, and approvals.
- Delivery intent uniqueness and operational execution references.
- Outbox/inbox, job leases, cursors, checkpoints, and audit index.
- Deletion/tombstone state.

Use tenant-aware composite keys, declarative partitioning for large tables, row-level security as defense in depth, and repository APIs that require tenant context.

### 7.2 ClickHouse ownership

ClickHouse stores rebuildable projections:

- Behavioral, purchase, engagement, and delivery events.
- Event aggregates and audience acceleration data.
- Membership deltas and campaign analytics.
- Usage and AI-evaluation facts.

It is not authoritative for consent, deduplication, journey definitions, or send uniqueness.

### 7.3 Object storage ownership

- Original imports and rejected records.
- Partitioned Parquet event archives.
- Audience and broadcast manifests.
- Exports, media, templates, model artifacts, and backups.
- Checksums, encryption, object versions, retention, and lifecycle tiers.

### 7.4 Stream rules

- Partition profile-affecting topics by tenant plus canonical subject to preserve subject order.
- Partition broadcasts by tenant plus campaign shard to avoid hot partitions.
- Do not create one topic per tenant.
- Tenant identity is present in key, payload, ACL validation, trace, and metric dimensions.
- Use outbox/inbox at database boundaries.
- Deduplicate at ingress and every irreversible side-effect boundary.

Initial topic families:

```text
events.accepted.v1
profiles.commands.v1
profiles.changed.v1
consent.changed.v1
audiences.membership.v1
journeys.triggers.v1
journeys.actions.v1
delivery.intents.v1
delivery.attempts.v1
engagement.events.v1
exports.events.v1
deadletter.<domain>.v1
```

## 8. Scale, cells, and SLOs

### 8.1 Cell architecture

A workload cell owns a bounded set of tenants and contains:

- Ingestion partitions and consumers.
- Profile storage/projectors.
- Analytics shards.
- Workflow namespace/task queues.
- Delivery pools and rate limiters.

A global control plane maps tenants to home region and cell. Large or regulated tenants can receive dedicated data stores or an entire cell. This limits blast radius and enables horizontal growth.

### 8.2 Noisy-neighbor controls

- Per-tenant event/byte/API concurrency quotas.
- Segment query cost and concurrency budgets.
- Workflow start, timer, AI token, and send budgets.
- Weighted fair queues rather than global FIFO.
- Per-tenant circuit breakers and pause controls.
- Reserved transactional channel capacity.
- Dedicated shards/cells for exceptional tenants.

### 8.3 Initial SLO proposal

These values must be confirmed in Phase 0:

| SLI | Initial production target |
|---|---|
| Ingestion availability | 99.95% per month, excluding invalid/quota-rejected traffic |
| Ingestion acknowledgement | p99 under 250 ms in-region for valid batches within documented size |
| Accepted event durability | No acknowledged loss; RPO 0 for committed regional writes |
| Event-to-profile projection | p99 under 5 s realtime mode; documented schedule in batch mode |
| Event-to-journey trigger | p99 under 10 s realtime mode |
| Scheduled action lateness | p99 under 60 s realtime mode; explicit batch-mode contract |
| Provider enqueue latency | p99 under 5 s after eligible intent, excluding configured rate limits |
| Operational dashboard freshness | under 60 s p95 |
| Dynamic audience freshness | live under 30 s where supported; otherwise declared schedule |
| Control-plane availability | 99.9% initially |
| Data recovery | RPO/RTO defined per deployment tier and tested quarterly |

Phase 0 must define workload tiers:

- Active profiles and identities.
- Average and peak accepted events/second.
- Monthly and peak-hour sends by channel.
- Largest tenant and largest launch audience.
- Concurrent journey participants and due timers.
- Audience count, expression complexity, and freshness.
- Data retention and regional residency.

## 9. Security, privacy, and governance

Required controls:

- OIDC/SAML SSO, MFA, SCIM, service accounts, scoped and rotated API keys.
- Fine-grained tenant/workspace/application/resource permissions.
- Creator/approver/publisher separation and launch approvals.
- TLS everywhere; optional service mTLS.
- KMS/Vault envelope encryption and secret references.
- Immutable audit events for login, access, export, configuration, publish, delivery, deletion, and AI activity.
- SDK authentication/signing options; webhook HMAC and replay protection.
- WAF, quotas, decompression limits, parser fuzzing, and schema-complexity limits.
- Egress proxy and SSRF protection for connected content and plugins.
- SBOM, dependency scanning, signed images/releases, and provenance.
- Encrypted backups, restore drills, and region-aware disaster recovery.

Privacy workflows:

- Preference center and consent evidence.
- DSAR export, correction, suppression, and deletion.
- Durable tombstone propagation to PostgreSQL, ClickHouse, object storage indexes/manifests, caches, exports, and AI/vector stores.
- Retention by field/event type and tenant policy.
- Legal hold and anonymization rules.
- SDK disable-collection and deletion APIs.
- Deletion completeness tests and an auditable final report.

## 10. Extensibility

An extension manifest declares:

- Name, version, compatibility range, publisher, signature.
- Capabilities and requested scopes.
- Configuration schema and secret references.
- Subscribed event schemas.
- Actions/conditions and input/output JSON Schemas.
- Callback endpoints or Wasm exports.
- Health check, timeouts, memory/CPU/network limits.
- Retry, idempotency, and rate-limit semantics.

Extension types:

- Channel provider.
- Inbound/outbound connector.
- Journey action or condition.
- Ingestion transformation.
- Template function.
- Export destination.
- AI model/provider.

Remote extensions use signed HTTP or Connect RPC. Pure deterministic transforms may use WASI/Wasm with no default network/filesystem access. No dynamically loaded native Go plugins.

## 11. API and SDK requirements

- Public REST/JSON APIs described by OpenAPI.
- Cursor pagination and stable ordering.
- `Idempotency-Key` on mutations and ingestion.
- ETag/conditional update for editable resources.
- Bulk operations with per-item results.
- Long operations return an operation ID and status resource.
- OAuth2/OIDC flows and scoped API keys.
- Versioned, signed outgoing webhooks with replay and delivery logs.
- Additive API compatibility within a major version and published deprecation windows.
- Generated server SDKs; handwritten web/mobile SDKs where lifecycle behavior matters.
- Raw event export with stable schemas.

Do not emulate third-party API surfaces in v1. Compatibility means migration tools and
concept mapping, not plugin or API binary compatibility. Reconsider compatibility
endpoints only through a later versioned product decision backed by conformance tests.

## 12. Repository and module layout

Proposed layout:

```text
/cmd
  /api
  /worker
  /cli
/internal
  /tenancy
  /identity
  /profiles
  /consent
  /ingestion
  /schemas
  /catalogs
  /audiences
  /journeys
  /content
  /delivery
  /experiments
  /analytics
  /ai
  /audit
  /operations
/pkg
  /event
  /extension
  /sdk
/api
  /openapi
  /proto
  /events
/web
/sdk
/extensions
/deploy
  /compose
  /helm
  /terraform
/migrations
/tests
  /contract
  /e2e
  /load
  /resilience
/docs
  /adr
  /security
  /operations
  /compatibility
```

Modules expose ports/interfaces and own their migrations/contracts. Direct cross-module table access is forbidden; in-process calls use typed application interfaces and extracted services retain the same contracts.

## 13. Testing and release gates

### 13.1 Test layers

- Domain unit tests.
- Property tests for identity merge, audience normalization, consent precedence, caps, timezones, and experiment bucketing.
- Projection golden tests and rebuild checksum comparisons.
- PostgreSQL, ClickHouse, stream, object-store, and workflow-runtime contract tests.
- Public API and event-schema compatibility tests.
- Provider adapter suites using sandboxes and recorded fixtures.
- Fake-clock end-to-end journey tests.
- Duplicate, reorder, late event, missing callback, and partial-outage tests.
- Load and soak tests for ingestion, projection, audience evaluation, timer wakeup, launch fan-out, rendering, and callbacks.
- Failure injection at every persistence/provider boundary.
- Cross-tenant, RLS, fuzz, SSRF, template-sandbox, and DSAR completeness tests.
- AI prompt-injection, unauthorized retrieval, schema, safety, cost, and fallback evaluations.

### 13.2 Release gates

- No known cross-tenant access path.
- Event and API schemas pass compatibility checks.
- Workflow histories replay on the candidate build.
- Database migration expansion/migration/contraction is rehearsed.
- Performance budgets pass for the supported deployment tier.
- Backup restore evidence is current.
- Provider retry tests demonstrate no duplicate intent creation.
- Deletion propagation and consent-before-send suites pass.
- SBOM and signed artifacts are produced.

## 14. Delivery roadmap

Durations assume a focused team of 6–10 engineers plus product/design and platform support. They are sequencing estimates, not commitments.

### Phase 0 — decisions, prototypes, and benchmarks (3–5 weeks)

Deliver:

- Product vocabulary and capability matrix: MVP, later, explicitly excluded.
- Scale tiers, SLOs, retention, residency, compliance, and provider decisions.
- ADRs for Go, storage, stream, workflow runtime, tenancy, and licensing.
- Threat model, data classification model, event contracts, and benchmark harness.
- Prototype: Go ingest -> Kafka/Redpanda -> profile projector -> PostgreSQL/ClickHouse.
- Prototype both PostgreSQL due-job and Temporal runtime behind one interface.
- Migration inventory and concept mapping for common self-hosted marketing automation data.

Exit:

- Measured throughput/latency/cost results.
- Approved MVP scope and license.
- No unresolved architecture decision that changes the Phase 1 data model.

### Phase 1 — platform kernel (8–12 weeks)

Deliver:

- Tenant/workspace/application model, OIDC, RBAC, service accounts, API keys.
- Canonical event envelope, schema registry, batch ingest API, quotas, dedupe.
- Identity graph, profile projection, typed attributes, consent ledger.
- PostgreSQL, ClickHouse, object storage, event stream, outbox/inbox.
- Audit, OpenTelemetry, operations API, local Docker Compose, CI/release pipeline.
- JavaScript SDK with sessions, identify, attributes, events, consent, offline retry.

Exit:

- Replay rebuilds the same profile projection.
- Cross-tenant isolation suite passes.
- Accepted events survive worker/process failure.
- A low-dependency bounded-job mode is demonstrated.

### Phase 2 — audiences and reliable email (10–14 weeks)

Deliver:

- Typed audience DSL, parser, normalized AST, planner, explanations.
- Static, scheduled dynamic, live subset, suppression, and snapshot audiences.
- Incremental evaluation plus reconciliation.
- Template/content versioning, Liquid subset, preview, link tracking.
- Email and webhook adapters, callbacks, suppression, frequency policy.
- Launch manifests, sharded delivery, provider limiting, retry/reconciliation.
- Operational campaign analytics.

Exit:

- A dynamic segment broadcast is reproducible from an immutable manifest.
- Every recipient has an explainable eligibility and policy record.
- Delivery intents are effectively once under injected worker failures.

### Phase 3 — durable journeys (12–18 weeks)

Deliver:

- Visual DAG builder and validation.
- Immutable publish/version/approval flow.
- Event and scheduled entries, conditions, splits, delays, event waits/timeouts.
- Actions, goals, exits, re-entry, pause/resume/cancel, and conversion windows.
- Durable scheduler/runtime, bounded workers, backfill, DLQ, replay UI.
- Quiet hours, timezone/DST, caps, fairness, and reserved transactional capacity.
- Fake-clock simulation and workflow replay compatibility tests.

Exit:

- Event- and time-driven journeys survive all worker restarts.
- No resident process owns workflow truth or sleeping timers.
- Duplicate/reordered events and late callbacks have deterministic outcomes.

### Phase 4 — channel and acquisition baseline (10–16 weeks)

Deliver:

- SMS, mobile/web push, in-app messages, content cards.
- Forms, tracking, landing pages, assets, scoring/stages.
- Companies/account relationships.
- Imports, exports, warehouse/object-store connectors.
- Contacts/custom fields/companies/tags/segments/templates/basic-campaign migration.
- Remote/Wasm extension protocol.
- Experimentation, holdouts, attribution, and richer analytics.

Exit:

- A credible combined messaging and acquisition functional baseline.
- Provider contract suite and migration reconciliation reports.

### Phase 5 — governed AI layer (8–12 weeks)

Deliver:

- AI gateway, provider policies, prompt/model registry, budgets, telemetry.
- Permission-aware retrieval and PII controls.
- Content, audience, journey, and analysis assistants.
- Typed tools, simulation, approval, AI audit, and evaluation harness.
- Recommendations/predictions as versioned data artifacts.
- Optional bounded realtime decision node with deterministic fallback.

Exit:

- AI can draft, validate, simulate, and explain major resources.
- AI cannot bypass resource permissions, consent, approval, or publish controls.
- Safety, retrieval, latency, cost, and fallback evaluation gates pass.

### Phase 6 — enterprise scale and ecosystem (ongoing)

Deliver:

- Cell routing, tenant migration, dedicated isolation tiers, regional residency.
- Multi-region DR, advanced deliverability, SSO/SCIM, compliance evidence.
- Billion-event benchmark progression and sustained-load optimization.
- Feature flags, advanced recommendations/decisioning, zero-copy audiences.
- More providers, extension marketplace, usage metering, and upgrade tooling.

Exit:

- Defined scale tier passes sustained load, failure, fairness, and restore drills.
- A cell failure has a bounded and tested blast radius.

## 15. Workstreams and ownership

| Workstream | Primary output |
|---|---|
| Platform kernel | Tenancy, auth, schema, audit, API conventions, release tooling |
| Data and identity | Ingest, identity graph, profiles, consent, replay |
| Audience and analytics | DSL, planner, ClickHouse, membership, reports |
| Journey runtime | DAG, timers, execution state, simulation, operator tools |
| Content and channels | Editors, Liquid, policy, provider adapters, callbacks |
| SDK and acquisition | Web/mobile SDKs, tracking, forms, pages |
| AI and optimization | Gateway, tools, retrieval, evaluation, recommendations |
| Security and operations | Threat model, secrets, privacy, observability, DR, cells |
| Migration and ecosystem | Importer, compatibility tests, extension protocol |

Each phase should ship vertical slices across these workstreams instead of independently completing large horizontal frameworks.

## 16. Principal risks

| Risk | Consequence | Mitigation |
|---|---|---|
| “Stateless” interpreted as no durable coordinator | Lost timers and duplicate sends | Persist all progress; scale compute, not state, to zero |
| Pure serverless used for sustained high load | Cost, cold starts, platform limits | Dual deployment model with warm critical consumers |
| Microservices introduced too early | Slow delivery and consistency failures | Modular control plane; extract only measured hot paths |
| Exactly-once promised across providers | Incorrect semantics and customer harm | At-least-once plus unique intents, idempotency, reconciliation |
| One heavy workflow per bulk recipient | Storage/history explosion | Immutable manifests, sharded expansion, lazy participant state |
| ClickHouse used as operational truth | Weak transactional correctness | PostgreSQL owns consent, definitions, dedupe, and intents |
| Cross-tenant leakage | Existential security failure | Mandatory tenant context, RLS, cells, property tests |
| Celebrity tenant/profile creates hot partitions | Lag and unfairness | Adaptive sharding, fairness, dedicated cells |
| AI placed unbounded on send path | Latency, cost, nondeterminism | Precompute; strict timeout/schema/budget/fallback |
| Model sees excessive PII | Regulatory and trust failure | Classification, retrieval ACL, redaction, audit |
| Workflow upgrade breaks replay | Stuck live journeys | Versioned graphs/code and replay release gates |
| Full broad-platform parity attempted at once | Program never reaches reliable core | Phase-gated vertical slices and explicit exclusions |
| Extensions run in the main process | Security and availability loss | Remote/Wasm capability sandbox |
| Provider retry duplicates messages | Customer harm | Stable intent, provider idempotency, reconciliation |
| Analytics load affects control plane | Outages | Separate analytical storage and resource pools |
| GPL code is copied unintentionally | Licensing conflict | Behavioral reference only, provenance log, legal review |

## 17. Accepted product decisions

The authoritative decision record is [`product-decisions.md`](product-decisions.md). The implementation plan uses these constraints:

- Build a stable self-hosted messaging-core v1 for technical growth teams.
- Provide a core visual UI backed by complete typed APIs.
- Ship email and webhooks first, with Amazon SES as the first email adapter.
- Support migration tooling only; do not promise plugin/API compatibility with third-party platforms.
- Use Apache-2.0 and keep third-party GPL source out of the codebase unless legally approved.
- Remain strictly cloud-neutral.
- Support a reduced PostgreSQL plus object-storage deployment profile.
- Implement PostgreSQL durable jobs first and qualify Temporal behind the same interface for higher scale.
- Support multi-tenant workload cells and 13-month default behavioral retention.
- Prove 10 million profiles, 2,000 sustained events/second, 10 million messages/day, and a one-million-recipient broadcast.
- Make the first release GDPR-ready and HIPAA-ready at the technical-control level without claiming certification or a hosted BAA program.
- Support hosted and local AI models; AI can create drafts, but a permitted human approves publication and bulk sends.
- Add native mobile SDKs, companies, forms, pages, assets, and scoring after core journeys.

## 18. Research references

Braze:

- [Product and platform overview](https://www.braze.com/product)
- [User tracking API and identity/data payloads](https://www.braze.com/docs/api/endpoints/user_data/post_user_track)
- [Flexible user identification](https://www.braze.com/resources/articles/building-braze-how-braze-supports-flexible-user-identification)
- [Dynamic segmentation](https://www.braze.com/resources/articles/dynamic-segmentation)
- [Canvas Flow architecture and capabilities](https://www.braze.com/resources/articles/building-braze-how-braze-built-our-canvas-flow-customer-journey-tool)
- [Action Paths](https://www.braze.com/resources/articles/personalize-customer-journeys-in-real-time-with-braze-action-paths)
- [Cross-channel messaging](https://www.braze.com/product/cross-channel-messaging)
- [Content Cards](https://www.braze.com/product/content-cards)
- [Liquid personalization](https://www.braze.com/docs/user_guide/personalization_and_dynamic_content/liquid/)
- [Catalogs](https://www.braze.com/resources/articles/braze-catalogs)
- [Feature flags](https://www.braze.com/resources/articles/feature-flags-cohesive-experiences)
- [Reporting and analytics](https://www.braze.com/product/reporting-analytics)
- [Job queue resiliency](https://www.braze.com/resources/articles/building-braze-job-queues-resiliency)
- [2025 scale figures](https://www.braze.com/resources/articles/2025-how-braze-powered-exceptional-marketing-at-scale)
- [GDPR capabilities](https://www.braze.com/resources/articles/braze-gdpr-compliance)

Architecture:

- [Apache Kafka documentation](https://kafka.apache.org/documentation/)
- [PostgreSQL row security](https://www.postgresql.org/docs/current/ddl-rowsecurity.html)
- [ClickHouse](https://clickhouse.com/clickhouse)
- [Temporal durable execution](https://docs.temporal.io/temporal)
- [Temporal schedules](https://docs.temporal.io/schedule)
- [OpenTelemetry](https://opentelemetry.io/docs/what-is-opentelemetry/)

## 19. Immediate next actions

1. Create Phase 0 ADRs and startup-tier benchmark workloads.
2. Scaffold the Go module, React application, OpenAPI/Protobuf contracts, CI, and cloud-neutral local deployment.
3. Implement the ingest-to-profile vertical prototype and measure it.
4. Prototype PostgreSQL durable jobs and define the workflow interface used by the later Temporal implementation.
5. Produce the MVP capability matrix and convert Phases 1–3 into tracked epics with acceptance tests.
