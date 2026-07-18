# Milestone 8 — Ralph-loop prompt

**What this is:** an autonomous single-task loop prompt for implementing OpenJourney
**Milestone 8 — Acquisition Baseline** (forms, pages, tracking, scoring, stages, companies,
imports) — `docs/milestones/v1-milestone-8-plan.md`, tasks 13.0–13.10. Each run does exactly ONE
task, verifies it, records progress in the plan file, commits, and stops. Run it repeatedly (fresh
context each time) until it prints `MILESTONE 8 COMPLETE` or writes a new line to
`docs/milestones/BLOCKERS.md`.

**Recommended runner:** use the repository CLI to start a fresh Codex or Antigravity process for
each task. The primary provider gets one attempt; if it fails before committing or recording a
blocker, the other provider gets one recovery attempt against the same working tree.

```bash
go run ./cmd/ralph --primary codex --unsafe-autonomous
go run ./cmd/ralph --primary antigravity --unsafe-autonomous
```

The defaults now target Milestone 8 (`--plan docs/milestones/v1-milestone-8-plan.md`,
`--branch phase8`, `--milestone 8`). Run `go run ./cmd/ralph --help` for model, timeout, iteration,
branch, prompt, plan, and milestone overrides. Use `--dry-run` to validate the repository, prompt,
task scan, CLIs, and configured models without switching branches or invoking an agent. Runtime
transcripts and metadata are stored under `.ralph/runs/` and are ignored by Git.

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
run on the `phase8` branch; re-trigger the same mission for each iteration. Keep auto-run enabled
for the verify commands in STEP 3.

---

```text
You are an autonomous coding agent implementing OpenJourney **Milestone 8 — Acquisition Baseline**
(forms, landing pages, link/UTM tracking, lead scoring, lifecycle stages, company profiles, CSV
imports), strictly following `docs/milestones/v1-milestone-8-plan.md`. This is ONE iteration of a
loop: do **exactly ONE task**, verify it, record it, commit, then STOP. A fresh agent runs next
iteration with NO memory of this one — all state must be on disk (the plan's checkboxes + git). Do
not try to do the whole milestone in one run.

## STEP 1 — Orient (every iteration, in this order)
1. Read `docs/milestones/v1-milestone-8-plan.md` IN FULL.
2. Re-read §"Design decisions (locked)" and §7 "Carry-over hazards & invariants". These are
   INVARIANTS you may not violate.
3. Skim the recipes it cites — 6.1–6.34 from prior plans (`v1-milestone-2..7-plan.md`) and this
   plan's new recipes 6.35–6.39. This milestone reuses the M7 scoring surface, the event pipeline,
   the blob-freeze publish pattern, the link-token redirect, and the audience DSL.
4. Run `git log --oneline -15` and `git status` to see what already exists on disk.
5. A task is DONE only if its line contains `[x]` or a `— done:` note; otherwise it is TODO. Work
   the **first TODO in document order**. Tasks run strictly 13.0 → 13.10 and NEVER skip ahead.
   **13.0 (the public-serving + anti-abuse substrate) must land first** — no public endpoint ships
   before it. Then 13.1–13.4 are the capture core. Document order already enforces this; do not
   reorder.

## STEP 2 — Do exactly ONE task
- Implement only that single sub-task. Follow its referenced Recipe: open the named existing
  file, copy it, rename, change the fields. DO NOT design from scratch or invent patterns —
  everything you need has a cited file:line in the plan.
- Honor these invariants every time they apply:
  - **Every public (unauthenticated) write is defended.** A public form POST / served page must pass
    per-IP rate limit + honeypot + an HMAC-signed form token + an optional captcha hook, AND validate
    the payload against the pinned form's JSON Schema. A public endpoint missing any of these is a
    bug. Never trust a client-IP header unless a trusted proxy is configured.
  - **Nothing writes `profiles`/`companies` directly.** Forms and imports EMIT events via
    `AcceptEvents` (idempotent on tenant+app+idempotency_key) and let the projector create/merge rows
    — preserving idempotency, identity-merge, and consent. Every submission/import row carries an
    idempotency key. Capture consent evidence into `consent_ledger.evidence`.
  - **Rules-based lead points REUSE M7 scoring.** A lead score is a `kind='expression'` scoring model
    writing numeric `profile_scores.value`, queried by the existing `Score` audience leaf — do NOT
    create a parallel points table. **Lifecycle stages are categorical → `profiles.attributes.stage`**
    (never `profile_scores`, which is numeric).
  - **Forms & pages are immutable once published.** Reuse the `journey/publish.go` blob freeze; publish
    requires the human-actor gate (`journeys.go:80`); served surfaces render the PINNED version, never
    the draft.
  - **Audience legs (Company / stage / score) are parameterized + scoped.** Never interpolate
    values/identifiers into SQL; keep the `fieldSafetyRegex` allowlist; filter `tenant_id` AND
    `workspace_id`.
  - **Consent + suppression honored on every capture and import** — imports never resurrect a
    suppressed endpoint; consent evidence is recorded.
  - **Determinism/governance.** NO `math/rand`. New scope → add in ALL THREE places (`rbac.go`
    allowlist, the `api_keys` default array in the new migration, the `s.authenticate("scope", ...)`
    routes). Widen the `operation_jobs.job_type` CHECK for `profiles.import`. Add new event types
    (`form.submitted`, `company.updated`, `stage.changed`) to `isBuiltInEvent` (or register schemas).
    Enumerate EVERY value the code writes in any CHECK constraint.
  - New migration = next zero-padded number (start at `035`), `IF NOT EXISTS`, uuid PK, timestamptz.
  - New frontend: follow the existing section/editor patterns; add NO new npm dependency; theme-aware.

## STEP 3 — Verify (MANDATORY — do not mark a task done if any check fails)
- Go changes: `go build ./... && go vet ./... && go test ./...` (or the touched package).
  Run `go mod tidy` if you added an import.
- Migration: confirm it applies cleanly against a test DB, and that any widened/added CHECK ACCEPTS
  every value the code writes AND still REJECTS an unknown one.
- UI changes: `cd web && npm run typecheck && npm run build && npm test`.
- The task's literal **"Done when"** condition MUST be observably satisfied. For the load-bearing
  properties (anti-abuse rejections, submission idempotency = one profile, publish-requires-human,
  parameterized DSL SQL, import idempotency) that means an actual test proving the property — not
  just `err == nil`.

## STEP 4 — Record & commit
- Edit `docs/milestones/v1-milestone-8-plan.md`: prefix the finished sub-task with `[x]` and
  append `— done: <one-line evidence>` (match the prior-plan style).
- Ensure you are on branch `phase8` (create it from the current branch if it doesn't exist).
  NEVER commit to `main`.
- Commit ONLY this task's changes with a conventional message, e.g.
  `feat(forms): 13.1 add versioned form registry with typed-field schema`
  or `feat(capture): 13.2 add defended public form-submit endpoint`.
  Follow the repository's commit-trailer convention.

## STEP 5 — Stop
- After committing ONE task, output a 2-line summary (task id + what you did) and STOP.
- If ALL tasks are `[x]`, output `MILESTONE 8 COMPLETE` and STOP.
- If BLOCKED (real ambiguity, a failing build you can't fix within this task's scope, a missing
  prerequisite, a needed human approval, or anything that would require weakening an invariant —
  e.g. an undefended public endpoint, a direct profile/company write, unparameterized DSL SQL, a
  parallel points table, or publishing without the human gate): do NOT hack around it and do NOT
  mark the task done. Append the blocker to `docs/milestones/BLOCKERS.md` (task id + what's blocking
  + what you need), commit that file only, and STOP.
```

---

**Runner:** loop this mission with a **fresh context each iteration**, capped at some max, and
stop when the output contains `MILESTONE 8 COMPLETE` or a new line lands in
`docs/milestones/BLOCKERS.md`.

**Note:** the M8 tasks are plain numbered lines. This prompt (and `cmd/ralph`) treats "no `[x]` /
no `— done:`" as TODO, so it works as-is; if you want an unambiguous scan signal, convert every
13.x sub-task to `[ ]` checkboxes in one pass first.
