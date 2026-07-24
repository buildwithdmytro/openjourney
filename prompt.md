# Milestone 18 — Ralph-loop prompt

**What this is:** an autonomous single-task loop prompt for a HARDENING milestone of OpenJourney
**Milestone 18 — Consolidation & Hardening** (fix confirmed security/data-integrity/audit/test-coverage
findings F1–F18 from a three-dimension audit — NO new features) — `docs/milestones/v1-milestone-18-plan.md`,
tasks 23.0–23.7. Each run does exactly ONE task, verifies it, records progress in the plan file, commits,
and stops. Run it repeatedly (fresh context each time) until it prints `MILESTONE 18 COMPLETE` or writes a
new line to `docs/milestones/BLOCKERS.md`.

**Recommended runner:**

```bash
go run ./cmd/ralph --primary antigravity --unsafe-autonomous
go run ./cmd/ralph --primary codex --unsafe-autonomous
go run ./cmd/ralph --primary claude --unsafe-autonomous
```

The Antigravity default model is `gemini-3.6-flash-medium` (override with `--antigravity-model <slug>`).
The defaults target Milestone 18 (`--plan docs/milestones/v1-milestone-18-plan.md`, `--branch phase18`,
`--milestone 18`). Use `--dry-run` to validate without invoking an agent.

---

```text
You are an autonomous coding agent implementing OpenJourney **Milestone 18 — Consolidation & Hardening**.
This is NOT a feature milestone: every task FIXES a specific, confirmed finding (F1–F18 in the plan's §1
findings table) from a security/test-coverage/data-layer audit, and ships a TEST that fails against the
current buggy code and passes after the fix. Strictly follow `docs/milestones/v1-milestone-18-plan.md`.
This is ONE iteration of a loop: do **exactly ONE task**, verify it, record it, commit, then STOP. A fresh
agent runs next iteration with NO memory of this one — all state must be on disk (the plan's checkboxes +
git). Do not try to do the whole milestone in one run.

## STEP 1 — Orient (every iteration, in this order)
1. Read `docs/milestones/v1-milestone-18-plan.md` IN FULL — especially §1 (the findings table F1–F18,
   which maps each finding to its file:line, fix, and task) and §"Design decisions (locked)" and §7
   "Carry-over hazards & invariants".
2. Run `git log --oneline -15` and `git status` to see what already exists on disk.
3. A task is DONE only if **its own** numbered line/block literally contains `[x]` or a `— done:` note.
   Work the **single first unchecked task in document order** and nothing else.
   - **NEVER skip a task or jump ahead.** Tasks run strictly 23.0 → 23.7. A conditional no-op task is
     marked `[x]` + `— done: <why>` and committed — NEVER skipped (that trips the runner's guard, exit 4).
   - **If the first unchecked task looks already-fixed:** VERIFY its literal *Done when*; if it holds,
     mark done + commit the docs change; else implement it.
   - **If you cannot complete it:** append a line to `docs/milestones/BLOCKERS.md` and commit only that.
   **23.0 (the critical fixes: deployment-breaking audit migration, secret-exfil, scope regression) comes
   first.**

## STEP 2 — Do exactly ONE task
- Open the file the finding cites (the plan gives file:line for each F#), apply the fix, add the test.
  DO NOT redesign — the fix is scoped and cited.
- Honor these invariants every time they apply:
  - **Every fix ships a test that FAILS against the current (buggy) code and passes after.** A hardening
    task whose test doesn't exercise the bug is INCOMPLETE. Where the property is only covered by a
    DB-gated integration test (skips without `OPENJOURNEY_TEST_DATABASE_URL`) or by a test that validates a
    re-implemented FAKE, add a NON-gated unit test that exercises the REAL code and asserts the REAL
    property (e.g. the SSRF test must assert the audit decision is `ssrf_blocked`, not merely `res == nil`
    — which passes even if the guard is deleted).
  - **Fix forward with NEW migrations; NEVER edit an applied migration (001–060).** Corrections (the 059
    backfill, the 060 scope-array regression, the append-only gaps, the indexes) land in NEW migrations
    `061`+, written idempotent (`IF NOT EXISTS`, `CREATE OR REPLACE`, guarded backfills) so they apply on
    BOTH a fresh and a populated database. Test the migration against a POPULATED table where relevant
    (the 059 bug only manifests when `audit_events` already has rows).
  - **Security decisions FAIL CLOSED.** Maker-checker returns DENIED when the creator is unknown (not
    allowed). The connected-content `auth_secret_ref` must MATCH a positive allowlist (e.g.
    `^CC_SECRET_[A-Z0-9_]+$`), not merely "not start with `secret:`". Audit-write failures PROPAGATE
    (stop `_ = s.audit(...)`), written in the mutation's transaction.
  - **Append-only means trigger + REVOKE + TRUNCATE-guard.** Every audit/version table gets a
    `BEFORE UPDATE OR DELETE` block trigger, a `REVOKE UPDATE, DELETE`, and (for audit tables) a
    `BEFORE TRUNCATE` guard. Copy the `045_connector_runs.sql` pattern; keep the `identity_merges`
    GUC-erasure carve-out (`047`).
  - **No behavior change beyond the fix.** Hardening must not regress a feature — the full suite stays
    green. New scopes (e.g. `sso:manage`) go in the FOUR places (`rbac.go`/the permissions catalog, the
    `api_keys.scopes` DEFAULT array re-declared in the newest migration, the route guards,
    `web/src/App.tsx AVAILABLE_SCOPES`); the `View` union is triplicated (`App.tsx`/`Sidebar.tsx`/
    `CommandPalette.tsx`).
  - **NO new dependency.** `go mod tidy`, `web/package.json`, `web/package-lock.json`,
    `sdk/javascript/package.json` MUST be unchanged (`crewjam/saml` from M17 stays; nothing new). UI tests
    reuse the M12 primitives + the vitest `vi.fn()` fetch-stub pattern.
  - New migration = next zero-padded number on disk (currently `061`).

## STEP 3 — Verify (MANDATORY — do not mark a task done if any check fails)
- Go changes: `go build ./... && go vet ./... && go test ./...` (add `-race ./internal/postgres/...` for
  the audit-chain tasks — do NOT run `-race` on the full suite; the pre-existing `internal/extension` WASM
  tests time out under `-race`, unrelated to this milestone). Run `go mod tidy`; confirm `git diff go.mod
  go.sum` shows NO new dependency.
- Migration: applies cleanly on BOTH an empty and (where relevant) a populated table; append-only tables
  reject UPDATE/DELETE/TRUNCATE.
- Web changes: `cd web && npm run typecheck && npm run build && npm test`.
- The task's literal **"Done when"** condition MUST be observably satisfied by a test that reproduces the
  finding.

## STEP 4 — Record & commit
- Edit `docs/milestones/v1-milestone-18-plan.md`: prefix the finished sub-task with `[x]` and append
  `— done: <one-line evidence incl. the finding F# and the regression test name>`.
- Ensure you are on branch `phase18`. NEVER commit to `main`.
- Commit ONLY this task's changes with a conventional message, e.g.
  `fix(security): 23.0.2 require positive allowlist for connected-content secret refs (F3)`.
  Follow the repository's commit-trailer convention.

## STEP 5 — Stop
- After committing ONE task, output a 2-line summary (task id + which finding it fixed) and STOP.
- If ALL tasks are `[x]`, output `MILESTONE 18 COMPLETE` and STOP.
- If BLOCKED (real ambiguity, a failing build/test you can't fix within scope, or anything that would
  require weakening an invariant — e.g. editing an applied migration, a fail-OPEN security decision, a
  test that doesn't exercise the bug, a new dependency): do NOT hack around it, do NOT delete/disable a
  failing test, and do NOT mark the task done. Append the blocker to `docs/milestones/BLOCKERS.md`
  (task id + what's blocking + what you need), commit that file only, and STOP.
```

---

**Runner:** loop this mission with a **fresh context each iteration**, capped at some max, and stop when
the output contains `MILESTONE 18 COMPLETE` or a new line lands in `docs/milestones/BLOCKERS.md`.

**Note:** the M18 tasks are `[ ]` checkboxes. Work the first unchecked task each iteration; mark it `[x]`
+ `— done:` when its *Done when* condition (a regression test that fails against the buggy code) is
satisfied. A conditional no-op task is marked done with a note — never skipped.
