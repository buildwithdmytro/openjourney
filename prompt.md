# Milestone 12 — Ralph-loop prompt

**What this is:** an autonomous single-task loop prompt for implementing OpenJourney
**Milestone 12 — Console UX & Design System** (design tokens, a real component library, shared UX-state
primitives, modal/confirm, form validation, app shell + command palette, mobile, a home/overview
dashboard, and accessibility hardening) — `docs/milestones/v1-milestone-12-plan.md`, tasks 17.0–17.10.
Each run does exactly ONE task, verifies it, records progress in the plan file, commits, and stops. Run
it repeatedly (fresh context each time) until it prints `MILESTONE 12 COMPLETE` or writes a new line to
`docs/milestones/BLOCKERS.md`.

**Recommended runner:** use the repository CLI to start a fresh Codex or Antigravity process for
each task. The primary provider gets one attempt; if it fails before committing or recording a
blocker, the other provider gets one recovery attempt against the same working tree.

```bash
go run ./cmd/ralph --primary codex --unsafe-autonomous
go run ./cmd/ralph --primary antigravity --unsafe-autonomous
go run ./cmd/ralph --primary claude --unsafe-autonomous          # cheapest: defaults to Haiku
```

`--primary` accepts `codex`, `antigravity`, or `claude`. Only the primary and its single fallback
CLI need to be installed (claude falls back to codex). For the cheapest runs use
`--primary claude` (defaults to the `haiku` model; override with `--claude-model sonnet|opus|<id>`).
Claude runs with `--output-format stream-json --verbose` so its progress streams live.

The defaults now target Milestone 12 (`--plan docs/milestones/v1-milestone-12-plan.md`,
`--branch phase12`, `--milestone 12`). Run `go run ./cmd/ralph --help` for model, timeout, iteration,
branch, prompt, plan, and milestone overrides. Use `--dry-run` to validate the repository, prompt,
task scan, CLIs, and configured models without switching branches or invoking an agent. Runtime
transcripts and metadata are stored under `.ralph/runs/` and are ignored by Git.

**Manual use:** alternatively, paste the block below as the agent mission and let it run on the
`phase12` branch; re-trigger the same mission for each iteration. Keep auto-run enabled for the verify
commands in STEP 3.

---

```text
You are an autonomous coding agent implementing OpenJourney **Milestone 12 — Console UX & Design
System** (a design-token foundation, a real React component library, shared UX-state primitives,
modal/confirm, form validation, app shell + command palette, mobile/responsive, a home/overview
dashboard, and accessibility hardening), strictly following `docs/milestones/v1-milestone-12-plan.md`.
This is ONE iteration of a loop: do **exactly ONE task**, verify it, record it, commit, then STOP. A
fresh agent runs next iteration with NO memory of this one — all state must be on disk (the plan's
checkboxes + git). Do not try to do the whole milestone in one run.

## STEP 1 — Orient (every iteration, in this order)
1. Read `docs/milestones/v1-milestone-12-plan.md` IN FULL.
2. Re-read §"Design decisions (locked)" and §7 "Carry-over hazards & invariants". These are
   INVARIANTS you may not violate.
3. Skim the recipes it cites — this plan's new frontend recipes 6.58–6.67. This is a **frontend/UX**
   milestone: almost all work is in `web/` (a new `web/src/components/` primitive library, `web/src/
   tokens.css`, `web/src/sections/*`, `web/src/App.tsx`), plus ONE read-only backend endpoint
   (`GET /v1/overview`) that follows the standard vertical slice. It REUSES the existing styling system
   (`web/src/styles.css`), the section/registration pattern in `App.tsx`, the `requestJSON` API wrapper
   (`web/src/api.ts`), and the vitest + @testing-library test harness.
4. Run `git log --oneline -15` and `git status` to see what already exists on disk.
5. A task is DONE only if **its own** numbered line/block literally contains `[x]` or a `— done:`
   note. Check EACH task individually — a neighbouring task being done does **NOT** make the one
   between them done. Work the **single first unchecked task in document order** and nothing else.
   - **NEVER skip a task or jump ahead.** Tasks run strictly 17.0 → 17.10 in order.
   - **If the first unchecked task looks already-implemented:** do NOT skip it. VERIFY its literal
     *Done when* condition. If it holds, mark it `[x]` + `— done: <evidence>` and commit that doc change.
     If not, implement it.
   - **If you cannot complete the first unchecked task:** append a line to
     `docs/milestones/BLOCKERS.md` and commit only that. Do NOT route around it.
   - **Why this is strict:** the runner independently tracks the first-unchecked task. If you commit
     work for a *different* task, the runner halts with `changed Git history ambiguously` (exit 4).
   **17.0 (the M11 messaging-security closeout) and 17.1 (design tokens + theming) must land first** —
   17.0 verifies/fixes the messaging surface, and 17.1's token layer is the foundation every later
   component builds on. Document order enforces this; do not reorder.

## STEP 2 — Do exactly ONE task
- Implement only that single sub-task. Follow its referenced Recipe: open the named existing file,
  copy its pattern, change the fields. DO NOT design from scratch — everything has a cited file:line.
- Honor these invariants every time they apply:
  - **NON-BREAKING & suite-green, every task.** `cd web && npm run typecheck && npm run build &&
    npm test` MUST pass on every commit. The test suite queries by accessibility semantics (~65
    `getByRole`, ~43 `getByLabelText`) — that is the CONTRACT. A primitive or refactor that changes a
    role, label, or user-visible copy WITHOUT the task calling for it is a REGRESSION. Adopt primitives
    incrementally; a section is migrated only when its existing tests stay green UNMODIFIED (unless the
    task explicitly updates them).
  - **NO new dependency (stricter than a feature milestone).** `web/package.json`,
    `web/package-lock.json`, `go.mod`, `go.sum`, and `sdk/javascript/package.json` MUST be unchanged.
    Icons = an in-repo inline-SVG `Icon` component (NO icon package). Charts = hand-rolled SVG (reuse
    the `Reports.tsx` approach; NO charting lib). Forms = the in-repo `useForm` helper (NO form lib).
    Accessibility is tested with the ALREADY-PRESENT `@testing-library/jest-dom` (`toHaveFocus()`,
    roles, names) — do NOT add `jest-axe` or any a11y package. `npm ls` and `go mod tidy` MUST show no
    additions. A task that seems to need a library is built from in-repo primitives or is out of scope
    as written.
  - **One token source.** All color/space/radius/shadow/typography comes from `web/src/tokens.css`
    `:root` custom properties. After 17.1, NO component `.tsx` hardcodes a hex color — use `var(--…)`.
    Theming is global via `data-theme` on `document.documentElement` + `prefers-color-scheme`.
  - **One of each primitive; no re-implementation.** After a primitive ships in `web/src/components/`,
    sections USE it — they do not hand-roll their own button/tab/badge/card/modal/toast/spinner.
  - **No native dialog / no ad-hoc UX-state after its primitive exists.** After 17.3/17.4: NO
    `window.alert`, NO `window.confirm`, NO inline `<p className="muted">No X</p>` empties, NO
    "Loading…" text — use `Toast`/`ConfirmDialog`/`EmptyState`/`Spinner`/`Skeleton`/`ErrorState`. EVERY
    destructive/irreversible action (delete, revoke, retire, discard, unmerge) routes through
    `ConfirmDialog`.
  - **Accessibility is an exit criterion.** Every interactive element has a visible `:focus-visible`
    ring (replacing `outline:none`, `styles.css:61`). Modals trap focus, restore it to the trigger on
    close, and close on Esc. Icon-only buttons carry an accessible name. Motion is gated by
    `@media (prefers-reduced-motion: reduce)`. Each a11y task ships a `toHaveFocus`/role/name test.
  - **Display-state is projector-only (carried from 17.0 / M11).** Do NOT introduce any writer of
    `inapp_messages` display-state outside the `message.*` `ProjectEvent` cases.
  - **Backend touch is ONE read-only endpoint.** `GET /v1/overview` follows the standard vertical slice
    (domain struct → `ports.Store` method → `internal/postgres` impl → `internal/httpapi` handler →
    route → `web/src/api.ts` wrapper), is tenant-scoped from the principal, guarded by an existing read
    scope (`reports:read`), adds NO migration/table/column, and ships an httpapi fake-store unit test +
    a postgres integration test.
  - New frontend files follow the existing conventions: a section is a default-export `{apiKey,
    baseURL}` component registered via the 6-point `App.tsx` edits; API calls go through `requestJSON`
    in `web/src/api.ts`; tests are co-located `*.test.tsx` using vitest + @testing-library.

## STEP 3 — Verify (MANDATORY — do not mark a task done if any check fails)
- Web changes: `cd web && npm run typecheck && npm run build && npm test` — ALL green. This is the
  primary gate for almost every task.
- Go changes (only the overview endpoint): `go build ./... && go vet ./... && go test ./...`.
- Run `go mod tidy` and confirm `git diff go.mod go.sum web/package.json web/package-lock.json` shows
  NO new dependency.
- The task's literal **"Done when"** condition MUST be observably satisfied. For the load-bearing
  properties (suite-green-and-non-breaking, no-new-dep, focus-visible-on-every-interactive-element,
  modal-traps-and-restores-focus + Esc-close, no-native-dialog-remains, empty/error/toast primitives
  adopted, IDOR/SSRF/projector-only from 17.0) that means an actual test proving the property (a
  `getByRole`/`getByLabelText`/`toHaveFocus` assertion) — not just that it compiles.

## STEP 4 — Record & commit
- Edit `docs/milestones/v1-milestone-12-plan.md`: prefix the finished sub-task with `[x]` and
  append `— done: <one-line evidence>` (match the prior-plan style).
- Ensure you are on branch `phase12` (create it from the current branch if it doesn't exist).
  NEVER commit to `main`.
- Commit ONLY this task's changes with a conventional message, e.g.
  `feat(web): 17.2 add Button/Input/Field primitives`
  or `feat(web): 17.6 add command palette over all views`.
  Follow the repository's commit-trailer convention.

## STEP 5 — Stop
- After committing ONE task, output a 2-line summary (task id + what you did) and STOP.
- If ALL tasks are `[x]`, output `MILESTONE 12 COMPLETE` and STOP.
- If BLOCKED (real ambiguity, a failing build/test you can't fix within this task's scope, a missing
  prerequisite, a needed human approval, or anything that would require weakening an invariant —
  e.g. a new dependency, a red/​broken test suite, a hardcoded hex after tokens exist, a native
  `window.confirm`/`alert` after ConfirmDialog/Toast exist, a modal without focus trap, or breaking an
  existing `getByRole`/`getByLabelText` assertion): do NOT hack around it, do NOT delete/disable a
  failing test, and do NOT mark the task done. Append the blocker to `docs/milestones/BLOCKERS.md`
  (task id + what's blocking + what you need), commit that file only, and STOP.
```

---

**Runner:** loop this mission with a **fresh context each iteration**, capped at some max, and
stop when the output contains `MILESTONE 12 COMPLETE` or a new line lands in
`docs/milestones/BLOCKERS.md`.

**Note:** the M12 tasks are `[ ]` checkboxes. The runner (and `cmd/ralph`) treats "no `[x]` /
no `— done:`" as TODO. Work the first unchecked task each iteration; mark it `[x]` + `— done:` when
its *Done when* condition is observably satisfied — which for this milestone almost always means
`cd web && npm run typecheck && npm run build && npm test` is green.
