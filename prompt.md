# Milestone 10 — Ralph-loop prompt

**What this is:** an autonomous single-task loop prompt for implementing OpenJourney
**Milestone 10 — Data Platform & Connectors** (warehouse/object-storage sources, reverse-ETL sinks,
event-stream export, identity resolution) — `docs/milestones/v1-milestone-10-plan.md`, tasks
15.0–15.12. Each run does exactly ONE task, verifies it, records progress in the plan file, commits,
and stops. Run it repeatedly (fresh context each time) until it prints `MILESTONE 10 COMPLETE` or
writes a new line to `docs/milestones/BLOCKERS.md`.

**Recommended runner:** use the repository CLI to start a fresh Codex or Antigravity process for
each task. The primary provider gets one attempt; if it fails before committing or recording a
blocker, the other provider gets one recovery attempt against the same working tree.

```bash
go run ./cmd/ralph --primary codex --unsafe-autonomous
go run ./cmd/ralph --primary antigravity --unsafe-autonomous
```

The defaults now target Milestone 10 (`--plan docs/milestones/v1-milestone-10-plan.md`,
`--branch phase10`, `--milestone 10`). Run `go run ./cmd/ralph --help` for model, timeout, iteration,
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
run on the `phase10` branch; re-trigger the same mission for each iteration. Keep auto-run enabled
for the verify commands in STEP 3.

---

```text
You are an autonomous coding agent implementing OpenJourney **Milestone 10 — Data Platform &
Connectors** (warehouse/object-storage/stream SOURCES that ride the event pipeline, reverse-ETL +
event-stream export SINKS, and namespaced identity resolution with reversible merge), strictly
following `docs/milestones/v1-milestone-10-plan.md`. This is ONE iteration of a loop: do **exactly
ONE task**, verify it, record it, commit, then STOP. A fresh agent runs next iteration with NO memory
of this one — all state must be on disk (the plan's checkboxes + git). Do not try to do the whole
milestone in one run.

## STEP 1 — Orient (every iteration, in this order)
1. Read `docs/milestones/v1-milestone-10-plan.md` IN FULL.
2. Re-read §"Design decisions (locked)" and §7 "Carry-over hazards & invariants". These are
   INVARIANTS you may not violate.
3. Skim the recipes it cites — 6.1–6.44 from prior plans (`v1-milestone-2..9-plan.md`) and this
   plan's new recipes 6.45–6.51. This milestone reuses the M9 extension host (a connector IS an
   extension of `kind='connector'`), the leased `operation_jobs` queue + `internal/operations`
   executor pattern, the M8 CSV-import shape (`executeImport`), the `outbox_events` fan-out +
   `internal/dispatcher`, the audience `compile_pg` compiler, the immutable blob freeze, the
   `identity_aliases`/`identity_merges` scaffolding, and the SSRF/allowlist egress + `*_ref` secrets.
4. Run `git log --oneline -15` and `git status` to see what already exists on disk.
5. A task is DONE only if **its own** numbered line/block literally contains `[x]` or a `— done:`
   note. Check EACH task individually — a neighbouring task being done (e.g. `15.0.2` and `15.0.4`
   marked done) does **NOT** make the one between them (`15.0.3`) done. Work the **single first
   unchecked task in document order** and nothing else.
   - **NEVER skip a task or jump ahead.** Do not implement a later task while an earlier one is
     unchecked — not even if the later one looks easier or more "real". Tasks run strictly
     15.0 → 15.12 in order.
   - **If the first unchecked task looks already-implemented in the code:** do NOT skip it and do NOT
     move on. VERIFY its literal *Done when* condition. If it genuinely holds, mark that task `[x]` +
     `— done: <evidence>` and commit **that doc change** (a docs-only commit for one task is correct).
     If it does not hold, implement it. Either way you have worked the first unchecked task.
   - **If you cannot complete the first unchecked task** (too hard, ambiguous, needs a human): append a
     line to `docs/milestones/BLOCKERS.md` and commit only that. Do NOT route around it by doing a
     different task.
   - **Why this is strict:** the runner independently tracks the exact first-unchecked task. If you
     commit work for a *different* task, the runner halts with `changed Git history ambiguously`
     (exit 4) and the loop stalls — your work does not advance the milestone. Always work the first
     unchecked task so the runner and you agree.
   **15.0 (the M9 security closeout) and 15.1 (connector foundation: registry + pipeline + freeze +
   scopes) must land first** — the new connector egress/scope/audit paths inherit 15.0's fixes, and no
   connector runs before 15.1's governed, versioned foundation exists. Document order already enforces
   this; do not reorder.

## STEP 2 — Do exactly ONE task
- Implement only that single sub-task. Follow its referenced Recipe: open the named existing file,
  copy it, rename, change the fields. DO NOT design from scratch or invent patterns — everything you
  need has a cited file:line in the plan.
- Honor these invariants every time they apply:
  - **Nothing writes `profiles`/`identity_*` directly.** A source builds `[]domain.Event` and calls
    `Store.AcceptEvents` (the M8 importer shape, `operations.go:192`); identity resolution/merge happens
    ONLY inside `ProjectEvent`/`resolveIdentity`. The ONLY sanctioned direct writer is
    `UpdateProfileAttributes` (`journey_runtime.go:644`) — do NOT add another.
  - **Deterministic idempotency everywhere.** Source rows dedup by `(tenant,app,idempotency_key)`
    (key = `"<connector>:<version>:<cursor>:<row>"`); sink writes are upsert-keyed; export dedups by
    `outbox_events UNIQUE(topic,event_id)`; merges are order-independent. Re-running any pipeline is a
    no-op, not a duplication. **NO `math/rand`, NO wall-clock in identity/merge decisions.**
  - **A connector IS an extension** of `kind='connector'` with `transport ∈ {native, remote_http}`.
    Reuse ALL M9 governance — JWS signature, scope intersection, `*_ref` secrets, `extension_activity`
    audit, circuit breaker, kill switch, rate/budget. Do NOT build a parallel registry/table system.
  - **Every connector call is bounded + governed + audited** — via the M9 host or a native driver
    reusing the same SSRF-guarded client. A connector failure NEVER stalls or dead-letters the host loop
    unbounded; it fails the `connector_runs` row and returns. (This is exactly the M9 15.0.4 gap — do not
    reintroduce it.)
  - **Reverse-ETL/export are read-only over OpenJourney data** (`profiles`/`accepted_events`/
    `outbox_events`) and never mutate source tables; reverse-ETL respects suppression (`CompileConsent`).
    Every mapped field name passes `fieldSafetyRegex`; every value stays parameterized.
  - **Credentials are `*_ref` references, never raw** (`resolver.go:44` → env/`_FILE`). Publish/enable
    of a connector or pipeline requires the human-actor gate (`journeys.go:80`). Native + remote egress
    is SSRF/allowlist-guarded (`host.go:35-84`); never blanket-bypass the private-IP guard.
  - **Identity resolution/merge is reversible + event-sourced.** Extend the existing
    `identity.alias`/`identity.merge` cases; a merge TOMBSTONES the loser (`profiles.merged_into`) and
    snapshots it to `reversal_ref` — NEVER hard-delete; an `identity.unmerge` event restores it. Winner
    is deterministic (namespace priority + policy version).
  - **Governance wiring.** New scope → add in ALL THREE places (`rbac.go` allowlist, the `api_keys`
    DEFAULT array RE-DECLARED in the new migration, the `s.authenticate("connectors:...", ...)` routes).
    New job type → add to the `operation_jobs.job_type` CHECK (DROP/ADD) AND a `FailOperationJob`
    terminal branch. Enumerate EVERY value the code writes in any CHECK. Append-only audit tables carry
    a `BEFORE UPDATE OR DELETE` trigger AND a `REVOKE UPDATE, DELETE`.
  - **NO new dependency — stricter than M9.** S3 = `minio-go`, warehouse = `clickhouse-go`, streams =
    `franz-go` (all already in `go.mod`); everything else (Snowflake/BigQuery/Parquet/Segment) is a
    REMOTE connector via the M9 host. `go mod tidy` MUST show no additions; `web/package.json` unchanged.
    A task that seems to need a native driver not already present is out of scope as written — use a
    remote connector.
  - New migration = next zero-padded number (start at `043`), `IF NOT EXISTS`, uuid PK, timestamptz.
  - New frontend: follow the existing section/editor patterns; add NO new npm dependency; theme-aware.

## STEP 3 — Verify (MANDATORY — do not mark a task done if any check fails)
- Go changes: `go build ./... && go vet ./... && go test ./...` (or the touched package).
  Run `go mod tidy` and confirm `git diff go.mod go.sum` shows NO new dependency.
- Migration: confirm it applies cleanly against a test DB, and that any widened/added CHECK ACCEPTS
  every value the code writes AND still REJECTS an unknown one.
- UI changes: `cd web && npm run typecheck && npm run build && npm test`.
- The task's literal **"Done when"** condition MUST be observably satisfied. For the load-bearing
  properties (source-round-trip-idempotent, reverse-ETL-no-dupes-and-source-untouched, export-at-least-
  once-deduped, order-independent-deterministic-merge, reversible-unmerge, secret-ref-only, SSRF/scope/
  human-gate enforced, append-only-audit) that means an actual test proving the property — not just
  `err == nil`.

## STEP 4 — Record & commit
- Edit `docs/milestones/v1-milestone-10-plan.md`: prefix the finished sub-task with `[x]` and
  append `— done: <one-line evidence>` (match the prior-plan style).
- Ensure you are on branch `phase10` (create it from the current branch if it doesn't exist).
  NEVER commit to `main`.
- Commit ONLY this task's changes with a conventional message, e.g.
  `feat(connectors): 15.1 add connector driver registry and pipeline freeze`
  or `feat(identity): 15.9 add deterministic reversible profile merge`.
  Follow the repository's commit-trailer convention.

## STEP 5 — Stop
- After committing ONE task, output a 2-line summary (task id + what you did) and STOP.
- If ALL tasks are `[x]`, output `MILESTONE 10 COMPLETE` and STOP.
- If BLOCKED (real ambiguity, a failing build you can't fix within this task's scope, a missing
  prerequisite, a needed human approval, or anything that would require weakening an invariant —
  e.g. a direct `profiles` write outside the event pipeline, an unbounded/host-stalling connector call,
  a raw secret in config, a new go.mod dependency, a non-reversible hard-delete merge, or an
  unguarded/un-audited egress): do NOT hack around it and do NOT mark the task done. Append the blocker
  to `docs/milestones/BLOCKERS.md` (task id + what's blocking + what you need), commit that file only,
  and STOP.
```

---

**Runner:** loop this mission with a **fresh context each iteration**, capped at some max, and
stop when the output contains `MILESTONE 10 COMPLETE` or a new line lands in
`docs/milestones/BLOCKERS.md`.

**Note:** the M10 tasks are plain numbered lines. This prompt (and `cmd/ralph`) treats "no `[x]` /
no `— done:`" as TODO, so it works as-is; if you want an unambiguous scan signal, convert every
15.x sub-task to `[ ]` checkboxes in one pass first.
