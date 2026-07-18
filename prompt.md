# Milestone 7 — Ralph-loop prompt

**What this is:** an autonomous single-task loop prompt for implementing OpenJourney
**Milestone 7 — Predictive Scoring, Realtime AI Decisioning & Online Optimization**
(`docs/milestones/v1-milestone-7-plan.md`, tasks 12.0–12.11). Each run does exactly ONE task,
verifies it, records progress in the plan file, commits, and stops. Run it repeatedly (fresh
context each time) until it prints `MILESTONE 7 COMPLETE` or writes a new line to
`docs/milestones/BLOCKERS.md`.

**Recommended runner:** use the repository CLI to start a fresh Codex or Antigravity process for
each task. The primary provider gets one attempt; if it fails before committing or recording a
blocker, the other provider gets one recovery attempt against the same working tree.

```bash
go run ./cmd/ralph --primary codex --unsafe-autonomous
go run ./cmd/ralph --primary antigravity --unsafe-autonomous
```

The defaults now target Milestone 7 (`--plan docs/milestones/v1-milestone-7-plan.md`,
`--branch phase7`, `--milestone 7`). Run `go run ./cmd/ralph --help` for model, timeout,
iteration, branch, prompt, plan, and milestone overrides. Use `--dry-run` to validate the
repository, prompt, task scan, CLIs, and configured models without switching branches or invoking
an agent. Runtime transcripts and metadata are stored under `.ralph/runs/` and are ignored by Git.

The runner prints a milestone task bar before and after each completed task. After every provider
attempt it also prints elapsed time, attempt totals, and Codex input/cached/output/reasoning token
usage. Antigravity headless mode does not expose token counts, so its attempts and duration are
reported with `provider quota unavailable`; the runner still shows unfinished tasks and remaining
iteration budget before and after each attempt. The latest machine-readable aggregate is written to
`.ralph/usage.json`.

The unrestricted flag is deliberately explicit: both agents need non-interactive permission to
edit, verify, update the plan, and commit. Run it only in a trusted checkout. The default Codex
model is `gpt-5.6-luna`; startup fails closed when that model is absent from the local Codex
catalog. The Antigravity default is `Gemini 3.5 Flash (Medium)`.

**Manual Antigravity use:** alternatively, paste the block below as the agent mission and let it
run on the `phase7` branch; re-trigger the same mission for each iteration. Keep auto-run enabled
for the verify commands in STEP 3.

---

```text
You are an autonomous coding agent implementing OpenJourney **Milestone 7 — Predictive Scoring,
Realtime AI Decisioning & Online Optimization**, strictly following
`docs/milestones/v1-milestone-7-plan.md`. This is ONE iteration of a loop: do **exactly ONE
task**, verify it, record it, commit, then STOP. A fresh agent runs next iteration with NO memory
of this one — all state must be on disk (the plan's checkboxes + git). Do not try to do the whole
milestone in one run.

## STEP 1 — Orient (every iteration, in this order)
1. Read `docs/milestones/v1-milestone-7-plan.md` IN FULL.
2. Re-read §"Design decisions (locked)" and §7 "Carry-over hazards & invariants". These are
   INVARIANTS you may not violate.
3. Skim the recipes it cites — 6.1–6.30 from prior plans (`v1-milestone-2..6-plan.md`) and this
   plan's new recipes 6.31–6.34. This milestone builds on the M6 AI gateway/registry/eval/audit and
   the M4 experiment stats/holdout.
4. Run `git log --oneline -15` and `git status` to see what already exists on disk.
5. A task is DONE only if its line contains `[x]` or a `— done:` note; otherwise it is TODO. Work
   the **first TODO in document order**. Tasks run strictly 12.0 → 12.11 and NEVER skip ahead.
   **12.0 (the M6 audit-hardening fold-in) must land first** — more AI decisioning rides on the
   audit trail. Document order already enforces this; do not reorder.

## STEP 2 — Do exactly ONE task
- Implement only that single sub-task. Follow its referenced Recipe: open the named existing
  file, copy it, rename, change the fields. DO NOT design from scratch or invent patterns —
  everything you need has a cited file:line in the plan.
- Honor these invariants every time they apply:
  - **Realtime AI node fails to a DETERMINISTIC branch, never to a retry.** In the `ai_decision`
    node, a model timeout / over-budget / provider error / schema-reject must return a normal
    ExecutionResult on the configured fallback branch — NEVER an error that triggers
    `FailJourneyStep` (which re-invokes the model up to 10× and can dead-letter the run). The run
    ALWAYS advances; the per-call timeout is far tighter than the 5-minute step lease.
  - **AI decisioning is bounded + governed.** The pinned `prompt_version` must be `status='active'`
    AND `eval_status='passed'`; enforce a per-call timeout AND a per-call cost cap; the output must
    map to a declared branch or fall back; record every decision in `ai_activity`.
  - **Scores are versioned + eval-gated + human-published.** A scoring-model version is immutable
    (blob-frozen); publishing requires the human-actor gate; a compute job refuses a non-`passed`
    version. `profile_scores` records which `model_version` produced each value.
  - **Score DSL leg is parameterized + scoped.** Never interpolate values/identifiers into SQL;
    keep the `fieldSafetyRegex` allowlist; filter `tenant_id` AND `workspace_id`.
  - **Online optimization PROPOSES; humans APPROVE.** Never self-reallocate or self-rollout.
    Reallocate via a NEW immutable experiment version (seed UNCHANGED, holdout preserved); a
    guardrail regression HALTS; the approve path reuses the human-actor gate (api_key/ai_agent →
    403). Never send to a holdout.
  - **Determinism.** NO `math/rand`; deterministic bucketing/assignment; the expression scorer is
    pure; tests use the `fake` provider + golden outputs; NO test depends on a live model.
  - **Audit + scan-free.** Every AI decision and score computation logs to the append-only
    `ai_activity`; segment resolution / reports add no full-population OLTP scans (score reads hit
    the indexed `profile_scores`).
  - New scope → add in ALL THREE places (`rbac.go` allowlist, the `api_keys` default array in the
    new migration, and the `s.authenticate("scope", ...)` routes). Widen the
    `operation_jobs.job_type` CHECK for `scores.compute`; enumerate EVERY value the code writes in
    any CHECK constraint.
  - New migration = next zero-padded number (start at `030`), `IF NOT EXISTS`, uuid PK, timestamptz.
    When altering an UNNAMED inline CHECK, confirm its generated name with `\d <table>` first.
  - New frontend: follow the existing section/editor patterns; add NO new npm dependency; theme-aware.

## STEP 3 — Verify (MANDATORY — do not mark a task done if any check fails)
- Go changes: `go build ./... && go vet ./... && go test ./...` (or the touched package).
  Run `go mod tidy` if you added an import.
- Migration: confirm it applies cleanly against a test DB, and that any widened/added CHECK ACCEPTS
  every value the code writes AND still REJECTS an unknown one.
- UI changes: `cd web && npm run typecheck && npm run build && npm test`.
- The task's literal **"Done when"** condition MUST be observably satisfied. For the load-bearing
  properties (realtime-node-falls-back-on-timeout, run-never-dead-letters-on-model-failure,
  scores-eval-gated, score-SQL-parameterized, online-opt-requires-human, seed-immutable,
  holdout-preserved, append-only-audit) that means an actual test proving the property on the FAKE
  provider / deterministic scorer — not just `err == nil`.

## STEP 4 — Record & commit
- Edit `docs/milestones/v1-milestone-7-plan.md`: prefix the finished sub-task with `[x]` and
  append `— done: <one-line evidence>` (match the prior-plan style).
- Ensure you are on branch `phase7` (create it from the current branch if it doesn't exist).
  NEVER commit to `main`.
- Commit ONLY this task's changes with a conventional message, e.g.
  `feat(scoring): 12.2 add versioned scoring-model registry`
  or `feat(journeys): 12.7 add bounded realtime ai_decision node with deterministic fallback`.
  Follow the repository's commit-trailer convention.

## STEP 5 — Stop
- After committing ONE task, output a 2-line summary (task id + what you did) and STOP.
- If ALL tasks are `[x]`, output `MILESTONE 7 COMPLETE` and STOP.
- If BLOCKED (real ambiguity, a failing build you can't fix within this task's scope, a missing
  prerequisite, a needed human approval, or anything that would require weakening an invariant —
  e.g. letting AI self-rollout, a realtime node that errors instead of falling back, unparameterized
  score SQL, changing an experiment seed, or sending to a holdout): do NOT hack around it and do NOT
  mark the task done. Append the blocker to `docs/milestones/BLOCKERS.md` (task id + what's blocking
  + what you need), commit that file only, and STOP.
```

---

**Runner:** loop this mission with a **fresh context each iteration**, capped at some max, and
stop when the output contains `MILESTONE 7 COMPLETE` or a new line lands in
`docs/milestones/BLOCKERS.md`.

**Note:** the M7 tasks are plain numbered lines. This prompt (and `cmd/ralph`) treats "no `[x]` /
no `— done:`" as TODO, so it works as-is; if you want an unambiguous scan signal, convert every
12.x sub-task to `[ ]` checkboxes in one pass first.
