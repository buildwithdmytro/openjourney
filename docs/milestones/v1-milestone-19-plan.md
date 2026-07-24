# UX, Stability & Usability Round 2 Plan: Design-System Completion, Worker Resilience, Feedback & Navigation (no new features)

Status: not started. A second **quality milestone** (after M12's design system and M18's security
hardening) — no new product surface. Fixes confirmed UX-consistency, stability, and usability findings
from a three-dimension audit (UX-polish, stability, usability). Every task fixes a **specific, cited
finding** and ships a test/verification; nothing invented. Findings are keyed **X#** (UX-polish), **S#**
(stability), **U#** (usability).

Delivers (all fixes to existing code — zero new features, **zero new dependencies**):
1. **Worker resilience (top fix)** — panic recovery → DLQ on the delivery/journey/dispatcher/projector
   loops (today a single panic crash-loops the whole fleet), plus backoff-on-error and bounded queries.
2. **Frontend resilience** — a root error boundary, a fabricated-sparkline removal, toast-timer/cap fix,
   double-submit guards, and defensive nested-data access.
3. **Feedback & guidance** — success toasts on every mutation (there's exactly ONE today), and actionable
   empty-state CTAs everywhere.
4. **Safety & navigation** — standardize destructive confirms on `ConfirmDialog`, a single source of truth
   for the nav config (deduped across 4 files), a rebalanced nav taxonomy, and a ⌘K palette that runs
   actions and searches by category.
5. **Design-system completion** — finish the M12 adoption the later sections skipped: `PageHeader`,
   `Card`/`DataTable`, the state primitives (`ErrorState`/`Spinner`/`Skeleton`), `Field`-based forms with
   inline validation, and an inline-style/hardcoded-color detox to tokens.
6. **M18 residual closeout** (`24.0`) — the three `_ = s.audit(...)` ignored returns.

This is a **recipe book**, like the Phase 2–18 plans. Every task cites the finding, the fix, and a
**Done when** check. Reuses the M12 `web/src/components/` primitives + existing patterns; no new recipes.

> **This is a quality milestone — correctness and consistency over speed.** Each fix ships a
> test/verification, and NOTHING regresses: the full suite (`go test ./...`, web, sdk) stays green, and UI
> migrations preserve the accessible roles/labels the ~320 web tests assert (a migration that changes a
> role/label without the task calling for it is a regression). Treat `24.1`-green (worker panic recovery →
> DLQ) as the stability checkpoint.

> **`24.0` and `24.1` come first.** `24.0` closes the M18 residual; `24.1` is the highest-leverage
> stability fix (a panic in any worker loop currently halts the entire delivery/journey/projection fleet).

## Design decisions (locked)

1. **Every fix ships a test/verification, and the suite stays green.** Stability fixes get a Go test
   (e.g. a panicking apply is dead-lettered, not crash-looped); UI fixes preserve the role/label
   contract the ~320 web tests assert and add a behavior test where the audit found none.
2. **Reuse the M12 design system — do not invent new components.** All UI work uses the existing
   `web/src/components/` primitives (`PageHeader`, `Card`, `DataTable`, `Field`, `EmptyState`,
   `ErrorState`, `Spinner`, `Skeleton`, `Toast`/`useToast`, `Modal`/`ConfirmDialog`, `useForm`) and
   `tokens.css`. Kill hand-rolled equivalents (raw `<article className="card">`, `<p className="error">`,
   inline "Loading…", the `ui-crash` card, native `confirm()`, hardcoded `#hex`/`rgba()` colors).
3. **Fail safe on stability.** Worker loops recover panics into the existing `attempts≥N → dead` DLQ path
   (never swallow); mutations disable their trigger while in-flight (no double-submit); render paths
   optional-chain fetched data; a root boundary guards the pre-auth chrome.
4. **Single source of truth for navigation.** The `View` union + `viewTitles` + `navGroups` + palette
   items are duplicated across `App.tsx`/`Sidebar.tsx`/`CommandPalette.tsx`/`AppShell.tsx` — consolidate
   into ONE config module so nav can't drift.
5. **No fabricated data shown to users.** Remove the synthesized Assistant sparkline (S/H2); render a
   metric with no real series as the value alone (or a real series).
6. **No new dependency; no feature change.** `go mod tidy` clean; `web/package.json`/`sdk` unchanged;
   `-race` scoped to `internal/postgres/...` (the WASM `-race` timeout is pre-existing).

## 1. Findings → fixes (evidence base)

| # | Sev | Finding | ref | Task |
|---|---|---|---|---|
| S1 | HIGH | Worker loops have NO `recover()` → a panic crash-loops the fleet, bypassing the DLQ | `cmd/campaigns-delivery/main.go:109`, `cmd/journeys-worker/main.go:114`, `cmd/campaigns-dispatcher/main.go:103`, `internal/projector` Drain | 24.1.1 |
| S2 | HIGH | Fabricated sparkline rendered under "Citation-Grounded Metrics" | `web/src/sections/Assistant.tsx:183` | 24.2.3 |
| S3 | MED | Double-submit unguarded on Launch/publish/provision (no in-flight disable) | `App.tsx:1676/1831`, `Messaging.tsx:145`, `Catalogs.tsx:214,352`, `Acquisition.tsx:28`, `Access.tsx:26`, `Connectors.tsx:18`, `Prompts.tsx:403,463`, `Scoring.tsx` | 24.2.4 |
| S4 | MED | Toast auto-dismiss timer resets on every push + no max-visible cap → unbounded stacking | `components/ToastProvider.tsx:40`, `Toast.tsx:13` | 24.2.2 |
| S5 | MED | No root error boundary → a pre-auth/chrome render throw white-screens the app | `web/src/main.tsx:7` | 24.2.1 |
| S6 | MED | Segment resolution `SELECT … FROM profiles` with no LIMIT → OOM on large tenants | `internal/postgres/segments.go:231` | 24.1.2 |
| S7 | MED | `principalFrom` unchecked ctx type assertion → panic if a handler lacks auth mw | `server.go:436,443,471,914` | 24.1.3 |
| S8 | LOW-MED | Reports/Analytics nested access without optional-chaining (contained by boundary) | `Reports.tsx:206`, `Analytics.tsx:149` | 24.2.3 |
| S9 | LOW-MED | Journey/experiment config type assertions without `, ok` | `journeys.go:225`, `experiments.go:509`, `validate.go:35+` | 24.1.3 |
| S10 | LOW-MED | Delivery loop skips backoff on error (`sleep` only when `!processed`) | `campaigns-delivery/main.go:124`, `journeys-worker/main.go:144` | 24.1.1 |
| S11 | LOW | Unbounded short-link list (no LIMIT) | `short_links.go:48` | 24.1.2 |
| U1 | HIGH | Almost no success feedback — exactly ONE success toast app-wide | `App.tsx:753` (only), all mutation handlers | 24.3.1 |
| U2 | HIGH | Empty states rarely guide — only 2 of ~20 pass a `cta`; many bare `<p>No X</p>` | `App.tsx:866,1032,1277,1462,1536,1795`, `Catalogs.tsx:251`, `Messaging.tsx:102`, etc. | 24.3.2 |
| U3 | MED | Destructive confirms inconsistent — native `confirm()` in places; NO confirm on identity unmerge | `Catalogs.tsx:114,215`, `Analytics.tsx:207`, `App.tsx:1237`, `Connectors.tsx:21` | 24.4.1 |
| U4 | MED | "Messaging" nav group overloaded (11 items, mislabeled) + nav config duplicated across 4 files | `Sidebar.tsx:10-39` + 3 files | 24.4.2 |
| U5 | MED | ⌘K palette: `action` field defined but never populated; searches labels only | `CommandPalette.tsx:6,89,100` | 24.4.3 |
| U6 | MED | Connectors "Mapping JSON" is a raw unvalidated `<textarea>` | `Connectors.tsx:25` | 24.4.4 |
| U7 | LOW | Inline validation only in Segments; no section uses `useForm` | (forms across sections) | 24.5.4 |
| U8 | LOW | a11y: mobile drawer `aria-controls` id doesn't exist; palette input label-less | `AppShell.tsx:80`, `PageHeader.tsx:22`, `CommandPalette.tsx:141` | 24.4.4 |
| X1 | MED | `PageHeader` adopted by ZERO sections → 3 competing header styles | all sections | 24.5.1 |
| X2 | MED | State primitives split 3 ways (`ErrorState` 4/19, `Skeleton` 0); hand-rolled error/loading + a `ui-crash` card duplicating `ErrorState` | many sections; `Overview.tsx:62`, `Analytics.tsx:222` | 24.5.2 |
| X3 | MED | Raw `<article className="card">` + legacy `ResourceTable`/`ResourceList`/raw `<table>` instead of `Card`/`DataTable` | ~11 sections + `App.tsx` | 24.5.2, 24.5.3 |
| X4 | MED | Inline-style + hardcoded `#3b82f6`/`rgba()` bypassing tokens | `Copilots.tsx`, `Assistant.tsx`, `Prompts.tsx`, `Journeys.tsx`, `App.tsx:910` | 24.5.1 |
| X5 | MED | Barely/not-migrated sections (Acquisition, Catalogs, Scoring, Experiments, Messaging, Extensions, Connectors) | those files | 24.5.3 |

(Robust, not touched: stale-response race handling in the analytics views; ConfirmDialog double-submit
safety; the `api.ts` defensive layer + chart null-guards; the leased-queue/DLQ design; hardened SSRF/
public edges.)

## 6. Task list

### Milestone 24.0 — M18 residual closeout — DO FIRST
1. [ ] **Propagate the remaining audit-write errors.** The three `_ = s.audit(...)` call sites still
   ignore the (now in-transaction) return; propagate it so a failed audit aborts its mutation.
   *Done when:* no `_ = s.audit(` remains; a forced audit failure aborts the governed mutation; test covers it.

### Milestone 24.1 — Worker & backend resilience — STABILITY CHECKPOINT
1. [ ] **Panic recovery → DLQ on all worker loops (S1, S10).** Wrap the per-message body of
   `campaigns.DeliverNext`/`journey.DeliverNext`/`dispatcher.Drain`/`projector.Drain` (or their call
   sites in `cmd/*/main.go`) in `defer func(){ if r:=recover(); r!=nil { …Fail*Job / mark dead … } }()`
   so a panic dead-letters via the existing `attempts≥N → dead` path instead of crash-looping; also sleep
   on error, not only when `!processed`.
   *Done when:* a test injects a panicking apply/handler and asserts the job is marked `dead` (not an
   infinite re-claim loop); a worker survives a poison message; `go test ./... ` green.
   **Stability checkpoint:** a single bad event can no longer halt the delivery/journey/projection fleet.
2. [ ] **Bound the unbounded queries (S6, S11).** `resolveSegmentIDs` (`segments.go:231`) keyset-paginates
   or pushes the predicate into SQL (no full-table load); `short_links.go:48` list gets a `LIMIT`.
   *Done when:* segment resolution no longer streams the whole profile table into memory (paged/bounded);
   the short-link list is bounded; tests cover the bound.
3. [ ] **Guard the unchecked type assertions (S7, S9).** `principalFrom` (`server.go:436+`) uses `, ok`
   and returns 401 when absent; the journey/experiment config assertions (`journeys.go:225`,
   `experiments.go:509`, `validate.go:35+`) assert with `, ok` and return a validation error.
   *Done when:* a handler without the auth middleware returns 401 (not a panic); a malformed config returns
   a validation error (not a panic); tests cover both.

### Milestone 24.2 — Frontend resilience
1. [ ] **Root error boundary (S5).** Wrap `<App/>` in a top-level error boundary in `web/src/main.tsx`
   so a pre-auth/chrome render throw shows a recovery card, not a white screen.
   *Done when:* a forced throw in the login/chrome renders the boundary fallback (test); the app never
   white-screens.
2. [ ] **Toast timer + cap fix (S4).** Memoize `onDismiss` per id (or depend only on `duration`) so a new
   toast doesn't reset others' countdowns; cap the visible list (last 3–5).
   *Done when:* a burst of toasts each auto-dismiss on their own timers and the visible count is capped;
   test covers the cap + independent dismissal.
3. [ ] **Remove fabricated data + defensive nested access (S2, S8).** Drop the synthesized Assistant
   sparkline (`Assistant.tsx:183`) — render the value alone when no real series exists; optional-chain
   the Reports/Analytics nested access (`Reports.tsx:206`, `Analytics.tsx:149`).
   *Done when:* the Assistant shows no invented trend; a partial report payload does not throw during
   render; tests cover both.
4. [ ] **Double-submit guards (S3).** Add an in-flight `saving` state gating the button on the S3
   handlers (campaign Launch, Messaging/Catalogs/Acquisition/Access/Connectors/Prompts/Scoring create/
   publish) — the pattern already in Copilots/Journeys.
   *Done when:* each listed mutation button disables while in-flight (no double campaign launch / duplicate
   published version / duplicate user); tests cover a representative set.

### Milestone 24.3 — Feedback & guidance
1. [ ] **Success toasts on every mutation (U1).** Every create/update/publish/delete fires a
   `useToast()` success toast (only `App.tsx:753` does today) — a consistent pattern across App.tsx
   handlers and the sections that import `useToast` but don't fire it.
   *Done when:* a representative create/update/delete in App + 3 sections shows a success toast; a test
   asserts the toast via `role="status"`; no silent-success mutation remains in the covered set.
2. [ ] **Actionable empty states (U2).** Every `EmptyState` passes a `cta` (create-your-first-X), and the
   bare `<p>No X</p>` empties (`App.tsx:866,1032,1277,1462,1536,1795`, `Catalogs.tsx:251`, `Messaging`,
   `Journeys`) become `EmptyState` with a CTA.
   *Done when:* no bare `<p>No …</p>` empty remains in the covered sections; each empty has a reachable CTA
   (test asserts the CTA role); the app guides a fresh workspace.

### Milestone 24.4 — Safety & navigation
1. [ ] **Standardize destructive confirms (U3).** Replace native `confirm()` (`Catalogs.tsx:114,215`,
   `Analytics.tsx:207`, `App.tsx:1237`) with `ConfirmDialog`, and ADD a confirm to identity unmerge
   (`Connectors.tsx:21`, an irreversible op fired straight from submit today).
   *Done when:* no `window.confirm` remains; every destructive action (incl. unmerge) routes through
   `ConfirmDialog`; tests cover confirm + cancel for unmerge and one former-native case.
2. [ ] **Single-source nav config + rebalance (U4).** Extract the `View` union + `viewTitles` + `navGroups`
   + palette items into ONE config module consumed by `App.tsx`/`Sidebar.tsx`/`CommandPalette.tsx`/
   `AppShell.tsx`; rebalance the 11-item "Messaging" group into sensible groups (e.g. Messaging vs
   Infrastructure vs Data).
   *Done when:* nav is defined once (no duplication across the 4 files); the overloaded group is split;
   all 29 views still render + route; tests green.
3. [ ] **⌘K actions + category search (U5, U8).** Populate the palette's `action` items (Create template,
   New API key, Publish flag, …) and search by category/keywords, not just labels; give the palette input
   an explicit label; fix the mobile drawer `aria-controls` id mismatch (`AppShell.tsx:80` vs the drawer id).
   *Done when:* ⌘K runs a create action and finds a view by keyword (not just exact label); the drawer's
   `aria-controls` points at the real drawer id; a keyboard-driven test covers an action.
4. [ ] **Validate the Connectors mapping input (U6).** Replace the raw `<textarea>` mapping JSON
   (`Connectors.tsx:25`) with the validating `JsonField`.
   *Done when:* malformed mapping JSON shows an inline error (no silent break); test covers invalid + valid.

### Milestone 24.5 — Design-system completion
1. [ ] **`PageHeader` everywhere + inline-style/token detox (X1, X4).** Adopt `PageHeader` in every section
   (collapse the 3 header styles); replace hardcoded `#3b82f6`/`rgba()`/magic-px inline styles with
   `tokens.css` vars in the heavy offenders (`Copilots`, `Assistant`, `Prompts`, `Journeys`, `App:910`).
   *Done when:* every section header is `PageHeader`; a grep shows no hardcoded hex/`rgba(` in the covered
   `.tsx`; `cd web && npm run typecheck && npm run build && npm test` green (unchanged role/label tests).
2. [ ] **Unify UX-state primitives (X2, X3).** Replace hand-rolled `<p className="error">`, inline
   "Loading…", and the `ui-crash` card with `ErrorState`/`Spinner`/`Skeleton` across the sections that
   hand-roll them (Acquisition, Catalogs, Experiments, Messaging, Extensions, FeatureFlags, Journeys,
   Scoring, App).
   *Done when:* no hand-rolled error/loading/`ui-crash` markup remains in the covered sections; loading
   uses `Spinner`/`Skeleton`; tests green.
3. [ ] **`Card`/`DataTable` adoption for the least-migrated sections (X3, X5).** Migrate Acquisition,
   Catalogs, Scoring, Experiments, Messaging, Extensions, Connectors to `Card` (from raw
   `<article className="card">`) and `DataTable` (from raw `<table>`/legacy `ResourceList`); replace the
   legacy `ResourceTable` in `App.tsx` with `DataTable`.
   *Done when:* the covered sections use `Card`/`DataTable`; no legacy `ResourceTable`/`ResourceList`
   remains; no raw `<table>` in the covered sections; visual parity; tests green.
4. [ ] **Field-based forms + inline validation (U7).** Migrate the raw-form sections (Acquisition,
   Catalogs, Scoring, Experiments, Messaging, Extensions) to `Field`-wrapped inputs and adopt `useForm`
   inline validation + disabled-until-valid on 2–3 representative forms (drop the imperative
   throw-on-submit).
   *Done when:* the covered forms use `Field`; ≥2 forms gate submit on validity with inline per-field
   errors; a test asserts an inline validation message; tests green.

### Milestone 24.6 — Integration & audit closeout
1. [ ] **Run the suite**: `go build ./... && go vet ./... && go test ./...` + `go test -race
   ./internal/postgres/...`, `go mod tidy` (no new dep), `cd web && npm run typecheck && npm run build &&
   npm test`, `cd sdk/javascript && npm run build && npm test`.
   *Done when:* all green and `git diff go.mod go.sum web/package.json web/package-lock.json
   sdk/javascript/package.json` is empty of additions.
2. [ ] **Audit doc** `docs/milestones/v1-milestone-19-audit.md` in the prior format, one row per `24.x`
   task mapping the finding(s) (X#/S#/U#) → fix → verifying test.
   *Done when:* the doc exists with a row per task, each citing its finding and test.

## 7. Carry-over hazards & invariants

1. **No regression: the suite stays green and UI migrations preserve roles/labels.** The ~320 web tests
   assert accessible roles/labels — a migration that changes them without the task saying so is a bug.
2. **Stability fails safe.** Worker panics dead-letter (never swallow); mutations disable in-flight; render
   paths optional-chain; a root boundary guards the chrome. No fabricated data is shown.
3. **Reuse the M12 design system.** No new components; kill hand-rolled equivalents (raw card/error/loading,
   native `confirm()`, hardcoded colors). One nav config, not four.
4. **Every fix ships a test/verification.** A quality task whose test doesn't exercise the finding is
   incomplete.
5. **No new dependency; no feature change.** `go mod tidy` clean; web/sdk unchanged; `-race` scoped to
   `internal/postgres/...`.
6. **The M18 residual (`24.0`) lands first**, then the worker-resilience checkpoint (`24.1`).

## 8. Open items to confirm before coding

1. **Design-system adoption depth.** v1 of this milestone migrates the least-migrated sections + the
   cross-cutting primitives (PageHeader/state/Card/DataTable) but may not perfect every one of the 16
   files. Confirm "cover the high-visibility gaps" vs "100% of every section" (the latter is a much larger
   sweep).
2. **Nav taxonomy.** Confirm the rebalanced groups (e.g. split "Messaging" into Messaging + Infrastructure
   [connectors/extensions] + Data [catalogs/schemas]).
3. **Worker recovery granularity.** Per-message `recover()` → `Fail*Job` (dead-letter the one message) vs a
   loop-level recover that logs + continues. Defaulting to per-message dead-letter (uses the existing DLQ).
4. **Success-toast coverage.** Every mutation vs the high-frequency ones. Defaulting to every create/
   update/publish/delete in the covered sections.
