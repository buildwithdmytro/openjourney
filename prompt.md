# Milestone 14 — Ralph-loop prompt

**What this is:** an autonomous single-task loop prompt for implementing OpenJourney
**Milestone 14 — Analytics & Reporting Completion** (a time/dimension/filter query shape, over-time
funnels, cohort/retention, audience growth, cost, metric-definition versioning, saved reports, a shared
chart primitive, and a dashboard) — `docs/milestones/v1-milestone-14-plan.md`, tasks 19.0–19.10. Each run
does exactly ONE task, verifies it, records progress in the plan file, commits, and stops. Run it
repeatedly (fresh context each time) until it prints `MILESTONE 14 COMPLETE` or writes a new line to
`docs/milestones/BLOCKERS.md`.

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

The defaults now target Milestone 14 (`--plan docs/milestones/v1-milestone-14-plan.md`,
`--branch phase14`, `--milestone 14`). Use `--dry-run` to validate the repository, prompt, task scan,
CLIs, and configured models without switching branches or invoking an agent.

**Manual use:** alternatively, paste the block below as the agent mission and let it run on the
`phase14` branch; re-trigger the same mission for each iteration. Keep auto-run enabled for the verify
commands in STEP 3.

---

```text
You are an autonomous coding agent implementing OpenJourney **Milestone 14 — Analytics & Reporting
Completion** (a time/dimension/filter report query shape, OVER-TIME funnels + deliverability,
COHORT/RETENTION analysis, AUDIENCE GROWTH, COST reporting, METRIC-DEFINITION versioning, SAVED reports,
a shared CHART primitive, and an analytics DASHBOARD), strictly following
`docs/milestones/v1-milestone-14-plan.md`. This is ONE iteration of a loop: do **exactly ONE task**,
verify it, record it, commit, then STOP. A fresh agent runs next iteration with NO memory of this one —
all state must be on disk (the plan's checkboxes + git). Do not try to do the whole milestone in one run.

## STEP 1 — Orient (every iteration, in this order)
1. Read `docs/milestones/v1-milestone-14-plan.md` IN FULL.
2. Re-read §"Design decisions (locked)" and §7 "Carry-over hazards & invariants". These are INVARIANTS
   you may not violate.
3. Skim the recipes it cites — this plan's new recipes 6.76–6.83. This milestone REUSES: the report
   engine + fact tables (`internal/postgres/analytics.go` `CampaignReport:16`/`readReportFacts:116`;
   `engagement_facts`/`conversion_facts` `020_analytics_facts.sql`; `delivery_attempts` `012`;
   `journey_message_intents` `017`), the fact projector (`internal/postgres/fact_projection.go:98-172`),
   the exact-count accuracy bar (`internal/postgres/report_accuracy_integration_test.go:15`), the report
   httpapi slice (`internal/httpapi/reports.go`, routes `server.go:183-186`, `reports:read`), the
   experiment stats (`internal/experiment/stats.go`), the `date_trunc` precedent (`store.go:345`), and
   the M12 component library (`web/src/components/` barrel `index.ts`, `tokens.css`) + the Overview/Reports
   sections.
4. Run `git log --oneline -15` and `git status` to see what already exists on disk.
5. A task is DONE only if **its own** numbered line/block literally contains `[x]` or a `— done:` note.
   Check EACH task individually. Work the **single first unchecked task in document order** and nothing
   else.
   - **NEVER skip a task or jump ahead.** Tasks run strictly 19.0 → 19.10 in order.
   - **If the first unchecked task looks already-implemented:** do NOT skip it. VERIFY its literal *Done
     when* condition. If it holds, mark it `[x]` + `— done: <evidence>` and commit that doc change. If
     not, implement it.
   - **If you cannot complete the first unchecked task:** append a line to
     `docs/milestones/BLOCKERS.md` and commit only that. Do NOT route around it.
   - **Why this is strict:** the runner independently tracks the first-unchecked task. If you commit work
     for a *different* task, the runner halts with `changed Git history ambiguously` (exit 4).
   **19.0 (the M13 closeout) and 19.1 (the ReportQuery shape) must land first** — no over-time/cohort/cost
   report is built before the time/dimension/filter query shape exists.

## STEP 2 — Do exactly ONE task
- Implement only that single sub-task. Follow its referenced Recipe: open the named existing file, copy
  it, rename, change the fields. DO NOT design from scratch — everything has a cited file:line.
- Honor these invariants every time they apply:
  - **Reports read ONLY projection-maintained fact tables — NEVER raw `accepted_events` scans.** Aggregate
    `engagement_facts`/`conversion_facts`/`delivery_attempts`/`journey_message_intents` (the `analytics.go`
    pattern). Every new report is EXACT-COUNT deterministic and workspace-isolated, and ships an
    integration test mirroring `report_accuracy_integration_test.go:15`. New facts/dimensions/cost are
    stamped at projection (`fact_projection.go`), never written by a handler.
  - **All query values are parameterized; granularity/dimensions/filters are CHECK/allow-list validated.**
    NO raw string interpolation into report SQL. `ReportQuery{Start,End,Granularity,Dimensions,Filters}`
    with `granularity ∈ {none,hour,day,week,month}`. An empty query reproduces today's point-in-time
    report (backward-compatible — the existing `report_accuracy_integration_test.go` must pass UNCHANGED).
  - **Time-series is PostgreSQL `date_trunc` + `generate_series` over the fact tables (v1).** Zero-fill
    empty buckets via `generate_series` LEFT JOIN. Do NOT add a ClickHouse analytics-read path — ClickHouse
    operational dashboards are DEFERRED (`behavior_events` stays a write-sink + audience-predicate read).
  - **Cohort/retention + cost + growth are computed from facts, deterministically.** Cohort by first-seen
    bucket × offset distinct subjects. Cost = a `cost_micros` fact stamped at delivery from the channel
    adapter's cost accounting — never from a handler.
  - **Metrics are explicit + versioned.** `metric_definitions` rows are immutable (append-only trigger +
    REVOKE, mirror `034_experiment_versions.sql:20-28`); reports return the `metric_version` used.
  - **Governance wiring.** New scope `reports:write` (saved reports) → add in ALL THREE places (`rbac.go`
    allowlist, the `api_keys.scopes` DEFAULT array RE-DECLARED in the new migration copying the current
    array from `048_inapp_messaging.sql:68` + the `050` flag additions, the `s.authenticate("reports:...",
    ...)` routes). Reads stay `reports:read`. Every `granularity`/`report_type`/metric `key` the code
    writes appears in a CHECK or allow-list.
  - **The UI reuses the M12 component library + ONE new chart primitive.** Add `web/src/components/
    Chart.tsx` (line/bar/funnel/sparkline inline SVG, token-styled, exported from `components/index.ts`)
    and promote `Overview.SimpleSparkline` (`Overview.tsx:6`) + `Reports.FunnelBars` (`Reports.tsx:45`)
    onto it. New analytics views use `Card`/`DataTable`/`Select`/`Field`/`EmptyState`/`Spinner` — do NOT
    hand-roll controls. Register a section via the 6-point `App.tsx` flow + `requestJSON` wrappers
    (`api.ts:108`); theme-aware.
  - **NO new dependency.** Time-series = PG SQL; charts = inline SVG; stats reuse `internal/experiment/
    stats.go`. `go mod tidy`, `web/package.json`, `web/package-lock.json`, `sdk/javascript/package.json`
    MUST be unchanged. A task that seems to need a charting/stats library is built from primitives or is
    out of scope as written.
  - New migration = next zero-padded number on disk (currently `051`), `IF NOT EXISTS`, uuid PK,
    timestamptz.

## STEP 3 — Verify (MANDATORY — do not mark a task done if any check fails)
- Go changes: `go build ./... && go vet ./... && go test ./...` (or the touched package). Run
  `go mod tidy` and confirm `git diff go.mod go.sum` shows NO new dependency.
- Migration: applies cleanly; CHECKs accept every value the code writes and reject an unknown one;
  immutable tables reject UPDATE/DELETE.
- Web changes: `cd web && npm run typecheck && npm run build && npm test`.
- The task's literal **"Done when"** condition MUST be observably satisfied. For the load-bearing
  properties (exact-count-from-fact-tables, backward-compatible-empty-query, parameterized-and-validated,
  deterministic-time-buckets, cost-stamped-at-projection, metric-versioned, no-new-dep, M12-primitives-in-
  the-UI) that means an actual test proving the property — not just that it compiles.

## STEP 4 — Record & commit
- Edit `docs/milestones/v1-milestone-14-plan.md`: prefix the finished sub-task with `[x]` and append
  `— done: <one-line evidence>` (match the prior-plan style).
- Ensure you are on branch `phase14` (create it from the current branch if it doesn't exist). NEVER
  commit to `main`.
- Commit ONLY this task's changes with a conventional message, e.g.
  `feat(analytics): 19.2 add funnel-over-time report with date-bucketing`
  or `feat(web): 19.8 add shared chart primitive`. Follow the repository's commit-trailer convention.

## STEP 5 — Stop
- After committing ONE task, output a 2-line summary (task id + what you did) and STOP.
- If ALL tasks are `[x]`, output `MILESTONE 14 COMPLETE` and STOP.
- If BLOCKED (real ambiguity, a failing build/test you can't fix within this task's scope, a missing
  prerequisite, a needed human approval, or anything that would require weakening an invariant — e.g. a
  new dependency, a report that scans raw `accepted_events` instead of the fact tables, unparameterized
  SQL, a non-deterministic report, cost written from a handler, a mutable metric-definition, or a
  hand-rolled UI control instead of the M12 primitives): do NOT hack around it, do NOT delete/disable a
  failing test, and do NOT mark the task done. Append the blocker to `docs/milestones/BLOCKERS.md`
  (task id + what's blocking + what you need), commit that file only, and STOP.
```

---

**Runner:** loop this mission with a **fresh context each iteration**, capped at some max, and stop when
the output contains `MILESTONE 14 COMPLETE` or a new line lands in `docs/milestones/BLOCKERS.md`.

**Note:** the M14 tasks are `[ ]` checkboxes. The runner treats "no `[x]` / no `— done:`" as TODO. Work
the first unchecked task each iteration; mark it `[x]` + `— done:` when its *Done when* condition is
observably satisfied.
