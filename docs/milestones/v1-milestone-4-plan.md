# Phase 4 (slice) Implementation Plan: Experimentation & Analytics

Status: proposed. Builds on completed Phase 2 (campaigns, email/webhook delivery,
`delivery_attempts`, `message.*` engagement events) and Phase 3 (durable journeys with
deterministic split nodes — see [`v1-milestone-3-plan.md`](./v1-milestone-3-plan.md) and its
review). Delivers the **experimentation + analytics slice** of `plan.md` Phase 4: A/B variants
with control/holdout, deterministic stable assignment, conversion goals with attribution
windows, and **PostgreSQL-only** funnel / deliverability / uplift reports. The other Phase 4
slices — new channels (SMS/push/in-app), forms/pages/scoring, imports/connectors, the
extension protocol — are **deferred to later milestones**.

This is a **recipe book**, like the Phase 2 and Phase 3 plans. Every task references a recipe
and ends with a **Done when** check. **If a task feels ambiguous, open the named existing
file, copy it, rename, and change the fields.** The Phase 2 recipes (6.1–6.9) and Phase 3
recipes (6.10–6.16) still apply verbatim; this plan adds analytics recipes (6.17–6.20).

> **Milestone 9.0 comes first and is non-negotiable.** It folds in the P1/P2 defects found in
> the Milestone 3 review. Do not start 9.1 until 9.0 is green — the analytics features below
> add more per-workspace segmentation and would inherit the isolation bugs otherwise.

## Design decisions (locked)

1. **PostgreSQL-only analytics — no ClickHouse dependency for reports.** Every accepted event
   is already durably in Postgres (`accepted_events`), and the "targeted → sent" half of every
   funnel is already in `delivery_attempts` / `journey_message_intents`. ClickHouse stays
   **optional** (events keep flowing to it if configured); ad-hoc high-cardinality slicing and
   raw event export are deferred to a later "Data & connectors" milestone. This honors the
   `product-decisions.md` reduced-profile promise (Postgres + S3, *no* ClickHouse) and sidesteps
   the cross-workspace `subject_hash` collision the M3 review flagged.
2. **No analytical scans on OLTP hot tables.** `plan.md` (§4.2 risk table) forbids
   "full-population database scans" and "analytics load affecting the control plane." Reports
   are therefore **NOT** `GROUP BY` over `accepted_events.payload`. Instead, engagement and
   conversion **facts are projected into small dedicated rollup tables** by the existing
   projection worker — the exact pattern `ProjectEvent` already uses for `consent_ledger` and
   `suppressions` (`internal/postgres/store.go:421,447`). Reports are cheap indexed reads over
   those fact tables.
   - **Acknowledged deviation:** this differs from the letter of `plan.md §7.2` (which earmarks
     ClickHouse for "campaign analytics"). It is the right MVP call — simpler, reduced-profile
     compatible, scan-free — and ClickHouse remains the documented scale path for later.
3. **Experiments are a first-class resource that BIND to a campaign or a journey split/message
   node.** They do not replace them. Assignment is **deterministic + stable**:
   `sha256(experiment.seed + ":" + profile_id) % 10000`, reusing the M3 split bucketing
   (`internal/journey/nodes.go:266-278`) extracted into a shared `internal/experiment` package.
   Assignments are authoritative in Postgres (`experiment_assignments`, `UNIQUE(experiment_id,
   profile_id)`).
4. **`experiment_id` + `variant` are stamped onto both the Postgres disposition rows
   (`delivery_attempts` / `journey_message_intents`) and the emitted `message.*` event
   payloads**, so facts can be grouped by variant regardless of store.
5. **Conversion goal + attribution window are stored on the immutable published version**
   (campaign / journey_version / experiment), per `plan.md §5.11` ("attribution model and
   conversion window must be stored with every published campaign/journey version"). A
   conversion credits a send when the goal event occurs within `[sent_at, sent_at+window]` for
   the same subject.
6. **Statistics: frequentist only, documented per report.** A two-proportion z-test with a
   reported p-value and confidence interval; never mixed with Bayesian (`plan.md §5.11`). The
   **winner is a recommendation only** — rolling it out requires the same human-actor approval
   gate journeys use for publish (`product-decisions.md`: a human approves bulk sends / rollout).
7. **Charts are hand-rolled inline SVG — no new frontend dependency.** Bar/funnel/uplift charts
   don't need a library (unlike the M3 DAG editor, which genuinely did). Follow the `dataviz`
   skill for color/labeling. This keeps CI's `npm audit` surface unchanged.
8. **New store methods extend the existing `ports.Store` interface**; group implementations into
   new files per domain (`experiments.go`, `analytics.go`).

---

## 1. Architecture

```mermaid
flowchart TD
    subgraph Author[Authoring]
        EXP[Experiment: variants + control/holdout + seed] --> BINDC[bind to campaign]
        EXP --> BINDJ[bind to journey split/message node]
        GOAL[Conversion goal + attribution window] -->|frozen on publish| VER[(immutable version)]
    end

    subgraph Assign[Deterministic assignment]
        DISP[campaign dispatch / journey split] --> ASG[experiment.Assign sha256 seed:profile]
        ASG -->|variant or holdout| AR[(experiment_assignments)]
        ASG -->|stamp variant| DA[(delivery_attempts / journey_message_intents)]
    end

    subgraph Send[Delivery — reuses Phase 2/3]
        DA -->|variant template| SEND[adapter.Send]
        SEND -->|message.sent w/ experiment_id+variant| ING[AcceptEvents]
    end

    subgraph Facts[Projection-maintained facts — Postgres, scan-free]
        ING --> PROJ[projection worker / ProjectEvent]
        PROJ -->|delivered/opened/clicked/bounced| EF[(engagement_facts)]
        PROJ -->|goal event within window| CF[(conversion_facts)]
    end

    subgraph Report[Reports — cheap indexed reads]
        DA --> RPT[reports service]
        EF --> RPT
        CF --> RPT
        RPT --> API[GET /v1/reports/campaigns|journeys|experiments/id]
        API --> UI[React Reports + Experiments views: SVG funnel/uplift]
    end
```

**Reused unchanged:** the projection worker + `ProjectEvent` side-effect switch
(`store.go:394`), `AcceptEvents` ingestion + idempotency, `delivery_attempts` /
`journey_message_intents` dispositions, the deterministic split bucketing
(`internal/journey/nodes.go`), the leased-queue + worker patterns, RBAC/scopes, telemetry, and
the React `requestJSON` + section-view patterns.

### 1.1 Where each funnel stage's data lives (no ClickHouse)

| Funnel stage | Source table | Already exists? |
|---|---|---|
| targeted / eligible / **sent** / suppressed / no_consent / fatigued | `delivery_attempts` (campaign) · `journey_message_intents` (journey) | ✅ yes (add `variant`/`experiment_id`) |
| **delivered / opened / clicked / bounced / complained** | `engagement_facts` (new, projected from `message.*` + `email.opened`/`link.clicked`) | ➕ new projection |
| **converted / revenue** | `conversion_facts` (new, projected from goal events with attribution) | ➕ new projection |

A report is a `GROUP BY (source_id, variant)` over these three small, indexed tables — never a
scan of `accepted_events`.

---

## 2. Schema (new migrations)

Next numbers after `017_journey_delivery.sql`. Conventions as always: `CREATE TABLE IF NOT
EXISTS`, uuid PKs, `timestamptz`, tenant/workspace FKs, CHECK-constrained enums, idempotent
projection via `UNIQUE(source_event_id, ...)` + `ON CONFLICT DO NOTHING`.

### 2.1 `018_experiments.sql`
```sql
CREATE TABLE IF NOT EXISTS experiments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    name text NOT NULL,
    description text,
    subject_type text NOT NULL CHECK (subject_type IN ('campaign','journey')),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','running','completed','archived')),
    method text NOT NULL DEFAULT 'frequentist' CHECK (method IN ('frequentist')),  -- Bayesian deferred
    seed text NOT NULL,                       -- stable assignment salt (never changes once running)
    holdout_pct integer NOT NULL DEFAULT 0 CHECK (holdout_pct BETWEEN 0 AND 100),
    primary_goal jsonb,                       -- {event_type, filter?, value_field?, window}
    guardrail_goals jsonb NOT NULL DEFAULT '[]'::jsonb,
    winner_variant text,                      -- set by recommendation; rollout is separate + approved
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS experiments_tenant_idx ON experiments (tenant_id, workspace_id);

CREATE TABLE IF NOT EXISTS experiment_variants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    experiment_id uuid NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    label text NOT NULL,                      -- 'control','a','b',...
    weight integer NOT NULL CHECK (weight >= 0),
    is_control boolean NOT NULL DEFAULT false,
    template_id uuid REFERENCES templates(id),  -- variant's message template (nullable = use base)
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (experiment_id, label)
);

-- Authoritative deterministic assignment (also derivable, but stored for audit + fast joins).
CREATE TABLE IF NOT EXISTS experiment_assignments (
    experiment_id uuid NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    variant text NOT NULL,                    -- variant label OR 'holdout'
    assigned_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (experiment_id, profile_id)   -- stable, one assignment per subject
);
CREATE INDEX IF NOT EXISTS experiment_assignments_variant_idx
    ON experiment_assignments (experiment_id, variant);

ALTER TABLE api_keys ALTER COLUMN scopes SET DEFAULT ARRAY[
    'events:write','profiles:read','schemas:read','schemas:write',
    'api_keys:read','api_keys:write','privacy:write','operations:read','operations:write',
    'users:read','users:write','roles:read','roles:write',
    'segments:read','segments:write','templates:read','templates:write',
    'campaigns:read','campaigns:write','suppressions:read','suppressions:write',
    'journeys:read','journeys:write','journeys:publish',
    'experiments:read','experiments:write','reports:read'
];
```

### 2.2 `019_experiment_bindings.sql`
```sql
-- Campaigns can be driven by an experiment (which template/variant each recipient gets).
ALTER TABLE campaigns
    ADD COLUMN IF NOT EXISTS experiment_id uuid REFERENCES experiments(id);

-- Stamp variant/experiment on the disposition rows so reports group by variant.
ALTER TABLE delivery_attempts
    ADD COLUMN IF NOT EXISTS experiment_id uuid,
    ADD COLUMN IF NOT EXISTS variant text;
ALTER TABLE journey_message_intents
    ADD COLUMN IF NOT EXISTS experiment_id uuid,
    ADD COLUMN IF NOT EXISTS variant text;

-- 'holdout' is a new terminal disposition (recipient intentionally not sent).
ALTER TABLE delivery_attempts DROP CONSTRAINT IF EXISTS delivery_attempts_decision_check;
ALTER TABLE delivery_attempts ADD CONSTRAINT delivery_attempts_decision_check
    CHECK (decision IN ('sent','suppressed','no_consent','fatigued','render_failed',
                        'send_failed','failed','holdout'));

-- Give journey_message_intents.decision the CHECK it never had (M3 review gap) + holdout.
ALTER TABLE journey_message_intents ADD CONSTRAINT journey_message_intents_decision_check
    CHECK (decision IS NULL OR decision IN ('sent','suppressed','no_consent','fatigued',
        'render_failed','send_failed','failed','holdout','processing','provider_sent','retryable_failed'));
```

### 2.3 `020_analytics_facts.sql`
```sql
-- Engagement funnel facts (delivered/opened/clicked/bounced/complained), projected from events.
CREATE TABLE IF NOT EXISTS engagement_facts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    source_type text NOT NULL CHECK (source_type IN ('campaign','journey')),
    source_id uuid NOT NULL,                  -- campaign_id or journey_id
    node_id text,                             -- journey message node (null for campaigns)
    experiment_id uuid,
    variant text,
    profile_id uuid,                          -- resolved (workspace-scoped); may be null if unresolvable
    channel text,
    event_type text NOT NULL
        CHECK (event_type IN ('delivered','opened','clicked','bounced','complained')),
    occurred_at timestamptz NOT NULL,
    source_event_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_event_id, event_type)      -- idempotent projection
);
CREATE INDEX IF NOT EXISTS engagement_facts_report_idx
    ON engagement_facts (tenant_id, workspace_id, source_type, source_id, variant, event_type);

-- Attributed conversions (+ optional revenue), projected from goal events.
CREATE TABLE IF NOT EXISTS conversion_facts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    source_type text NOT NULL CHECK (source_type IN ('campaign','journey')),
    source_id uuid NOT NULL,
    experiment_id uuid,
    variant text,
    profile_id uuid NOT NULL,
    goal_name text NOT NULL,
    value numeric NOT NULL DEFAULT 0,         -- revenue metric (0 if none)
    occurred_at timestamptz NOT NULL,         -- when the goal event happened
    attributed_send_at timestamptz NOT NULL,  -- the send it was credited to
    source_event_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_event_id, goal_name)       -- idempotent projection
);
CREATE INDEX IF NOT EXISTS conversion_facts_report_idx
    ON conversion_facts (tenant_id, workspace_id, source_type, source_id, variant, goal_name);
```

---

## 3. Deterministic assignment & attribution (the two must-get-right algorithms)

### 3.1 Assignment (`internal/experiment/assign.go`)
```
Assign(seed, profileID string, variants []Variant, holdoutPct int) (variant string, holdout bool)
  bucket := binary.BigEndian.Uint64(sha256(seed + ":" + profileID)[:8]) % 10000
  if bucket < holdoutPct*100 -> return "holdout", true
  // remaining 10000 - holdoutPct*100 buckets split by variant weight (cumulative)
```
- Stable: same `(seed, profileID)` → same result forever (seed is frozen once an experiment is
  `running`). Reuse the exact bucketing from `internal/journey/nodes.go:266-278` — extract a
  shared `bucketOf(key string, mod uint64) uint64` helper both call.
- **Never** `math/rand`. Persist to `experiment_assignments` with `ON CONFLICT DO NOTHING`.

### 3.2 Attribution (projection-time, bounded lookup — NOT a scan)
When a goal event is projected (`ProjectEvent`, new case), for the event's `profile_id`:
- Look up the most recent send to that profile within the window: a **single indexed query**
  `SELECT ... FROM delivery_attempts WHERE tenant_id=$1 AND profile_id=$2 AND decision='sent'
   AND attempted_at BETWEEN $goalTime - window AND $goalTime ORDER BY attempted_at DESC LIMIT 1`
  (and the journey equivalent over `journey_message_intents`). Credit that send's
  `source_id`/`experiment_id`/`variant`.
- If a send is found, insert one `conversion_facts` row (`ON CONFLICT (source_event_id,
  goal_name) DO NOTHING`). If none, the goal event is unattributed → no row (still counted in
  raw analytics later if ClickHouse is added).
- The window comes from the **frozen goal definition** on the published version, not from
  current config — so re-projection is deterministic.

---

## 4. Exit-criteria traceability (this slice of `plan.md` Phase 4)

| Phase 4 element | How this plan meets it | Milestone |
|---|---|---|
| A/B + multivariate variants, control/holdout, allocation % | `experiments` + `experiment_variants` + weighted deterministic `Assign` | 9.1, 9.2 |
| Deterministic stable assignment | `sha256(seed:profile)`, frozen seed, stored assignments | 9.1 |
| Primary/secondary conversion events, windows, revenue, guardrails | goal defs frozen on the published version; `conversion_facts` with `value` | 9.3, 9.5 |
| Frequentist method documented; never mixed | `internal/experiment/stats.go` two-proportion z-test; `method` column pins it | 9.5 |
| Winner recommendation + separately-approved rollout | `winner_variant` recommendation; rollout behind the human-actor approval gate | 9.5 |
| Canonical funnel + deliverability/uplift reports, total & unique | Postgres `delivery_attempts` + `engagement_facts` + `conversion_facts` reports | 9.4, 9.5 |
| Near-real-time operational dashboards | React Reports view (SVG funnel/uplift) over the fact tables | 9.6 |

---

## 5. Implementation recipes (new; 6.1–6.16 from prior plans still apply)

### 6.17 Projected fact table (engagement / conversion)
- Migration: copy the `suppressions` table shape (`011_delivery_policy.sql`) — add
  `source_event_id uuid` + `UNIQUE(source_event_id, ...)` for idempotency.
- Projection: add a `case` to `ProjectEvent` (`store.go:408` switch) copying the
  `message.bounced → suppressions` block (`store.go:447-482`): decode the typed payload, do any
  bounded lookup, `INSERT ... ON CONFLICT DO NOTHING` inside the same `tx`. **Done when:**
  projecting the event twice creates exactly one fact row.

### 6.18 Read-only report query (Postgres, scan-free)
- Store method on `*postgres.Store` in new `internal/postgres/analytics.go`: a `GROUP BY` over
  a fact table filtered by `tenant_id`, `workspace_id`, and `source_id` (all indexed). Return a
  typed struct. Add to `ports.Store`. **Always parameterize; always tenant+workspace scope.**
- HTTP: copy a `campaigns.go` read handler; route `GET /v1/reports/...` with scope
  `reports:read`. **Done when:** the endpoint returns counts matching seeded facts.

### 6.19 Deterministic assignment
- `internal/experiment/assign.go` reusing `bucketOf` from `nodes.go`. Unit-test that the same
  input yields the same variant across 10k calls and process restarts. **Done when:** stability
  + weight-distribution tests pass.

### 6.20 Hand-rolled SVG chart (React)
- A small `<FunnelBars data={...} />` / `<UpliftTable/>` component rendering inline `<svg>`
  `<rect>`s with labels; follow the `dataviz` skill for palette/contrast; theme-aware. No new
  dependency. **Done when:** `npm run build` passes and the chart renders in light+dark.

---

## 6. Task list

Testing bar: unit + golden per milestone; one consolidated integration/determinism/load pass in
9.7. Each task ends with a **Done when**. Do them in order; compile + `go vet` between milestones.

### Milestone 9.0 — Fold-in hardening (Milestone 3 review fixes) — DO FIRST
1. [x] **Workspace/app isolation.** Add `workspace_id` scoping to `enrollEventTriggered`
   (`journey_runtime.go:670`) and `resolveWaitingRuns` (`journey_runtime.go:792`, also match `app_id`);
   `GetJourneyTransitions` (`journey_runtime.go:511`, join `journey_runs`, filter
   `workspace_id`); and `CompileClickHouseSingle`/`QueryClickHouseMatches`
   (`evaluate.go:123`, add workspace/app). *Done when:* a cross-workspace event no longer
   enrolls/resolves/reads another workspace's runs (add an integration test with two workspaces). — done: TestJourneyWorkspaceIsolation proves correct isolation scoping for event triggers, wait events, and transitions.
2. [x] **Poison-pill DLQ.** In `ClaimJourneyStep` (`journey_runtime.go:212`) add an attempts cap to
   the lease-reclaim branch (or dead-letter on reclaim past the threshold) so a panicking node
   eventually reaches the DLQ instead of looping forever. *Done when:* a step that crashes the
   worker N times is marked `dead` and surfaces in `/v1/journeys/dlq`. — done: ClaimJourneyStep transitions timed-out poison pills to dead and TestJourneyDLQPoisonPill verifies it surfaces in the DLQ.
3. [x] **`provider_sent` reconcile.** In `deliver.go:143`, the `provider_sent` reconcile branch must
   jump straight to `message.sent` emission — skip the quiet-hours + `policy.Evaluate`
   re-decision so an already-delivered message can't be flipped to `suppressed`/`fatigued` with
   its event dropped. Also ensure a repeatedly-failing emit dead-letters instead of sticking at
   `attempts>=3` forever. *Done when:* unit test — send succeeds, first emit fails, second pass
   emits `message.sent` even with a suppression added in between. — done: provider_sent reconciliation bypasses policy re-evaluation, and unit tests prove suppression cannot drop the event and repeated emission failure reaches dead-letter status.
4. [x] **`always` re-entry dedup.** Make `reentry_sequence` deterministic per firing (derive from
   the `entry_key`/time-slot, not the live run count) so a repeat with the same `entry_key`
   conflicts on the unique index. *Done when:* a scheduled `always` journey enrolls a profile
   exactly once per firing (test the busy-loop case). — done: scheduled `always` re-entry derives its sequence from the minute firing slot, and TestEnrollScheduledAlwaysOncePerFiring proves busy-loop retries enroll once while the next firing enrolls again.
5. [x] **Cheap correctness P2s (batch):** deterministic `app_id` in the journey `message.sent` key
   (`deliver.go:53` — `GetFirstAppID` needs an `ORDER BY`, no `"app-1"` fallback that changes
   the key); set `subject_external_id` on scheduled/backfill runs (`enroll.go:89,232`) so their
   `wait_event` nodes can resolve; validate `quiet_hours_start != quiet_hours_end`
   (`quiethours.go:53`); make the `action` node's profile mutation + `profile.updated` emit part
   of the transition tx (or document idempotent-at-least-once); fix the `AdvanceRunTx` ↔
   `resolveWaitingRuns` lock order to run→step both sides. *Done when:* each has a unit test. — done: ordered app lookup rejects synthetic fallback, scheduled/backfill runs retain external subjects, equal quiet-hour bounds are rejected, action replay uses a deterministic event key, and transitions lock run before step.
6. [x] **Journey `message.sent` contract.** Either add `campaign_id` (empty is invalid) — recommend
   relaxing `Event.Validate` `message.sent` to accept a journey send keyed by `journey_id` when
   `campaign_id` is absent (`domain.go:151`) — so journey sends are replayable and analyzable.
   *Done when:* a journey `message.sent` passes `Validate`. — done: Event validation accepts a journey_id in place of campaign_id, with positive journey-send and negative missing-source contract tests.
7. [x] **Add the missing negative test** for the publish/backfill human-actor gate
   (`server_test.go`): authenticate as `ActorType:"api_key"`, assert 403. Correct the two audit
   overstatements in `v1-milestone-3-audit.md`. *Done when:* the test fails if the gate is removed. — done: TestJourneyPublishAndBackfillRequireHumanActor proves API-key actors receive 403 without publishing or enrolling, and the audit now accurately describes transactional scope and one-step-at-a-time workers.

### Milestone 9.1 — Experiment definitions & deterministic assignment
1. [x] **Migration** `018_experiments.sql` per §2.1 (Recipe 6.1) + scopes
   `experiments:read/write`, `reports:read` in `rbac.go` allowlist and the default array
   (Recipe 6.5). *Done when:* tables exist; a fresh key carries the scopes. — done: `018_experiments.sql` applies with all three experiment tables, and TestExperimentMigrationAndDefaultScopesIntegration proves a fresh API key carries the experiment/report scopes.
2. [x] **Domain models** `Experiment`, `ExperimentVariant`, `ExperimentAssignment` (Recipe 6.2). — done: snake_case JSON domain structs mirror the experiment, variant, and assignment schema; full build/vet and scoped domain tests pass.
3. [x] **Store CRUD** `internal/postgres/experiments.go` + `ports.Store` (Recipe 6.3): create/get/
   list/update experiment (+ variants); `AssignExperiment(ctx, expID, profileID)` writing
   `experiment_assignments` `ON CONFLICT DO NOTHING`. Guard: the `seed` is immutable once
   `status='running'`. *Done when:* `go build ./...` passes. — done: transactional tenant/workspace-scoped CRUD and stable conflict-safe assignment are implemented; a live-Postgres integration test proves two-workspace isolation, assignment stability, and running-seed immutability, with full build/vet passing.
4. [x] **Assignment lib** `internal/experiment/assign.go` (Recipe 6.19) + the shared `bucketOf`
   helper (refactor `nodes.go` to use it). *Done when:* stability + distribution unit tests pass. — done: SHA-256 assignment is stable across 10k calls and restart-equivalent inputs; a 100k-subject test proves holdout and weighted distribution tolerance, and journey splits use the shared bucket helper unchanged.
5. [x] **HTTP + React scaffold** (Recipes 6.4, 6.8): experiments CRUD; a new `Experiments` section
   (add to `View` union, `viewTitles`, nav array, hash routing — App.tsx pattern). *Done when:*
   `npm run build` passes; create/list works. — done: scoped CRUD routes and an Experiments hash/nav view create and list experiments; focused handler/UI tests plus full build, vet, typecheck, frontend build, and frontend suite pass.
6. [x] **OpenAPI** entries for the new routes. *Done when:* redocly lints clean. — done: all four experiment CRUD routes and their experiment/variant schemas are documented; Redocly lint and the focused experiment endpoint test pass.

### Milestone 9.2 — Wire variants into campaigns & journeys
1. **Migration** `019_experiment_bindings.sql` per §2.2 (`campaigns.experiment_id`;
   `variant`/`experiment_id` on both disposition tables; `holdout` decision + the missing
   journey CHECK). *Done when:* columns/constraints exist.
2. **Campaign variant resolution.** In `internal/campaigns/deliver.go`, if the campaign has an
   `experiment_id`, per recipient call `experiment.Assign`, write the assignment, and choose the
   variant's `template_id` (fall back to the campaign template); stamp `variant`+`experiment_id`
   on the `delivery_attempts` row and the `message.sent` payload. `holdout` → record
   `decision='holdout'`, do **not** send. *Done when:* a 2-variant + 10% holdout campaign
   produces the expected split and no sends for holdout.
3. **Journey variant resolution.** The split node gains an optional `experiment_id` in its
   config → branch label = variant (record the assignment); a message node with an
   `experiment_id` selects the variant template and stamps `variant`+`experiment_id` on the
   intent + `message.sent`. Reuse the existing deterministic branch pick. *Done when:* a journey
   split bound to an experiment records assignments matching `experiment.Assign`.
4. **Telemetry**: `openjourney_experiment_assignments_total{variant}`. *Done when:* counter
   increments on assignment.

### Milestone 9.3 — Conversion goals & attribution
1. **Migration** `020_analytics_facts.sql` — the `conversion_facts` table (engagement table too,
   used in 9.4). Add goal storage: a `conversion_goal jsonb` + `attribution_window` on
   `campaigns`/`journey_versions` (frozen at publish/dispatch), and `experiments.primary_goal`.
   *Done when:* columns/tables exist.
2. **Goal freeze.** Persist the goal + window onto the immutable version at publish/dispatch
   (campaign: at `SaveCampaignManifestAndJobs`; journey: in the published `journey_versions`).
   *Done when:* the frozen goal is readable from the version, not live config.
3. **Attribution projection** (Recipe 6.17 + §3.2): a `ProjectEvent` case that, for an event
   matching a subject's active goal, does the bounded recent-send lookup and inserts a
   `conversion_facts` row idempotently, copying value from the payload's `value_field`.
   *Done when:* a goal event inside the window creates one attributed conversion; outside the
   window creates none; projecting twice creates one.

### Milestone 9.4 — Engagement facts + funnel/deliverability reports
1. **Engagement projection** (Recipe 6.17): `ProjectEvent` cases for `message.delivered`,
   `email.opened`, `link.clicked`, `message.bounced`, `message.complained` → `engagement_facts`,
   resolving `source_id`/`variant`/`experiment_id`/`profile_id` via a bounded lookup to
   `delivery_attempts`/`journey_message_intents`. *Done when:* projecting each event type creates
   one fact with the right variant.
2. **Reports service** `internal/postgres/analytics.go` (Recipe 6.18): `CampaignReport`,
   `JourneyReport` returning the funnel (targeted/sent/suppressed from `delivery_attempts`;
   delivered/opened/clicked from `engagement_facts`; converted from `conversion_facts`) as both
   **total** and **unique** (`COUNT(DISTINCT profile_id)`) with documented definitions, plus
   deliverability (bounce/complaint rate). Add to `ports.Store`. *Done when:* counts match
   seeded data.
3. **Wire ClickHouse-free reads**: no `SetClickHouse` needed — reads use `s.pool`. HTTP
   `GET /v1/reports/campaigns/{id}`, `GET /v1/reports/journeys/{id}` (scope `reports:read`).
   *Done when:* endpoints return correct JSON.

### Milestone 9.5 — Uplift, significance & winner recommendation
1. **Stats** `internal/experiment/stats.go`: two-proportion z-test → `{rate, uplift, z, pValue,
   ciLow, ciHigh}`; frequentist, documented. Unit-test against known textbook inputs. *Done
   when:* the math matches hand-computed values.
2. **Experiment report** `ExperimentReport` (per variant: sent, conversions, rate, uplift vs
   control, p-value, CI; guardrail rates). `GET /v1/reports/experiments/{id}`. *Done when:* a
   seeded experiment returns correct per-variant stats.
3. **Winner recommendation**: compute `winner_variant` when significance threshold met + no
   guardrail regression; store as a recommendation. A separate `POST /v1/experiments/{id}/rollout`
   requires the **human-actor approval gate** (reuse the journeys publish gate) and creates a new
   campaign/journey version pinned to the winning variant. *Done when:* rollout is rejected for a
   non-user actor and, for a user, produces a new version.

### Milestone 9.6 — Reports & Experiments UI
1. **Experiments view**: CRUD, variant editor (label/weight/control/template), holdout %, bind
   to a campaign or journey. *Done when:* `npm run build` passes; an experiment round-trips.
2. **Reports view** (Recipe 6.20): SVG funnel bars, a variant-comparison table (rate/uplift/
   p-value with a clear "not yet significant" state), deliverability tiles; a "Report" link on
   campaign/journey rows. Follow the `dataviz` skill. *Done when:* charts render in light+dark
   and match the API numbers.

### Milestone 9.7 — Integration, determinism, load & audit (closeout)
1. **Determinism tests**: same subject → same variant across re-runs and a simulated process
   restart; holdout excluded from sends; assignment distribution within tolerance of weights.
2. **Attribution tests**: goal in/out of window; revenue summed; duplicate goal event → one
   conversion fact (projection idempotency).
3. **Report-accuracy integration test** (DB-gated, copy `TestCampaignsEndToEnd`): seed a
   campaign + experiment + engagement + conversion events, drive projection, assert the funnel/
   uplift/deliverability numbers exactly.
4. **Load**: report-query latency over a large `engagement_facts`/`conversion_facts` set (assert
   indexed, sub-linear); projection throughput. *Done when:* documented within budget.
5. **Telemetry**: `openjourney_experiment_assignments_total`, `openjourney_conversions_attributed_total`.
6. **Run the suite**: `go build/vet/test ./...`, `go mod tidy`, `npm run build && npm audit`.
7. **Audit doc** `docs/milestones/v1-milestone-4-audit.md` in the M2/M3 table format, one row per
   requirement with direct evidence, incl. the 9.0 fixes.

---

## 7. Carry-over hazards & invariants

1. **Every fact projection must be idempotent** (`UNIQUE(source_event_id, ...)` + `ON CONFLICT
   DO NOTHING`) — the projection worker retries on failure and re-drives on reorder.
2. **Every report query must filter `tenant_id` AND `workspace_id`** and be parameterized — no
   `fmt.Sprintf` into SQL. Reports read `s.pool`, never a raw scan of `accepted_events`.
3. **Assignment seed is immutable once `running`** — changing it re-buckets everyone and
   destroys comparability. Enforce in `UpdateExperiment`.
4. **Attribution uses the frozen goal/window on the published version**, not live config, so
   re-projection is deterministic.
5. **No `math/rand`** anywhere in assignment (breaks stability + replay).
6. **Winner is advisory**; rollout is a separate, human-approved action that mints a new
   immutable version — AI/automation cannot self-roll-out (`product-decisions.md`).
7. **Do 9.0 first.** The isolation fixes must land before experiments add more per-workspace
   segmentation on top.

## 8. Open items to confirm before coding

- **Multi-touch vs last-touch attribution.** This plan specifies **last-touch within window**
  (simplest, deterministic). Confirm that's acceptable for v1; first-touch/multi-touch is a
  later enhancement.
- **Journey engagement linkage.** `delivered`/`opened`/`clicked` for journey sends must carry
  enough identity (journey_id/run_id/node_id, or a tracking token that does) to resolve
  `source_id`/`variant` at projection time. Confirm the M2/M3 tracking token already binds these
  for journey sends; if not, add it as a 9.4 sub-task.
- **ClickHouse later.** When ad-hoc high-cardinality reporting or raw event export is needed, add
  a ClickHouse-backed acceleration path behind the same `reports` API (events already flow to
  `behavior_events`); this milestone deliberately does not.
- **Bayesian option.** `method` is pinned to `frequentist`; a Bayesian method is a documented
  later addition (never mixed in one report).
