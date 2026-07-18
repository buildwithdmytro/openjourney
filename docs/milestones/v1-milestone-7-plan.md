# Phase 5 (final slice) Implementation Plan: Predictive Scoring, Realtime AI Decisioning & Online Optimization

Status: not started. Completes **Phase 5** (the governed AI layer) on top of Milestone 6
(AI gateway, prompt/model registry, typed tools, permission-aware retrieval + PII redaction,
immutable AI-activity audit, budgets, offline eval) — see
[`v1-milestone-6-plan.md`](./v1-milestone-6-plan.md) and its audit. Delivers `plan.md §6`
**Initial AI deliverables 5–6** plus the §6.3 online-optimization requirement:

1. **Predictive/propensity scores as versioned data artifacts**, score-triggered
   audiences/journeys.
2. **A bounded realtime AI decision node** in journeys — strict timeout, per-call budget, schema,
   and a **deterministic fallback** — now unblocked because M6 shipped the evaluation harness
   (`plan.md §6`: "Bounded channel/timing/content decisioning **only after evaluation
   infrastructure exists**").
3. **Online optimization** — read live experiment results and **propose** a reallocation / winner
   as a new immutable version for **human approval** (never self-reallocate).

Everything stays governed exactly as M6 established: AI **proposes**, a human **approves**; every
AI action is recorded; nothing bypasses consent, budgets, or the publish gate.

This is a **recipe book**, like the Phase 2–6 plans. Every task references a recipe and ends with
a **Done when** check. **If a task feels ambiguous, open the named existing file, copy it, rename,
and change the fields.** Recipes 6.1–6.30 from prior plans still apply verbatim; this plan adds
recipes 6.31–6.34.

> **Milestone 12.0 comes first and is non-negotiable.** It closes the two overstatements the M6
> review found: (a) a successful model call can skip its `allowed` audit row if the budget-increment
> write errors (`internal/ai/gateway.go:210-212,253-255`), and (b) `ai_activity` "immutability" is
> app-convention only — no DB enforcement. Fix these before building more AI decisioning on the
> audit trail.

## Design decisions (locked)

1. **Predictive scores are versioned data artifacts + a per-profile fact table.** A **scoring
   model** has immutable versions (mirror `prompt_versions` / `026_ai_registry.sql`:
   `status`, `eval_status`, `manifest_key` blob-frozen via the `journey/publish.go:40-54` freeze,
   `published_by`). Two kinds: **`expression`** (deterministic rules/regression over profile
   attributes + event history — no model call, evaluated in-process; the self-host-friendly
   default) and **`llm`** (reuses the M6 gateway + eval gate + a scoring `prompt_version` whose
   `output_schema` is a numeric score). Computed scores land in a new **`profile_scores`** table
   (NOT `profiles.attributes`), so they are versioned, auditable, and history-capable.
2. **Scores are computed in batch via the leased `operation_jobs` queue** (`scores.compute` job
   type — widen the CHECK per the `025_ai_gateway.sql:22-24` precedent), scoring a segment's
   profiles idempotently into `profile_scores`. Long-op → operation id + status resource, copying
   the `privacy_requests`/`ai_generation_requests` pattern.
3. **Score-triggered audiences/journeys reuse existing plumbing.** Add a **`Score`** audience-AST
   leaf (`{model, score_name, operator, value}`), a `parse.go` validator case, a `compile_pg.go`
   leg that reads `profile_scores` via a **parameterized subquery** (precedent: `CompileConsent`,
   `compile_pg.go:96` — never string-interpolate; keep the `fieldSafetyRegex` allowlist), and a
   `segments.go:199` resolve case. Then `ResolveSegment` → scheduled `EnrollJourneyRun`
   (`enroll.go:117`) fires with **zero** new enrollment code. Scores are equally usable in journey
   `condition`/`split` nodes (they already evaluate audience DSL).
4. **Bounded realtime AI decision node (`ai_decision`).** Today `ai_decision` is reserved and
   rejected (`internal/journey/nodes.go:132`). Its executor calls `Gateway.Generate` **inline
   under a hard `context.WithTimeout`** (from node config) and maps the validated output to exactly
   one declared branch label; on timeout / over-budget / provider error / schema-reject it takes a
   configured **deterministic fallback branch** (`experiment.BucketOf` for any tie-break). It
   **must not** rely on `FailJourneyStep` retries for model failures (those re-invoke ≤10× and can
   dead-letter a run, `journey_runtime.go:214-218`) — the run **always advances**. Every decision
   is recorded in `ai_activity` and stamped on the transition.
5. **Close the M6 gateway per-call gap.** `Gateway.Generate` has no per-call timeout or cost cap
   (only a 90s HTTP client timeout + a monthly workspace budget, `gateway.go:93-113`,
   `provider.go:164`). Add `Timeout` + `MaxCostCents` to `GenerateRequest`; the gateway enforces a
   hard per-call deadline (`context.WithTimeout`) and a per-call cost ceiling, returning a typed
   error the realtime node maps to its fallback.
6. **Online optimization proposes; humans approve.** A controller reads `ExperimentReport`
   (reusing `CompareProportions` + guardrail checks + `recommendWinner`, `analytics.go:284-393`)
   and, when a variant is significant with **no guardrail regression**, records a **proposal** to
   reallocate variant weights (or roll out a winner). Applying a proposal mints a **new immutable
   version** and requires the **human-actor gate** the M4 rollout uses (`experiments.go:72`); an
   `ai_agent`/`api_key` actor is rejected. **The experiment seed stays immutable** (weights change
   only via a new version, preserving comparability); **holdout is always preserved** (never sent
   to); **a guardrail regression halts** reallocation. No automated self-rollout
   (`product-decisions.md`).
7. **Governance & determinism carry over from M4/M6.** No `math/rand`; deterministic
   bucketing/assignment; the `expression` scorer is deterministic; tests use the `fake` provider +
   golden outputs. Every AI decision and score computation is recorded in the append-only
   `ai_activity`; reports stay scan-free; scopes go in three places; every new enum value is in a
   CHECK.

---

## 1. Architecture

```mermaid
flowchart TD
    subgraph Score[Predictive scoring — versioned artifacts]
        SM[scoring_model_versions: expression | llm\nimmutable, eval-gated, blob-frozen] --> JOB[scores.compute job\noperation_jobs leased queue]
        JOB -->|expression: in-process| PS[(profile_scores)]
        JOB -->|llm: M6 gateway + eval gate| PS
    end

    subgraph Trigger[Score-triggered targeting — reuses existing plumbing]
        PS --> DSL[audience DSL Score leaf → compile_pg subquery]
        DSL --> SEG[(segment resolve)]
        SEG --> ENR[scheduled EnrollJourneyRun / campaign dispatch]
    end

    subgraph RT[Bounded realtime AI decision node]
        STEP[journey step: ai_decision] -->|ctx WithTimeout + MaxCostCents| GW[Gateway.Generate\nactive+eval-passed prompt_version]
        GW -->|valid output → branch label| NEXT[next node]
        GW -->|timeout / over-budget / error / schema-reject| FB[deterministic fallback branch]
        STEP --> ACT[(ai_activity — append-only)]
    end

    subgraph Opt[Online optimization — propose, human approves]
        RPT[ExperimentReport: CompareProportions + guardrails] --> PROP[weight/winner proposal]
        PROP --> GATE{human-actor gate}
        GATE -->|approve| NV[(new immutable experiment version:\nreallocated weights, seed unchanged, holdout preserved)]
    end
```

**Reused unchanged:** the M6 gateway/registry/eval/`ai_activity`/redaction, the M4 experiment
stats (`CompareProportions`), holdout/assignment (`experiment.Assign`/`BucketOf`),
`ExperimentReport`/`recommendWinner`, the immutable-version blob freeze (`journey/publish.go`,
`blob`), the audience DSL (`Parse`/`CompileProfile`/`CompileConsent`), segment resolution +
scheduled enrollment, the journey step runtime (`TickNext`/`Execute`/`AdvanceRunTx`), the
`operation_jobs` leased queue + `internal/operations` worker + status-resource pattern, RBAC/scopes,
and telemetry.

### 1.1 Where each new thing lives

| Concern | Reuse | New build |
|---|---|---|
| Versioned scoring model | `prompt_versions` shape + `journey/publish.go` freeze | `scoring_models` + `scoring_model_versions` |
| Computed scores | `profiles` (join target) | `profile_scores` fact table |
| Batch scoring | `operation_jobs` + `internal/operations` + status resource | `scores.compute` job type + case |
| Score targeting | audience DSL + `segments.go` + enrollment | `Score` AST leaf + parse/compile/resolve cases |
| Realtime decision | journey node executor + M6 gateway | `ai_decision` node + gateway per-call timeout/budget |
| Online optimization | `ExperimentReport` + `recommendWinner` + rollout gate | proposal table + reallocation-as-new-version |
| AI audit | `ai_activity` | DB-level append-only + `policy_decision`/`action` CHECKs (12.0) |

---

## 2. Schema (new migrations)

Next numbers after `029_ai_eval.sql`. Conventions as always: `IF NOT EXISTS`, uuid PKs,
`timestamptz`, tenant/workspace FKs, CHECK-constrained enums enumerating **every** value the code
writes, idempotent projections.

### 2.1 `030_ai_activity_hardening.sql` (M6 fold-in)
```sql
-- Constrain the previously free-form decision/action strings (enumerate every value the code writes).
ALTER TABLE ai_activity ADD CONSTRAINT ai_activity_policy_decision_check
    CHECK (policy_decision IN ('allowed','denied_policy','denied_budget','denied_scope',
                               'denied_input','schema_reject','execution_error'));

-- Application-convention "immutable" → DB-enforced append-only.
CREATE OR REPLACE FUNCTION ai_activity_block_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'ai_activity is append-only';
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS ai_activity_no_update ON ai_activity;
CREATE TRIGGER ai_activity_no_update BEFORE UPDATE OR DELETE ON ai_activity
    FOR EACH ROW EXECUTE FUNCTION ai_activity_block_mutation();
```

### 2.2 `031_scoring.sql`
```sql
CREATE TABLE IF NOT EXISTS scoring_models (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    name text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('expression','llm')),
    current_version_id uuid,
    latest_version integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, name)
);

CREATE TABLE IF NOT EXISTS scoring_model_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    scoring_model_id uuid NOT NULL REFERENCES scoring_models(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    version integer NOT NULL,
    score_name text NOT NULL,                 -- e.g. 'purchase_propensity'
    definition jsonb NOT NULL,                -- expression: {expr, inputs}; llm: {prompt_version_id}
    output_min numeric NOT NULL DEFAULT 0,
    output_max numeric NOT NULL DEFAULT 1,
    manifest_key text NOT NULL,               -- content-addressed frozen blob
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
    eval_status text NOT NULL DEFAULT 'pending' CHECK (eval_status IN ('pending','passed','failed')),
    published_by uuid,
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (scoring_model_id, version)
);

-- Per-profile computed scores (latest per model+score_name). Queryable by the audience compiler.
CREATE TABLE IF NOT EXISTS profile_scores (
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    app_id uuid NOT NULL,
    profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    scoring_model_id uuid NOT NULL REFERENCES scoring_models(id) ON DELETE CASCADE,
    score_name text NOT NULL,
    value numeric NOT NULL,
    model_version integer NOT NULL,           -- which immutable version produced it
    computed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, profile_id, scoring_model_id, score_name)
);
CREATE INDEX IF NOT EXISTS profile_scores_query_idx
    ON profile_scores (tenant_id, workspace_id, scoring_model_id, score_name, value);

ALTER TABLE operation_jobs DROP CONSTRAINT IF EXISTS operation_jobs_job_type_check;
ALTER TABLE operation_jobs ADD CONSTRAINT operation_jobs_job_type_check
    CHECK (job_type IN ('privacy.export','privacy.delete','profiles.replay','retention.enforce',
                        'ai.generate','scores.compute'));

ALTER TABLE api_keys ALTER COLUMN scopes SET DEFAULT ARRAY[
    'events:write','profiles:read','schemas:read','schemas:write',
    'api_keys:read','api_keys:write','privacy:write','operations:read','operations:write',
    'users:read','users:write','roles:read','roles:write',
    'segments:read','segments:write','templates:read','templates:write',
    'campaigns:read','campaigns:write','suppressions:read','suppressions:write',
    'journeys:read','journeys:write','journeys:publish',
    'experiments:read','experiments:write','reports:read',
    'device_tokens:read','device_tokens:write',
    'ai:read','ai:configure','ai:invoke','prompts:read','prompts:write',
    'scoring:read','scoring:write','scoring:compute'
];
```

### 2.3 `032_online_optimization.sql`
```sql
-- A proposal is advisory until a human approves it into a new immutable version.
CREATE TABLE IF NOT EXISTS optimization_proposals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    experiment_id uuid NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('reallocate','winner')),
    report_snapshot jsonb NOT NULL,           -- the ExperimentReport that justified it
    proposed_weights jsonb,                    -- reallocate: {variant: weight}
    winner_variant text,                       -- winner
    rationale text NOT NULL,
    status text NOT NULL DEFAULT 'proposed'
        CHECK (status IN ('proposed','approved','rejected','superseded')),
    approved_by uuid,                          -- human approver; NULL until approved
    approved_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS optimization_proposals_idx
    ON optimization_proposals (tenant_id, workspace_id, experiment_id, status);
```

---

## 3. The seams to get right

### 3.1 Bounded realtime decision (`internal/journey/nodes.go` new `ai_decision` case)
```
Execute(...) for ai_decision:
  cfg := AIDecisionConfig{PromptVersionID, TimeoutMS, MaxCostCents, Branches[], Fallback}
  dctx, cancel := context.WithTimeout(ctx, cfg.Timeout); defer cancel()
  out, err := gateway.Generate(dctx, aiPrincipal(run), GenerateRequest{
      PromptVersionID: cfg.PromptVersionID, Timeout: cfg.Timeout, MaxCostCents: cfg.MaxCostCents,
      OutputSchema: branchEnumSchema(cfg.Branches), DomainValidator: mustBeDeclaredBranch(cfg.Branches),
      RetrievedData: retrieveForDecision(run), Purpose: "journey_decision"})
  branch := cfg.Fallback
  if err == nil && isDeclared(out.branch, cfg.Branches) { branch = out.branch }   // else deterministic fallback
  return ExecutionResult{ NextNodeID: findNextNode(graph, n.ID, branch), Transition: {outcome: branch, ai_activity_id: out.ActivityID} }
```
- **Never** return an error for a model timeout/over-budget — that would trigger `FailJourneyStep`
  retries. The run advances via the fallback branch; the ai_activity row records `denied_budget`/
  `schema_reject`/`execution_error`.

### 3.2 Score condition (`internal/audience`)
- `ast.go`: `Score{Model, ScoreName, Operator, Value}` (`Type()="score"`). `parse.go`: a `case
  "score"` validating operator ∈ {greater_than,less_than,equals} and non-empty model/score_name.
  `compile_pg.go`: emit a **parameterized** `profile_id IN (SELECT profile_id FROM profile_scores
  WHERE tenant_id=$n AND scoring_model_id=$n AND score_name=$n AND value <op> $n)` (bind everything;
  keep `fieldSafetyRegex` for any identifier). `segments.go:199`: a `case *audience.Score` mirroring
  the `ProfileAttribute` path.

### 3.3 Online-optimization proposal (`internal/experiment` + a controller)
- Read `ExperimentReport`; if `recommendWinner` yields a winner with no guardrail regression, write
  an `optimization_proposals` row (`proposed`). **Applying** it (`POST
  /v1/experiments/{id}/optimize/{proposalId}/approve`) reuses the human-actor gate and mints a new
  immutable experiment version with reallocated weights (seed unchanged, holdout preserved).

---

## 4. Exit-criteria traceability (`plan.md §6` deliverables 5–6 + §6.3, §14 Phase 5 close)

| Element | How this plan meets it | Milestone |
|---|---|---|
| Recommendations/predictive scores as versioned data artifacts | `scoring_model_versions` (immutable, eval-gated, blob) + `profile_scores` | 12.2–12.5 |
| Score-triggered audiences/journeys | `Score` DSL leaf → segment resolve → scheduled enrollment | 12.6 |
| Bounded realtime decisioning w/ deterministic fallback (only after eval infra) | `ai_decision` node + gateway per-call timeout/budget | 12.1, 12.7 |
| Online optimization uses holdouts + guardrails | `ExperimentReport` + guardrail gate → proposal → human-approved new version | 12.8, 12.9 |
| AI never self-rolls-out; human approval | reuse the M4 rollout gate; seed immutable; holdout preserved | 12.9 |
| Every AI action recorded; immutable audit | 12.0 hardening + `ai_activity` on every decision/score | 12.0, 12.7 |

---

## 5. Implementation recipes (new; 6.1–6.30 from prior plans still apply)

### 6.31 Versioned scoring-model artifact
- Copy the M6 registry (`prompts`/`prompt_versions`) + the `journey/publish.go` freeze: canonicalize
  the version, `sha256`, `blob.Put`, insert the immutable row, flip `current_version_id`. Publish
  requires the human gate + `eval_status='passed'`. **Done when:** publish is idempotent; api_key →
  403; an unevaluated version can't be used by a compute job.

### 6.32 Score-based audience condition
- Add the `Score` AST leaf + `parse` case + `compile_pg` **parameterized** subquery on
  `profile_scores` + `segments.go` resolve case. **Done when:** a segment with a score condition
  resolves to exactly the profiles whose latest score satisfies the operator, and the SQL is fully
  parameterized (no interpolated values).

### 6.33 Bounded realtime AI node
- New `ai_decision` node (§3.1): inline `Gateway.Generate` under `context.WithTimeout` + per-call
  budget; validated output → declared branch; timeout/error/over-budget → deterministic fallback;
  record `ai_activity`; publish-time validation in `validateOutgoing`/`validateDurations`. **Done
  when:** a model that times out still advances the run to the fallback branch (test with a slow
  fake provider), and the run never dead-letters on model failure.

### 6.34 Online-optimization proposal → approved new version
- Controller reads `ExperimentReport`, applies the significance + guardrail gate, writes a proposal;
  an approve endpoint (human-actor gate) mints a new immutable experiment version with reallocated
  weights (seed unchanged, holdout preserved). **Done when:** a proposal is advisory until approved,
  api_key approval → 403, and approval produces a new version whose seed equals the old one.

---

## 6. Task list

Testing bar: unit + golden per milestone; the fake provider + deterministic expression scorer make
everything reproducible; one consolidated integration/governance/determinism pass in 12.11. Each
task ends with a **Done when**. Do them in order; compile + `go vet` between milestones.

### Milestone 12.0 — Fold-in hardening (M6 review fix) — DO FIRST
1. [x] **Audit-on-egress ordering.** Fix `internal/ai/gateway.go:210-212,253-255` so a successful
   provider call **always** records its `allowed` `ai_activity` row even if the budget-increment
   write errors (record activity, then best-effort increment; never return before logging). *Done
   when:* a unit test with a failing budget-increment still writes exactly one `allowed` activity row. — done: recorded activity before calling best-effort budget increments, and added TestGatewayBudgetIncrementFailureDoesNotBlockAllowedActivity.
2. [x] **DB-level append-only audit** + decision CHECK — migration `030_ai_activity_hardening.sql` per
   §2.1 (trigger blocks UPDATE/DELETE; `policy_decision` CHECK enumerates all written values). *Done
   when:* an `UPDATE ai_activity` raises; every `policy_decision` the code writes passes the CHECK. — done: added migration 030_ai_activity_hardening.sql and TestAIActivityHardening_12_0_2 to verify constraint & append-only trigger.
3. [x] **Correct the M6 audit doc** (`v1-milestone-6-audit.md`): state that `ai_activity` immutability
   is now DB-enforced and the egress-audit ordering is fixed. *Done when:* the audit matches the code. — done: updated v1-milestone-6-audit.md to document DB-enforced append-only audit and ordering fix.

### Milestone 12.1 — Gateway per-call timeout + budget
1. [x] **Per-call bounds.** Add `Timeout time.Duration` + `MaxCostCents int64` to
   `ai.GenerateRequest`; the gateway wraps the provider call in `context.WithTimeout` and denies
   pre-call if the estimated/actual cost would exceed `MaxCostCents`, returning typed errors
   (`ErrCallTimeout`, `ErrCallBudgetExceeded`) plus the recorded `ai_activity`. *Done when:* a slow
   fake provider trips `ErrCallTimeout`; an over-cap call trips `ErrCallBudgetExceeded`; both log
   activity. — done: implemented pre-call and post-call budget/timeout checks in Gateway.Generate and verified with TestGatewayPerCallBounds.

### Milestone 12.2 — Scoring-model registry (versioned artifact)
1. [x] **Migration** `031_scoring.sql` (scoring tables + `scores.compute` job type + scopes
   `scoring:read/write/compute`) per §2.2 + rbac allowlist + `api_keys` default. *Done when:* tables
   exist; a fresh key carries the scopes. — done: created 031_scoring.sql migration, updated rbac.go allowedPermissions, and verified tables/scopes via TestScoringMigrationAndDefaultScopesIntegration_12_2_1.
2. [x] **Registry store + freeze** (Recipe 6.31) `internal/postgres/scoring.go` + `ports.Store`:
   `scoring_models`/`scoring_model_versions` CRUD; publish freezes to blob + immutable row behind the
   human gate; refuses `eval_status != 'passed'`. *Done when:* publish idempotent, api_key → 403. — done: implemented scoring_models and scoring_model_versions CRUD operations, blob freeze and publish with human gate, verified with TestScoringRegistry.

### Milestone 12.3 — Expression scorer (deterministic, non-LLM)
1. [x] **Evaluator** `internal/scoring/expression.go`: evaluate a safe expression over a profile's
   attributes + bounded event aggregates → a numeric score clamped to `[output_min, output_max]`;
   pure/deterministic (no `math/rand`, no clock beyond an injected one). *Done when:* the same
   profile+version always yields the same score (golden test). — done: implemented deterministic Go-AST-based expression parser & evaluator and verified with golden & determinism tests.
2. **Eval gate for expression models**: a deterministic eval run (golden cases) flips
   `eval_status='passed'`. *Done when:* an expression version can't be computed until eval passes.

### Milestone 12.4 — LLM scorer (reuse M6 gateway + eval)
1. **LLM scorer** `internal/scoring/llm.go`: for a `kind='llm'` model, call `Gateway.Generate` with
   the pinned scoring `prompt_version` (output_schema = numeric score), reuse the M6 eval gate.
   *Done when:* an eval-passed LLM scoring version produces a schema-valid numeric score via the fake
   provider; an unevaluated one is refused.

### Milestone 12.5 — profile_scores + batch scoring job
1. **Batch scoring job**: `scores.compute` enqueue (operation id + status resource, copy
   `ai_generation_requests`) + a worker `case "scores.compute"` in `internal/operations` that
   resolves a segment and writes `profile_scores` idempotently (upsert on the PK). Telemetry
   `openjourney_scores_computed_total`. *Done when:* scoring a segment twice yields one row per
   (profile, model, score_name) with the latest value; the status resource reaches `complete`.

### Milestone 12.6 — Score condition in the audience DSL
1. **Score leaf** (Recipe 6.32): `Score` AST node + `parse` case + `compile_pg` parameterized
   subquery on `profile_scores` + `segments.go` resolve case. *Done when:* a segment with a score
   condition resolves to the right profiles and the compiled SQL binds every value (no interpolation);
   a score-triggered scheduled journey enrolls them with no new enrollment code.

### Milestone 12.7 — Bounded realtime AI decision node
1. **`ai_decision` node** (Recipe 6.33): config struct + executor calling `Gateway.Generate` inline
   under `context.WithTimeout` + `MaxCostCents`; validated output → declared branch; timeout / error
   / over-budget / schema-reject → deterministic fallback branch; record `ai_activity` + stamp the
   transition. Remove `ai_decision` from the rejected set (`nodes.go:132`). *Done when:* a slow fake
   provider still advances the run to the fallback branch; a valid output takes the model's branch.
2. **Publish-time validation**: `validateOutgoing` case (branch labels = config branches + the
   fallback must be among them) + `validateDurations` (bounded timeout); require a pinned
   `prompt_version_id`. *Done when:* a graph with an `ai_decision` missing its fallback branch or a
   pinned prompt fails `Validate`.
3. **Determinism test**: same inputs + timed-out model → same fallback branch across runs; the run
   never dead-letters on repeated model failure. *Done when:* asserted.

### Milestone 12.8 — Online-optimization controller
1. **Migration** `032_online_optimization.sql` per §2.3. *Done when:* table exists.
2. **Proposal generation** (Recipe 6.34): a controller reads `ExperimentReport`
   (`CompareProportions` + guardrail regression gate) and writes an `optimization_proposals` row
   when a variant is significant with no guardrail regression; **no** change to live assignment.
   *Done when:* a seeded significant experiment yields a `proposed` row; a guardrail-regressing one
   yields none.

### Milestone 12.9 — Human-approved reallocation / rollout
1. **Approve endpoint** `POST /v1/experiments/{id}/optimize/{proposalId}/approve` (human-actor
   gate): mint a new immutable experiment version with the proposed weights (seed unchanged, holdout
   preserved) or roll out the winner; mark the proposal `approved` with the approver. *Done when:*
   an api_key actor → 403; a user actor produces a new version whose seed equals the original and
   whose holdout_pct is preserved.

### Milestone 12.10 — UI
1. **Scoring + scores**: scoring-model editor (expression/LLM, publish), a per-profile scores
   inspector, and a **score condition** in the segment builder. *Done when:* `npm run build` passes;
   a scoring model and a score-triggered segment round-trip.
2. **Realtime node + optimization**: an `ai_decision` node in the journey builder (with a required
   fallback branch) and an online-optimization proposals review/approve panel. *Done when:* the node
   round-trips with its fallback; a proposal can be approved from the UI (human actor).

### Milestone 12.11 — Integration, determinism, governance & audit closeout
1. **Score-triggered E2E** (DB-gated): compute scores for a segment, resolve a score-conditioned
   segment, enroll a scheduled journey; assert the enrolled set. *Done when:* counts match.
2. **Realtime-node determinism**: model timeout → deterministic fallback; run advances, never
   dead-letters; every decision logged in `ai_activity`. *Done when:* asserted.
3. **Governance**: online-optimization approval requires a human actor (api_key → 403); seed
   immutable across reallocation; holdout never sent to; append-only `ai_activity` (UPDATE raises).
   *Done when:* all asserted.
4. **Run the suite**: `go build/vet/test ./...`, `go mod tidy`, `cd web && npm run typecheck &&
   npm run build && npm test`, `npm audit`. *Done when:* green.
5. **Audit doc** `docs/milestones/v1-milestone-7-audit.md` in the M2–M6 table format, one row per
   requirement (12.0–12.11) with direct evidence. *Done when:* every row cites a named file/test.

---

## 7. Carry-over hazards & invariants

1. **Do 12.0 first.** The M6 audit gaps (egress-audit ordering + non-enforced immutability) must
   close before more AI decisioning rides on the audit trail.
2. **Realtime AI node fails to a deterministic branch, never to a retry.** A model timeout /
   over-budget / error / schema-reject returns a normal `ExecutionResult` on the configured fallback
   branch — never an error that triggers `FailJourneyStep` (which re-invokes the model and can
   dead-letter the run). The run always advances; the timeout is far tighter than the 5-minute step
   lease.
3. **AI decisioning is bounded + governed.** Pinned `prompt_version` must be `active` +
   `eval_status='passed'`; enforce a per-call timeout **and** per-call cost cap; output must map to a
   declared branch or fall back; record every decision in `ai_activity`.
4. **Scores are versioned + eval-gated + human-published.** A scoring-model version is immutable;
   publishing requires the human gate; a compute job refuses a non-`passed` version. `profile_scores`
   records which `model_version` produced each value.
5. **Score DSL leg is parameterized + scoped.** Never interpolate values/identifiers into SQL; keep
   the `fieldSafetyRegex` allowlist; filter `tenant_id` + `workspace_id`.
6. **Online optimization proposes; humans approve.** Never self-reallocate/self-rollout. Reallocate
   via a **new immutable version** (seed unchanged, holdout preserved); a guardrail regression halts;
   the approve path reuses the human-actor gate (api_key/ai_agent → 403).
7. **Determinism.** No `math/rand`; deterministic bucketing/assignment; the expression scorer is
   pure; tests use the fake provider + golden outputs; no test depends on a live model.
8. **Audit + scan-free.** Every AI decision and score computation logs to append-only `ai_activity`;
   reports/segment resolution add no full-population OLTP scans (score reads hit the indexed
   `profile_scores`).
9. **Scopes in three places** (rbac allowlist, `api_keys` default in the new migration, routes);
   widen `operation_jobs.job_type`; enumerate every new enum value in a CHECK.
10. **Reuse existing seams** — gateway, eval, blob freeze, `CompareProportions`/`Assign`,
    `audience.Parse`/compile, `operation_jobs`, the journey runtime — don't reinvent them.

## 8. Open items to confirm before coding

- **Score storage.** This plan uses a dedicated `profile_scores` table (versioned, history-capable)
  rather than `profiles.attributes`. Confirm; and whether to keep score history or latest-only
  (plan keeps latest via the PK — add a `profile_score_history` table only if trends are needed).
- **Expression language.** The `expression` scorer uses a small safe expression over attributes +
  bounded event aggregates. Confirm the expression surface (a restricted arithmetic/logical grammar)
  vs. a fuller rules engine — recommend the restricted grammar for determinism + safety.
- **Realtime node bounds.** Confirm the per-call timeout ceiling (e.g. ≤ a few seconds) and default
  `MaxCostCents`, so a decision node can never stall the durable runtime or blow the budget.
- **Online optimization cadence.** The controller runs on a schedule (reuse the scheduled-enrollment
  cron pattern) or on-demand. Confirm cadence and that it only ever writes proposals.
- **Embeddings/vector retrieval** remains deferred (as in M6); the `llm` scorer uses structured
  generation, not semantic retrieval. Confirm.
