# Milestone 6 — Ralph-loop prompt

**What this is:** an autonomous single-task loop prompt for implementing OpenJourney
**Milestone 6 — Governed AI Layer** (`docs/milestones/v1-milestone-6-plan.md`, tasks 11.0–11.14).
Each run does exactly ONE task, verifies it, records progress in the plan file, commits, and
stops. Run it repeatedly (fresh context each time) until it prints `MILESTONE 6 COMPLETE` or
writes a new line to `docs/milestones/BLOCKERS.md`.

**Recommended runner:** use the repository CLI to start a fresh Codex or Antigravity process for
each task. The primary provider gets one attempt; if it fails before committing or recording a
blocker, the other provider gets one recovery attempt against the same working tree.

```bash
go run ./cmd/ralph --primary codex --unsafe-autonomous
go run ./cmd/ralph --primary antigravity --unsafe-autonomous
```

Run `go run ./cmd/ralph --help` for model, timeout, iteration, branch, prompt, and plan overrides.
Use `--dry-run` to validate the repository, prompt, task scan, CLIs, and configured models without
switching branches or invoking an agent. Runtime transcripts and metadata are stored under
`.ralph/runs/` and are ignored by Git.

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
run on the `phase6` branch; re-trigger the same mission for each iteration. Keep auto-run enabled
for the verify commands in STEP 3.

---

```text
You are an autonomous coding agent implementing OpenJourney **Milestone 6 — Governed AI Layer**,
strictly following `docs/milestones/v1-milestone-6-plan.md`. This is ONE iteration of a loop: do
**exactly ONE task**, verify it, record it, commit, then STOP. A fresh agent runs next iteration
with NO memory of this one — all state must be on disk (the plan's checkboxes + git). Do not try
to do the whole milestone in one run.

## STEP 1 — Orient (every iteration, in this order)
1. Read `docs/milestones/v1-milestone-6-plan.md` IN FULL.
2. Re-read §"Design decisions (locked)" and §7 "Carry-over hazards & invariants". These are
   INVARIANTS you may not violate.
3. Skim the recipes it cites — 6.1–6.25 from prior plans (`v1-milestone-2..5-plan.md`) and this
   plan's AI recipes 6.26–6.30. This milestone builds on all of them.
4. Run `git log --oneline -15` and `git status` to see what already exists on disk.
5. A task is DONE only if its line contains `[x]` or a `— done:` note; otherwise it is TODO. Work
   the **first TODO in document order**. Tasks run strictly 11.0 → 11.14 and NEVER skip ahead.
   **11.0 (the M5 push-callback signature fix) must land first**, and the **substrate 11.1–11.7
   must be fully done before any copilot 11.8–11.11** — the copilots inherit the gateway,
   registry, redaction, and audit. Document order already enforces this; do not reorder.

## STEP 2 — Do exactly ONE task
- Implement only that single sub-task. Follow its referenced Recipe: open the named existing
  file, copy it, rename, change the fields. DO NOT design from scratch or invent patterns —
  everything you need has a cited file:line in the plan.
- Honor these invariants every time they apply:
  - **AI NEVER publishes.** Run AI tools under an `ActorType="ai_agent"` principal whose scopes
    are the INTERSECTION of the caller's scopes and the tool's declared scopes; it must be
    structurally rejected by the human-approval gate (`journeys.go:80`, `experiments.go:72`).
    Every copilot's terminal action is a DRAFT resource or a proposed NEW immutable version —
    NEVER a mutation of a live journey/campaign/segment. Do not add an AI bypass to the gate.
  - **Redact before egress, fail closed.** No `restricted`/unauthorized field ever leaves for a
    model provider; retrieval is permission-aware; untrusted retrieved DATA is passed in a
    delimited section, isolated from instructions (prompt-injection defense). A field with no
    classification defaults to REDACT, not send.
  - **Structured output only.** Model output MUST pass its `output_schema` AND the deterministic
    domain validator (`audience.Parse`, journey `validate`, or `render`) before ANY use;
    schema-reject → one bounded repair retry → hard fail. No free-form model text drives a mutation.
  - **Pinned immutable prompts.** Every gateway invoke references a `prompt_version_id` that is
    `status='active'` AND `eval_status='passed'`. Changing a prompt = a NEW version (+ eval gate +
    human publish). Reuse the `journey/publish.go` freeze (sha256 → blob.Put → immutable row).
  - **Egress safety.** Never blanket-bypass the SSRF private-IP guard (`webhook.go` `IsSafeURL`).
    Hosted providers must match the domain allowlist; a local/self-host endpoint is allowed ONLY
    if present in `ai_provider_configs.endpoint_allowlist`. Keep the TOCTOU-safe dial guard.
  - **Log every AI action** in the append-only `ai_activity` table (allowed AND denied), with
    model/prompt_version/cost/tokens/policy_decision/approver. An unlogged AI action is a bug.
  - **Budgets enforced at the gateway** — over-budget denies with a clear error; no silent overspend.
  - **Determinism** — use the `fake` provider + golden outputs in tests; NO test depends on a live
    model; NO `math/rand` anywhere.
  - New scope → add in ALL THREE places (`rbac.go` allowlist, the `api_keys` default array in the
    new migration, and the `s.authenticate("scope", ...)` routes). Widen the
    `operation_jobs.job_type` CHECK for `ai.generate`; register the `ai.action` event type; and
    enumerate EVERY value the code writes in any CHECK constraint.
  - New migration = next zero-padded number (start at `025`), `IF NOT EXISTS`, uuid PK, timestamptz.
    When altering an UNNAMED inline CHECK, confirm its generated name with `\d <table>` first.
  - New frontend: follow the existing section/editor patterns; add NO new npm dependency; theme-aware.

## STEP 3 — Verify (MANDATORY — do not mark a task done if any check fails)
- Go changes: `go build ./... && go vet ./... && go test ./...` (or the touched package).
  Run `go mod tidy` if you added an import.
- Migration: confirm it applies cleanly against a test DB, and that any widened CHECK now ACCEPTS
  the new values AND still REJECTS an unknown one.
- UI changes: `cd web && npm run typecheck && npm run build && npm test`.
- The task's literal **"Done when"** condition MUST be observably satisfied. For the governance
  properties (AI-cannot-publish, redaction-before-egress, unauthorized-retrieval-blocked,
  schema-reject-repair-or-fail, over-budget-denied, every-invoke-logged, eval-gate) that means an
  actual test proving the property on the FAKE provider — not just `err == nil`.

## STEP 4 — Record & commit
- Edit `docs/milestones/v1-milestone-6-plan.md`: prefix the finished sub-task with `[x]` and
  append `— done: <one-line evidence>` (match the prior-plan style).
- Ensure you are on branch `phase6` (create it from the current branch if it doesn't exist).
  NEVER commit to `main`.
- Commit ONLY this task's changes with a conventional message, e.g.
  `feat(ai): 11.1.2 add provider-neutral gateway with fake/anthropic/openai profiles`
  or `fix(channels): 11.0.1 require signature verification on push callbacks`.
  Follow the repository's commit-trailer convention.

## STEP 5 — Stop
- After committing ONE task, output a 2-line summary (task id + what you did) and STOP.
- If ALL tasks are `[x]`, output `MILESTONE 6 COMPLETE` and STOP.
- If BLOCKED (real ambiguity, a failing build you can't fix within this task's scope, a missing
  prerequisite, a needed human approval, or anything that would require weakening an invariant —
  e.g. bypassing the SSRF guard, sending an unredacted field, or letting AI publish): do NOT hack
  around it and do NOT mark the task done. Append the blocker to `docs/milestones/BLOCKERS.md`
  (task id + what's blocking + what you need), commit that file only, and STOP.
```

---

**Runner:** loop this mission with a **fresh context each iteration**, capped at some max, and
stop when the output contains `MILESTONE 6 COMPLETE` or a new line lands in
`docs/milestones/BLOCKERS.md`.

**Note:** the M6 tasks are plain numbered lines. This prompt treats "no `[x]` / no `— done:`" as
TODO, so it works as-is; if you want an unambiguous scan signal, convert every 11.x sub-task to
`[ ]` checkboxes in one pass first.
