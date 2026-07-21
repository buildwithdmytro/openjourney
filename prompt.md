# Milestone 15 — Ralph-loop prompt

**What this is:** an autonomous single-task loop prompt for implementing OpenJourney
**Milestone 15 — Catalogs & Connected Content** (reference-data catalogs, a render-context seam,
send-time catalog lookups, and governed SSRF-safe external data fetch) —
`docs/milestones/v1-milestone-15-plan.md`, tasks 20.0–20.9. Each run does exactly ONE task, verifies it,
records progress in the plan file, commits, and stops. Run it repeatedly (fresh context each time) until
it prints `MILESTONE 15 COMPLETE` or writes a new line to `docs/milestones/BLOCKERS.md`.

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

The defaults now target Milestone 15 (`--plan docs/milestones/v1-milestone-15-plan.md`,
`--branch phase15`, `--milestone 15`). Use `--dry-run` to validate the repository, prompt, task scan,
CLIs, and configured models without switching branches or invoking an agent.

**Manual use:** alternatively, paste the block below as the agent mission and let it run on the
`phase15` branch; re-trigger the same mission for each iteration. Keep auto-run enabled for the verify
commands in STEP 3.

---

```text
You are an autonomous coding agent implementing OpenJourney **Milestone 15 — Catalogs & Connected
Content** (reference-data CATALOGS for personalization, a RENDER-CONTEXT SEAM, a send-time CATALOG-LOOKUP
filter, governed CONNECTED CONTENT sources, and an SSRF-safe send-time external-data-fetch tag), strictly
following `docs/milestones/v1-milestone-15-plan.md`. This is ONE iteration of a loop: do **exactly ONE
task**, verify it, record it, commit, then STOP. A fresh agent runs next iteration with NO memory of this
one — all state must be on disk (the plan's checkboxes + git). Do not try to do the whole milestone in one
run.

## STEP 1 — Orient (every iteration, in this order)
1. Read `docs/milestones/v1-milestone-15-plan.md` IN FULL.
2. Re-read §"Design decisions (locked)" and §7 "Carry-over hazards & invariants". These are INVARIANTS
   you may not violate.
3. Skim the recipes it cites — this plan's new recipes 6.84–6.91. This milestone REUSES: the Liquid render
   engine (`internal/render/render.go` — `Render:10`, `NewEngine:33`, `RegisterFilter:21`, `RegisterTag:27`,
   the disabled-`include` tag pattern `:36`), the extension template-function shape
   (`internal/extension/template.go:59` tag / `:82` filter), the SSRF-guarded egress
   (`channels.IsSafeURL`/`IsPrivateIP` `internal/channels/webhook.go:280/239`; the guarded transport
   `internal/channels/httpprovider.go:48-92`), the `*_ref` secret convention
   (`internal/extension/resolver.go:44,65`; `security.go:11,61`), the M9 bounding/audit pattern
   (`internal/extension/host.go:232` timeout, `:150` circuit breaker, `:374` audit), the standard CRUD
   slice (`saved_reports`: `internal/postgres/saved_reports.go`, ports `store.go:205-208`, routes
   `server.go:187-190`), the send-time render sites (`internal/campaigns/deliver.go:255-292`,
   `internal/journey/deliver.go:328-380`, `internal/httpapi/templates.go:171-236`), and the M12 component
   library (`web/src/components/`, `Acquisition.tsx` tabbed list+detail+bulk-upload).
4. Run `git log --oneline -15` and `git status` to see what already exists on disk.
5. A task is DONE only if **its own** numbered line/block literally contains `[x]` or a `— done:` note.
   Check EACH task individually. Work the **single first unchecked task in document order** and nothing
   else.
   - **NEVER skip a task or jump ahead.** Tasks run strictly 20.0 → 20.9 in order.
   - **If the first unchecked task looks already-implemented:** do NOT skip it. VERIFY its literal *Done
     when* condition. If it holds, mark it `[x]` + `— done: <evidence>` and commit that doc change. If
     not, implement it.
   - **If you cannot complete the first unchecked task:** append a line to
     `docs/milestones/BLOCKERS.md` and commit only that. Do NOT route around it.
   - **Why this is strict:** the runner independently tracks the first-unchecked task. If you commit work
     for a *different* task, the runner halts with `changed Git history ambiguously` (exit 4).
   **20.0 (M14 closeout), 20.1 (catalog foundation), and 20.4 (the render-context seam) are foundational**
   — the catalog filter (20.5) and connected-content tag (20.7) cannot work until the seam threads
   store/principal/ctx into the render engine. Document order handles this; do not reorder.

## STEP 2 — Do exactly ONE task
- Implement only that single sub-task. Follow its referenced Recipe: open the named existing file, copy
  it, rename, change the fields. DO NOT design from scratch — everything has a cited file:line.
- Honor these invariants every time they apply:
  - **Connected Content is a send-time SSRF surface — allowlisted, authed, bounded, cached, audited,
    NEVER free-form.** The fetch URL host MUST match an enabled `connected_content_sources` row; the
    transport reuses the dial-time IP guard (`channels.IsSafeURL`/`IsPrivateIP`, mirror
    `httpprovider.go:48-92` — validate ALL resolved IPs, dial the first verified IP, re-check redirects);
    auth is `*_ref` only (`ResolveConfigMap` `resolver.go:65`); every fetch is timeout- + circuit-breaker-
    bounded and audited (mirror `host.go:374`, payload redacted). A Liquid tag must NEVER dial an
    arbitrary or private/loopback/CGNAT host.
  - **Render failures degrade to a FALLBACK — never fail the send.** A missing catalog item, a blocked/
    unlisted/disabled source, or an auth/timeout/circuit-open fetch returns empty/default + an audit row,
    NOT a render error that fails the whole delivery (mirror the extension template fallback
    `template.go:68,89`).
  - **Reference data is store-read; bulk load is a direct batched INSERT — NOT `AcceptEvents`.** Catalog
    items upsert by `(catalog_id, item_key)` via chunked multi-row `INSERT ... ON CONFLICT DO UPDATE`
    (idiom `052_metric_definitions.sql:32-42`); re-upload is idempotent. Do NOT route reference data
    through the event pipeline.
  - **Caching is mandatory + bounded + race-free.** Render runs per-recipient/per-field
    (`deliver.go:262+`); catalog lookups and connected-content responses go through the bounded TTL cache
    (`internal/render/cache.go`) or a large send fans out to N lookups/fetches. `go test -race` must pass.
  - **Secrets are `*_ref` only** (`resolver.go:44`, `security.go:11`), redacted on read (`security.go:61`).
    Publish/enable of a connected-content source is human-actor-gated (`isHuman`, `identity.go:85`).
  - **The render-context seam is backward-compatible.** Add `render.RenderWithContext(ctx, tmpl, vars,
    deps)`; the bare `render.Render(tmpl, vars)` (`render.go:10`) keeps working (no deps = today's
    profile-attribute rendering, unchanged). Register the filter/tag like `template.go:59,82`.
  - **Governance wiring — new scopes in FOUR places.** `catalogs:read`/`catalogs:write` → (a) `rbac.go:12-32`
    `allowedPermissions`, (b) the `api_keys.scopes` DEFAULT array RE-DECLARED in the new migration copying
    the live default from `052_metric_definitions.sql:45-63`, (c) the `s.authenticate("catalogs:...", ...)`
    route guards, AND (d) `web/src/App.tsx:102 AVAILABLE_SCOPES`. The `View` union is duplicated in
    `web/src/App.tsx:65` AND `web/src/components/Sidebar.tsx:3` — update BOTH. Every `status` the code
    writes appears in a CHECK.
  - **NO new dependency.** Render = existing `osteele/liquid`; fetch = the SSRF guard + stdlib `net/http`;
    cache = stdlib `sync`; UI = the M12 `web/src/components/` library (`DataTable`/`JsonField`/`Field`/
    `Modal`/`ConfirmDialog`/`Toast`) — do NOT hand-roll controls. `go mod tidy`, `web/package.json`,
    `web/package-lock.json`, `sdk/javascript/package.json` MUST be unchanged. A task that seems to need a
    library is built from primitives or is out of scope as written.
  - New migration = next zero-padded number on disk (currently `054`), `IF NOT EXISTS`, uuid PK,
    timestamptz.

## STEP 3 — Verify (MANDATORY — do not mark a task done if any check fails)
- Go changes: `go build ./... && go vet ./... && go test ./...` (add `-race` for the cache/fetcher tasks).
  Run `go mod tidy` and confirm `git diff go.mod go.sum` shows NO new dependency.
- Migration: applies cleanly; CHECKs accept every value the code writes and reject an unknown one;
  `UNIQUE(catalog_id,item_key)` holds.
- Web changes: `cd web && npm run typecheck && npm run build && npm test`.
- The task's literal **"Done when"** condition MUST be observably satisfied. For the load-bearing
  properties (SSRF-blocked-and-allowlisted connected content, render-degrades-to-fallback,
  idempotent-bulk-upsert, TTL-cache-bounded-and-race-free, secret-ref-only, backward-compatible-render-seam,
  no-new-dep, M12-primitives-in-the-UI) that means an actual test proving the property — not just that it
  compiles.

## STEP 4 — Record & commit
- Edit `docs/milestones/v1-milestone-15-plan.md`: prefix the finished sub-task with `[x]` and append
  `— done: <one-line evidence>` (match the prior-plan style).
- Ensure you are on branch `phase15` (create it from the current branch if it doesn't exist). NEVER
  commit to `main`.
- Commit ONLY this task's changes with a conventional message, e.g.
  `feat(catalogs): 20.4 add render-context seam threading store + principal`
  or `feat(catalogs): 20.7 add SSRF-safe connected_content tag`. Follow the repository's commit-trailer
  convention.

## STEP 5 — Stop
- After committing ONE task, output a 2-line summary (task id + what you did) and STOP.
- If ALL tasks are `[x]`, output `MILESTONE 15 COMPLETE` and STOP.
- If BLOCKED (real ambiguity, a failing build/test you can't fix within this task's scope, a missing
  prerequisite, a needed human approval, or anything that would require weakening an invariant — e.g. a
  new dependency, a connected-content fetch that isn't SSRF-guarded or allowlisted, a render error that
  fails the send instead of degrading, reference data routed through `AcceptEvents`, a raw secret in a
  source config, an unbounded/un-race-safe cache, or a hand-rolled UI control instead of the M12
  primitives): do NOT hack around it, do NOT delete/disable a failing test, and do NOT mark the task done.
  Append the blocker to `docs/milestones/BLOCKERS.md` (task id + what's blocking + what you need), commit
  that file only, and STOP.
```

---

**Runner:** loop this mission with a **fresh context each iteration**, capped at some max, and stop when
the output contains `MILESTONE 15 COMPLETE` or a new line lands in `docs/milestones/BLOCKERS.md`.

**Note:** the M15 tasks are `[ ]` checkboxes. The runner treats "no `[x]` / no `— done:`" as TODO. Work
the first unchecked task each iteration; mark it `[x]` + `— done:` when its *Done when* condition is
observably satisfied.
