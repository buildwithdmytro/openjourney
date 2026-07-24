# Milestone 19 — Ralph-loop prompt

**What this is:** an autonomous single-task loop prompt for a QUALITY milestone of OpenJourney
**Milestone 19 — UX, Stability & Usability (Round 2)** (fix confirmed UX-consistency, stability, and
usability findings X#/S#/U# from a three-dimension audit — NO new features) —
`docs/milestones/v1-milestone-19-plan.md`, tasks 24.0–24.6. Each run does exactly ONE task, verifies it,
records progress in the plan file, commits, and stops. Run it repeatedly (fresh context each time) until
it prints `MILESTONE 19 COMPLETE` or writes a new line to `docs/milestones/BLOCKERS.md`.

**Recommended runner:**

```bash
go run ./cmd/ralph --primary antigravity --unsafe-autonomous
go run ./cmd/ralph --primary codex --unsafe-autonomous
```

The Antigravity default model is `gemini-3.6-flash-medium`. The defaults target Milestone 19
(`--plan docs/milestones/v1-milestone-19-plan.md`, `--branch phase19`, `--milestone 19`).

---

```text
You are an autonomous coding agent implementing OpenJourney **Milestone 19 — UX, Stability & Usability
(Round 2)**. This is NOT a feature milestone: every task FIXES a specific, confirmed finding (keyed X#
UX-polish / S# stability / U# usability in the plan's §1 findings table) from a three-dimension audit, and
ships a TEST/verification. Strictly follow `docs/milestones/v1-milestone-19-plan.md`. This is ONE iteration
of a loop: do **exactly ONE task**, verify it, record it, commit, then STOP. A fresh agent runs next
iteration with NO memory of this one — all state must be on disk. Do not try to do the whole milestone in
one run.

## STEP 1 — Orient (every iteration, in this order)
1. Read `docs/milestones/v1-milestone-19-plan.md` IN FULL — especially §1 (the findings table X#/S#/U#
   mapping each finding to its ref, fix, and task) and §"Design decisions (locked)" and §7 hazards.
2. Run `git log --oneline -15` and `git status`.
3. A task is DONE only if **its own** numbered line/block literally contains `[x]` or a `— done:` note.
   Work the **single first unchecked task in document order** and nothing else.
   - **NEVER skip a task.** Tasks run 24.0 → 24.6 in order. A conditional no-op task is marked `[x]` +
     `— done: <why>` and committed — NEVER skipped (that trips the runner's guard, exit 4).
   - **If the first unchecked task looks already-done:** VERIFY its *Done when*; mark done + commit if it
     holds, else implement it.
   - **If you cannot complete it:** append a line to `docs/milestones/BLOCKERS.md` and commit only that.
   **24.0 (M18 residual) and 24.1 (worker panic-recovery → DLQ, the top stability fix) come first.**

## STEP 2 — Do exactly ONE task
- Open the file the finding cites (the plan gives the ref for each X#/S#/U#), apply the fix, add the
  test/verification. DO NOT redesign — the fix is scoped and cited.
- Honor these invariants every time they apply:
  - **NO regression: the suite stays green and UI migrations PRESERVE roles/labels.** The ~320 web tests
    assert accessible roles/labels — a migration that changes a role/label/user-visible copy WITHOUT the
    task calling for it is a REGRESSION. Every task's test must exercise the finding (a quality task whose
    test doesn't reproduce the bug is INCOMPLETE).
  - **Stability fails SAFE.** Worker loops recover panics into the EXISTING `attempts≥N → dead` DLQ path
    (never swallow — dead-letter the poison message so it can't crash-loop the fleet). Mutations disable
    their trigger button while in-flight (no double-submit). Render paths optional-chain fetched data. A
    root error boundary guards the pre-auth chrome. NO fabricated/synthesized data is shown to users
    (remove the Assistant sparkline that invents a trend).
  - **Reuse the M12 design system — invent NOTHING.** All UI uses the existing `web/src/components/`
    primitives (`PageHeader`, `Card`, `DataTable`, `Field`, `EmptyState`, `ErrorState`, `Spinner`,
    `Skeleton`, `Toast`/`useToast`, `Modal`/`ConfirmDialog`, `useForm`) + `tokens.css`. KILL hand-rolled
    equivalents: raw `<article className="card">`, `<p className="error">`, inline "Loading…", the
    `ui-crash` card, native `window.confirm()`, hardcoded `#hex`/`rgba()` colors, legacy `ResourceTable`/
    `ResourceList`, raw `<table>`.
  - **Feedback + guidance are consistent.** Every create/update/publish/delete fires a `useToast()`
    success toast; every `EmptyState` passes an actionable `cta`; every destructive action (incl. identity
    unmerge) routes through `ConfirmDialog`.
  - **One source of truth for navigation.** The `View` union + `viewTitles` + `navGroups` + palette items
    are consolidated into ONE config consumed by `App.tsx`/`Sidebar.tsx`/`CommandPalette.tsx`/`AppShell.tsx`
    (they are duplicated today). The ⌘K palette runs actions (its `action` field is defined but unused) and
    searches by category/keyword, not just labels.
  - **NO new dependency; no feature change.** `go mod tidy`, `web/package.json`, `web/package-lock.json`,
    `sdk/javascript/package.json` MUST be unchanged (`crewjam/saml` stays; nothing new). `-race` is scoped
    to `internal/postgres/...` (the pre-existing `internal/extension` WASM tests time out under full-suite
    `-race`).

## STEP 3 — Verify (MANDATORY)
- Go changes: `go build ./... && go vet ./... && go test ./...` (add `-race ./internal/postgres/...` for
  the audit-chain package; do NOT `-race` the full suite). Run `go mod tidy`; confirm `git diff go.mod
  go.sum` shows NO new dependency.
- Web changes: `cd web && npm run typecheck && npm run build && npm test`.
- The task's literal **"Done when"** condition MUST be satisfied by a test/verification that reproduces
  the finding.

## STEP 4 — Record & commit
- Edit `docs/milestones/v1-milestone-19-plan.md`: prefix the finished sub-task with `[x]` and append
  `— done: <evidence incl. the finding key(s) and the test name>`.
- Ensure you are on branch `phase19`. NEVER commit to `main`.
- Commit ONLY this task's changes, e.g. `fix(stability): 24.1.1 recover worker-loop panics into the DLQ (S1,S10)`.

## STEP 5 — Stop
- After committing ONE task, output a 2-line summary (task id + which finding it fixed) and STOP.
- If ALL tasks are `[x]`, output `MILESTONE 19 COMPLETE` and STOP.
- If BLOCKED (real ambiguity, a failing build/test you can't fix in scope, or anything that would weaken
  an invariant — e.g. swallowing a panic instead of dead-lettering, a new dependency, breaking an existing
  role/label test, showing fabricated data): do NOT hack around it, do NOT delete/disable a failing test,
  and do NOT mark the task done. Append the blocker to `docs/milestones/BLOCKERS.md` (task id + what's
  blocking + what you need), commit that file only, and STOP.
```

---

**Runner:** loop this mission with a **fresh context each iteration**, capped at some max, stopping when
the output contains `MILESTONE 19 COMPLETE` or a new line lands in `docs/milestones/BLOCKERS.md`.

**Note:** the M19 tasks are `[ ]` checkboxes. Work the first unchecked task each iteration; mark it `[x]`
+ `— done:` when its *Done when* condition (a test that reproduces the finding) is satisfied. A conditional
no-op task is marked done with a note — never skipped.
