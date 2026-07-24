# Changelog

## v1.1.0 — UX, stability & usability quality round (M19)

A second evidence-based quality pass (after the M18 security hardening), driven by a three-dimension
audit (UX-polish, stability, usability). No new features.

- **Stability:** worker loops now recover panics into the dead-letter path — a single poison
  message can no longer crash-loop the delivery/journey/projection fleet; added a root React error
  boundary, bounded previously-unbounded queries (segment resolution, short links), guarded unchecked
  type assertions, and fixed the toast timer/cap behavior.
- **Correctness:** removed a fabricated "citation-grounded" sparkline that invented a trend from a single
  value; added defensive optional-chaining and double-submit guards on publish/launch/provision actions.
- **Usability:** success toasts on every mutation (was one app-wide), actionable empty-state CTAs,
  destructive actions standardized on the styled confirm dialog (native `window.confirm` removed, identity
  unmerge now confirmed), a single source-of-truth navigation config with a rebalanced taxonomy, and a ⌘K
  command palette that runs actions and searches by keyword.
- **Design system:** finished the M12 component-library adoption the later sections had skipped
  (`PageHeader`, `Card`/`DataTable`, shared loading/empty/error primitives, `Field`-based forms with
  inline validation) and detoxed inline styles / hardcoded colors onto design tokens.
- Also resolved the M18 residual (three ignored in-transaction audit-write returns).

## v1.0.0 — Feature-complete + hardened

OpenJourney reaches **v1**: an open-source, self-hostable customer-engagement platform (a Braze-style
system) built in Go + React/TypeScript. It was delivered across **18 milestones**, each an event-sourced,
governed, multi-tenant slice with an audit doc under `docs/milestones/`. The core `plan.md` product model
is fully implemented, plus an enterprise layer and a dedicated hardening pass.

### What v1 delivers

**Platform & data**
- Multi-tenant kernel (tenant → workspace → application), event ingestion, outbox/inbox, projections,
  leased job queues, cursors, and a blob store.
- Identity graph: namespaced identity resolution with deterministic, reversible (tombstoned) merges.
- Data platform & connectors: warehouse/object-storage/stream **sources** (S3, ClickHouse, Kafka),
  **reverse-ETL** and Currents-style **event-stream export**, a leased scheduler — all governed, bounded,
  and event-sourced.

**Audiences, journeys & channels**
- Audiences/segments with a compiled DSL (Postgres + ClickHouse event-history predicates), consent, and
  suppression.
- Durable journeys (graph runtime, wait/condition/message/AI-decision/experiment nodes) and campaigns.
- Channels: email (SES + webhooks), SMS (Twilio), mobile & web push (FCM/APNs/VAPID), and **in-app
  messaging + content cards** with a public, IDOR-safe SDK delivery contract.
- Delivery pipeline: a robust state machine, policy engine (suppression/consent/frequency-cap),
  deterministic Liquid rendering, provider routing with SSRF-safe egress.

**Intelligence & experimentation**
- Governed AI layer: an LLM gateway (budget/timeout/PII-redaction/append-only audit), a prompt registry,
  copilots (content/audience/journey/performance/insights), and a **bounded, read-only, audited agentic
  assistant** over a governed tool registry.
- Predictive scoring (expression + LLM models, eval-gated + human-published), realtime AI decisioning,
  and advisory experiment optimization.
- Experimentation: deterministic stable bucketing, frequentist A/B, holdouts, guardrails.
- **Feature flags**: environment-scoped, typed variants, segment targeting, deterministic rollout, kill
  switch, exposure events, and an SDK evaluation contract.

**Analytics & reporting**
- Fact-table-based, exact-count reports: funnels, deliverability, retention/cohort, audience growth,
  cost, over-time — with a `ReportQuery` (time/dimension/filter) shape, versioned metric definitions,
  saved reports, a chart primitive, and dashboards.

**Content & personalization**
- Reference-data **catalogs** and **Connected Content** (governed, allowlisted, SSRF-safe, bounded,
  cached, audited send-time external data fetch) via a backward-compatible render-context seam.
- Acquisition: forms, landing pages, tracking, assets, imports, lead scoring, stages, companies.
- Extension ecosystem: signed remote-HTTP & WASI extensions with scopes, budgets, circuit breakers, and
  append-only activity audit.

**Console & enterprise**
- A React admin console with a design-system component library, tokens + dark mode, shared UX-state
  primitives, a command palette, and responsive/accessible layouts.
- Enterprise readiness: **SAML SSO**, **SCIM 2.0** provisioning, custom roles + teams + a unified
  permission catalog, **server-side maker-checker** separation of duties, a **tamper-evident
  (append-only + hash-chained) audit log**, and GDPR/CCPA **data-subject-request** workflows.

### Engineering notes
- **Dependency discipline:** exactly **one** third-party runtime dependency was added across the entire
  v1 build beyond the initial stack — `github.com/crewjam/saml` (vetted, for XML-signature verification;
  hand-rolling SAML crypto was deliberately avoided). Everything else reuses stdlib + existing patterns.
- **Security posture:** public edges are token/HMAC-authenticated and IDOR-safe (column-pinned subject
  resolution); all external egress passes a dial-time SSRF guard (RFC1918/loopback/link-local/CGNAT/ULA,
  DNS-rebinding-safe); secrets are `*_ref` only (never stored raw, redacted on read); audit/version tables
  are append-only (trigger + REVOKE + TRUNCATE-guard), with a hash-chained tamper-evident main audit log;
  the AI agent loop is bounded, read-only, scope-intersected, and audited.
- **Quality:** ~747 Go tests across 49 packages, ~320 web tests, ~30 SDK tests; `go vet` clean. A final
  **hardening milestone (M18)** fixed 18 audit findings (each with a regression test), including a
  deployment-breaking audit migration, a secret-exfiltration gap, and an API-key default-scope regression.

### Known minor items (non-blocking)
- Three `_ = s.audit(...)` call sites still ignore the (now in-transaction) audit return value —
  near-harmless, since a failed in-tx insert aborts the surrounding transaction on commit.
- Many `*_integration_test.go` suites are Postgres-gated (`OPENJOURNEY_TEST_DATABASE_URL`); the
  security-critical properties additionally have non-gated unit tests as of M18.
- Deferred by design: ClickHouse near-real-time operational dashboards, WhatsApp channel +
  deliverability/reputation tooling, and deeper predictive/ML model management (feature store, adaptive
  online optimization) — candidate directions for v1.x/v2.

### Build process
Milestones were executed as autonomous single-task loops (`cmd/ralph`) against explicit "recipe-book"
plans (`docs/milestones/v1-milestone-*-plan.md`), with per-milestone security/correctness review folded
into the next milestone's `N.0` closeout.
