# Milestone 9 — Ralph-loop prompt

**What this is:** an autonomous single-task loop prompt for implementing OpenJourney
**Milestone 9 — Extension Ecosystem** (signed remote + WASI extensions) —
`docs/milestones/v1-milestone-9-plan.md`, tasks 14.0–14.10. Each run does exactly ONE task,
verifies it, records progress in the plan file, commits, and stops. Run it repeatedly (fresh
context each time) until it prints `MILESTONE 9 COMPLETE` or writes a new line to
`docs/milestones/BLOCKERS.md`.

**Recommended runner:** use the repository CLI to start a fresh Codex or Antigravity process for
each task. The primary provider gets one attempt; if it fails before committing or recording a
blocker, the other provider gets one recovery attempt against the same working tree.

```bash
go run ./cmd/ralph --primary codex --unsafe-autonomous
go run ./cmd/ralph --primary antigravity --unsafe-autonomous
```

The defaults now target Milestone 9 (`--plan docs/milestones/v1-milestone-9-plan.md`,
`--branch phase9`, `--milestone 9`). Run `go run ./cmd/ralph --help` for model, timeout, iteration,
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
run on the `phase9` branch; re-trigger the same mission for each iteration. Keep auto-run enabled
for the verify commands in STEP 3.

---

```text
You are an autonomous coding agent implementing OpenJourney **Milestone 9 — Extension Ecosystem**
(signed remote extensions + a WASI/Wasm sandbox: channel providers, journey actions/conditions,
ingestion transforms, template functions, connectors), strictly following
`docs/milestones/v1-milestone-9-plan.md`. This is ONE iteration of a loop: do **exactly ONE task**,
verify it, record it, commit, then STOP. A fresh agent runs next iteration with NO memory of this
one — all state must be on disk (the plan's checkboxes + git). Do not try to do the whole milestone
in one run.

## STEP 1 — Orient (every iteration, in this order)
1. Read `docs/milestones/v1-milestone-9-plan.md` IN FULL.
2. Re-read §"Design decisions (locked)" and §7 "Carry-over hazards & invariants". These are
   INVARIANTS you may not violate.
3. Skim the recipes it cites — 6.1–6.39 from prior plans (`v1-milestone-2..8-plan.md`) and this
   plan's new recipes 6.40–6.44. This milestone reuses the channel Registry, the `ai_decision`
   bounded-call node pattern, the HMAC/SSRF egress stack, the immutable-version blob freeze,
   `deriveAgent`, the Liquid engine, and the outbox/leased queue.
4. Run `git log --oneline -15` and `git status` to see what already exists on disk.
5. A task is DONE only if its line contains `[x]` or a `— done:` note; otherwise it is TODO. Work
   the **first TODO in document order**. Tasks run strictly 14.0 → 14.10 and NEVER skip ahead.
   **14.0 (the signed, scope-granted, versioned manifest registry) must land first** — no extension
   is invoked before it exists. Then 14.1 is the invocation host; each extension type builds on it.
   Document order already enforces this; do not reorder.

## STEP 2 — Do exactly ONE task
- Implement only that single sub-task. Follow its referenced Recipe: open the named existing
  file, copy it, rename, change the fields. DO NOT design from scratch or invent patterns —
  everything you need has a cited file:line in the plan.
- Honor these invariants every time they apply:
  - **Every extension call is bounded and fails to a DETERMINISTIC value, never a host retry.** A
    remote timeout / open circuit breaker / wasm trap returns a fallback signal the caller maps to a
    deterministic branch (journey node → fallback branch), reject-or-passthrough (ingestion
    transform), or a normal `DeliveryError` (channel) — it must NEVER trigger `FailJourneyStep` / job
    retries that stall or dead-letter the host. Reuse the `ai_decision` pattern (`nodes.go:294`).
  - **Extensions run under `ActorType="extension"` with scopes = grant ∩ manifest-requested** (reuse
    `deriveAgent`, `internal/ai/tools/tools.go:133`). They CANNOT bypass consent, publish, or
    human-approval gates; a call beyond the grant is `denied_scope` + logged.
  - **WASI transforms are deterministic + sandboxed.** wazero module with NO host network/filesystem,
    NO host clock/RNG; a memory cap + a ctx-deadline kill. Ingestion transforms may enrich/annotate
    the payload ONLY — never forge identity or bypass consent.
  - **Remote calls are signed + SSRF-guarded.** HMAC-sign outbound (reuse `webhook.go:134`); the
    endpoint MUST be on the extension's `endpoint_allowlist`; keep the TOCTOU-safe dial guard; never
    blanket-bypass the private-IP guard.
  - **Manifests are signed + verified before install.** Verify the publisher JWS (`go-jose`, already a
    dep) against a trusted key; install/enable requires the human-actor gate (`journeys.go:80`); an
    unsigned/wrong-key manifest is rejected.
  - **Every invocation is audited** in the append-only `extension_activity` table (allowed AND
    denied). An unlogged extension call is a bug. Config carries secret REFERENCES (`*_ref` →
    env/`_FILE`), never raw secrets; the kill switch (`status='disabled'`) is honored on every path.
  - **Determinism/governance.** NO `math/rand`. New scope → add in ALL THREE places (`rbac.go`
    allowlist, the `api_keys` default array in the new migration, the `s.authenticate("scope", ...)`
    routes). Widen the `operation_jobs.job_type` CHECK for `connector.run`. Enumerate EVERY value the
    code writes in any CHECK constraint. The ONLY new dependency is `wazero` (pure-Go WASI) — after
    adding it run `go mod tidy`; add NO other Go or npm dependency.
  - New migration = next zero-padded number (start at `041`), `IF NOT EXISTS`, uuid PK, timestamptz.
  - New frontend: follow the existing section/editor patterns; add NO new npm dependency; theme-aware.

## STEP 3 — Verify (MANDATORY — do not mark a task done if any check fails)
- Go changes: `go build ./... && go vet ./... && go test ./...` (or the touched package).
  Run `go mod tidy` if you added an import (expect `wazero` only, in 14.4).
- Migration: confirm it applies cleanly against a test DB, and that any widened/added CHECK ACCEPTS
  every value the code writes AND still REJECTS an unknown one.
- UI changes: `cd web && npm run typecheck && npm run build && npm test`.
- The task's literal **"Done when"** condition MUST be observably satisfied. For the load-bearing
  properties (unsigned-manifest-rejected, bounded-failure→deterministic-fallback, wasm-cannot-reach-
  net/fs + killed-at-deadline, scope-intersection-enforced, append-only-audit, wasm-determinism)
  that means an actual test proving the property — not just `err == nil`.

## STEP 4 — Record & commit
- Edit `docs/milestones/v1-milestone-9-plan.md`: prefix the finished sub-task with `[x]` and
  append `— done: <one-line evidence>` (match the prior-plan style).
- Ensure you are on branch `phase9` (create it from the current branch if it doesn't exist).
  NEVER commit to `main`.
- Commit ONLY this task's changes with a conventional message, e.g.
  `feat(extensions): 14.0 add signed versioned manifest registry`
  or `feat(extensions): 14.4 add wazero WASI sandbox for deterministic transforms`.
  Follow the repository's commit-trailer convention.

## STEP 5 — Stop
- After committing ONE task, output a 2-line summary (task id + what you did) and STOP.
- If ALL tasks are `[x]`, output `MILESTONE 9 COMPLETE` and STOP.
- If BLOCKED (real ambiguity, a failing build you can't fix within this task's scope, a missing
  prerequisite, a needed human approval, or anything that would require weakening an invariant —
  e.g. an unsigned/unverified extension, an unbounded or host-stalling call, a wasm module with
  net/fs access, a raw secret in config, or an extension exceeding its granted scopes): do NOT hack
  around it and do NOT mark the task done. Append the blocker to `docs/milestones/BLOCKERS.md`
  (task id + what's blocking + what you need), commit that file only, and STOP.
```

---

**Runner:** loop this mission with a **fresh context each iteration**, capped at some max, and
stop when the output contains `MILESTONE 9 COMPLETE` or a new line lands in
`docs/milestones/BLOCKERS.md`.

**Note:** the M9 tasks are plain numbered lines. This prompt (and `cmd/ralph`) treats "no `[x]` /
no `— done:`" as TODO, so it works as-is; if you want an unambiguous scan signal, convert every
14.x sub-task to `[ ]` checkboxes in one pass first.
