# Milestone 13 — Ralph-loop prompt

**What this is:** an autonomous single-task loop prompt for implementing OpenJourney
**Milestone 13 — Feature Flags** (environment-scoped flags, deterministic rollout + stable bucketing,
segment/rule targeting, kill switch, exposure events, a public evaluation edge, and the browser-SDK
contract) — `docs/milestones/v1-milestone-13-plan.md`, tasks 18.0–18.10. Each run does exactly ONE task,
verifies it, records progress in the plan file, commits, and stops. Run it repeatedly (fresh context each
time) until it prints `MILESTONE 13 COMPLETE` or writes a new line to `docs/milestones/BLOCKERS.md`.

**Recommended runner:** use the repository CLI to start a fresh Codex or Antigravity process for
each task. The primary provider gets one attempt; if it fails before committing or recording a
blocker, the other provider gets one recovery attempt against the same working tree.

```bash
go run ./cmd/ralph --primary codex --unsafe-autonomous
go run ./cmd/ralph --primary antigravity --unsafe-autonomous
go run ./cmd/ralph --primary claude --unsafe-autonomous          # cheapest: defaults to Haiku
```

`--primary` accepts `codex`, `antigravity`, or `claude`. Claude runs with `--output-format stream-json
--verbose` so its progress streams live. For the cheapest runs use `--primary claude` (defaults to the
`haiku` model; override with `--claude-model sonnet|opus|<id>`).

The defaults now target Milestone 13 (`--plan docs/milestones/v1-milestone-13-plan.md`,
`--branch phase13`, `--milestone 13`). Use `--dry-run` to validate the repository, prompt, task scan,
CLIs, and configured models without switching branches or invoking an agent.

**Manual use:** alternatively, paste the block below as the agent mission and let it run on the
`phase13` branch; re-trigger the same mission for each iteration. Keep auto-run enabled for the verify
commands in STEP 3.

---

```text
You are an autonomous coding agent implementing OpenJourney **Milestone 13 — Feature Flags**
(environment-scoped flags with typed variants, DETERMINISTIC percentage rollout + stable bucketing,
segment/rule TARGETING, a KILL SWITCH, EXPOSURE EVENTS, a PUBLIC evaluation edge, and a browser-SDK
evaluation contract), strictly following `docs/milestones/v1-milestone-13-plan.md`. This is ONE iteration
of a loop: do **exactly ONE task**, verify it, record it, commit, then STOP. A fresh agent runs next
iteration with NO memory of this one — all state must be on disk (the plan's checkboxes + git). Do not
try to do the whole milestone in one run.

## STEP 1 — Orient (every iteration, in this order)
1. Read `docs/milestones/v1-milestone-13-plan.md` IN FULL.
2. Re-read §"Design decisions (locked)" and §7 "Carry-over hazards & invariants". These are INVARIANTS
   you may not violate.
3. Skim the recipes it cites — this plan's new recipes 6.68–6.75. This milestone REUSES far more than it
   builds: the experiment bucketing engine (`internal/experiment/assign.go` — `BucketOf`/`Assign`), the
   audience-DSL targeting engine (`Store.EvaluateAudience`, `internal/postgres/journey_runtime.go:544`),
   the journeys publish/freeze/version pattern (`internal/postgres/journeys.go:109`), the kill-switch
   check (`internal/extension/host.go:121`), the human-actor gate (`isHuman`, `internal/httpapi/
   identity.go:85`), the PUBLIC SDK edge (`internal/httpapi/messages.go:18` `fetchInbox` — INCLUDING the
   `byExternalID` column-pin IDOR fix), the event pipeline (`Store.AcceptEvents` `store.go:210` +
   `ProjectEvent` `store.go:456`), the JS SDK (`sdk/javascript/src/index.ts`), and the M12 component
   library (`web/src/components/`).
4. Run `git log --oneline -15` and `git status` to see what already exists on disk.
5. A task is DONE only if **its own** numbered line/block literally contains `[x]` or a `— done:` note.
   Check EACH task individually. Work the **single first unchecked task in document order** and nothing
   else.
   - **NEVER skip a task or jump ahead.** Tasks run strictly 18.0 → 18.10 in order.
   - **If the first unchecked task looks already-implemented:** do NOT skip it. VERIFY its literal *Done
     when* condition. If it holds, mark it `[x]` + `— done: <evidence>` and commit that doc change. If
     not, implement it.
   - **If you cannot complete the first unchecked task:** append a line to
     `docs/milestones/BLOCKERS.md` and commit only that. Do NOT route around it.
   - **Why this is strict:** the runner independently tracks the first-unchecked task. If you commit work
     for a *different* task, the runner halts with `changed Git history ambiguously` (exit 4).
   **18.0 (the M12 UX closeout) and 18.1 (flag foundation: schema + store + scopes) must land first** —
   no flag evaluates before the governed, versioned foundation + kill switch + human gate exist.

## STEP 2 — Do exactly ONE task
- Implement only that single sub-task. Follow its referenced Recipe: open the named existing file, copy
  it, rename, change the fields. DO NOT design from scratch — everything has a cited file:line.
- Honor these invariants every time they apply:
  - **Deterministic bucketing — reuse `internal/experiment/assign.go`, do NOT write a new hash.**
    `BucketOf(key, mod)` (SHA-256 → uint64 % mod) for rollout gating; `Assign(seed, subject, variants,
    holdoutPct)` for weighted variants. Same `(seed, subject)` → same variant ALWAYS. **NO `math/rand`,
    NO wall-clock, NO time-based assignment** in any rollout/variant decision.
  - **Targeting reuses the audience DSL — do NOT write a new eval engine.** A flag's targeting is an
    ordered `[{dsl, variant}]`; evaluate each via `store.EvaluateAudience(ctx, p, profileID, rule.dsl)`
    (`journey_runtime.go:544`, the same primitive journey condition nodes use at `nodes.go:283`); first
    match wins, else rollout bucket, else default.
  - **The evaluate edge is PUBLIC and IDOR-safe — copy `fetchInbox` (`messages.go:18`) EXACTLY.** Register
    in the PUBLIC mux block (`server.go:205`), NOT `s.authenticate`. Rate-limit via `s.publicLimiter`/
    `ClientIP`. Resolve the subject with the **`byExternalID` column-pin** `GetProfileIDBySubject(ctx,
    tenant, app, subject, byExternalID)` (`messages.go:87`, impl `postgres/messages.go:101`): anonymous
    callers match `anonymous_id` ONLY, known subjects require a valid `SignInAppToken` and match
    `external_id` ONLY. **NEVER** the `external_id OR anonymous_id` match — that was the CRITICAL IDOR
    fixed in `bd12506`; do not reintroduce it.
  - **A flag is governed like a journey.** Publish/enable/kill-switch are human-actor-gated (`isHuman(p)`,
    `identity.go:85` → 403 `human_approval_required`); published versions are immutable (blob freeze +
    `sha256` digest guard + a `BEFORE UPDATE OR DELETE` trigger + REVOKE, copy `PublishJourney`
    `journeys.go:109-174` + `034_experiment_versions.sql:20-28`). The kill switch (`status='disabled'`,
    copy `host.go:121`) short-circuits evaluation to the DEFAULT value — never a partial rollout.
  - **Exposure is event-sourced; the projector is the only writer.** Evaluation emits a
    `feature_flag.exposure` event via `AcceptEvents` (`store.go:210`, precedent `messages.go:255-276`);
    the `ProjectEvent` case (`store.go:456`) is the ONLY writer of `feature_flag_exposures`. No HTTP
    handler writes exposure state directly. `accepted_events` has no `event_type` CHECK, so the new type
    needs only a projector case. Exposures are idempotency-keyed.
  - **Environment is a validated column, not a table (v1).** `environment text CHECK IN ('development',
    'staging','production')`, `UNIQUE(tenant_id, app_id, environment, key)`. Reject an unknown environment.
  - **Governance wiring.** New scopes `flags:read`/`flags:write` → add in ALL THREE places (`rbac.go`
    allowlist, the `api_keys.scopes` DEFAULT array RE-DECLARED in the new migration copying the current
    array from `048_inapp_messaging.sql:68`, the `s.authenticate("flags:...", ...)` routes). Every
    `flag_type`/`environment`/`status` value the code writes appears in a CHECK.
  - **NO new dependency.** `go mod tidy`, `web/package.json`, `web/package-lock.json`, and
    `sdk/javascript/package.json` MUST be unchanged. Bucketing = `assign.go`, targeting = audience DSL,
    UI = the M12 `web/src/components/` primitives (do NOT hand-roll buttons/inputs/modals — use them). A
    task that seems to need a library is built from existing primitives or is out of scope as written.
  - New migration = next zero-padded number on disk (currently `050`), `IF NOT EXISTS`, uuid PK,
    timestamptz. New frontend follows the 6-point `App.tsx` registration + `requestJSON` wrappers + the
    M12 primitives; theme-aware; co-located vitest test.

## STEP 3 — Verify (MANDATORY — do not mark a task done if any check fails)
- Go changes: `go build ./... && go vet ./... && go test ./...` (or the touched package). Run
  `go mod tidy` and confirm `git diff go.mod go.sum` shows NO new dependency.
- Migration: confirm it applies cleanly, the CHECKs accept every value the code writes and reject an
  unknown one, and `feature_flag_versions` rejects UPDATE/DELETE.
- Web changes: `cd web && npm run typecheck && npm run build && npm test`.
- SDK changes: `cd sdk/javascript && npm run build && npm test`.
- The task's literal **"Done when"** condition MUST be observably satisfied. For the load-bearing
  properties (deterministic-stable-bucketing, targeting-first-match, IDOR-safe-evaluate-edge,
  human-gated-immutable-publish, kill-switch-returns-default, exposure-projector-only-and-idempotent,
  no-new-dep) that means an actual test proving the property — not just that it compiles.

## STEP 4 — Record & commit
- Edit `docs/milestones/v1-milestone-13-plan.md`: prefix the finished sub-task with `[x]` and append
  `— done: <one-line evidence>` (match the prior-plan style).
- Ensure you are on branch `phase13` (create it from the current branch if it doesn't exist). NEVER
  commit to `main`.
- Commit ONLY this task's changes with a conventional message, e.g.
  `feat(flags): 18.2 add deterministic evaluation engine reusing experiment bucketing`
  or `feat(flags): 18.5 add public IDOR-safe evaluation edge`. Follow the repository's commit-trailer
  convention.

## STEP 5 — Stop
- After committing ONE task, output a 2-line summary (task id + what you did) and STOP.
- If ALL tasks are `[x]`, output `MILESTONE 13 COMPLETE` and STOP.
- If BLOCKED (real ambiguity, a failing build/test you can't fix within this task's scope, a missing
  prerequisite, a needed human approval, or anything that would require weakening an invariant — e.g. a
  new dependency, a non-deterministic (rand/wall-clock) rollout, the `external_id OR anonymous_id` IDOR
  match on the evaluate edge, an exposure write outside `ProjectEvent`, a non-human publish, a mutable
  flag version, or a hand-rolled UI control instead of the M12 primitives): do NOT hack around it, do NOT
  delete/disable a failing test, and do NOT mark the task done. Append the blocker to
  `docs/milestones/BLOCKERS.md` (task id + what's blocking + what you need), commit that file only, and STOP.
```

---

**Runner:** loop this mission with a **fresh context each iteration**, capped at some max, and stop when
the output contains `MILESTONE 13 COMPLETE` or a new line lands in `docs/milestones/BLOCKERS.md`.

**Note:** the M13 tasks are `[ ]` checkboxes. The runner treats "no `[x]` / no `— done:`" as TODO. Work
the first unchecked task each iteration; mark it `[x]` + `— done:` when its *Done when* condition is
observably satisfied.
