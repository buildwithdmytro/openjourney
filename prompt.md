# Milestone 11 — Ralph-loop prompt

**What this is:** an autonomous single-task loop prompt for implementing OpenJourney
**Milestone 11 — In-App & Web Messaging** (an inbox channel delivered through the existing pipeline,
content cards, web push as a wake signal, the public client SDK contract, and the M10 security
closeout) — `docs/milestones/v1-milestone-11-plan.md`, tasks 16.0–16.11. Each run does exactly ONE
task, verifies it, records progress in the plan file, commits, and stops. Run it repeatedly (fresh
context each time) until it prints `MILESTONE 11 COMPLETE` or writes a new line to
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

The defaults now target Milestone 11 (`--plan docs/milestones/v1-milestone-11-plan.md`,
`--branch phase11`, `--milestone 11`). Run `go run ./cmd/ralph --help` for model, timeout, iteration,
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
catalog. The Antigravity default is `Gemini 3.5 Flash (Medium)`. The Claude Code default is `haiku`
(the cheapest model); it runs in `--print` mode with `--dangerously-skip-permissions`, and its
preflight only checks that the `claude` CLI is present (model aliases resolve at runtime).

**Manual Antigravity use:** alternatively, paste the block below as the agent mission and let it
run on the `phase11` branch; re-trigger the same mission for each iteration. Keep auto-run enabled
for the verify commands in STEP 3.

---

```text
You are an autonomous coding agent implementing OpenJourney **Milestone 11 — In-App & Web Messaging**
(an in-app / inbox CHANNEL delivered through the existing message pipeline, CONTENT CARDS, WEB PUSH as
a VAPID wake signal, the PUBLIC client SDK contract, and the M10 security closeout), strictly following
`docs/milestones/v1-milestone-11-plan.md`. This is ONE iteration of a loop: do **exactly ONE task**,
verify it, record it, commit, then STOP. A fresh agent runs next iteration with NO memory of this one —
all state must be on disk (the plan's checkboxes + git). Do not try to do the whole milestone in one
run.

## STEP 1 — Orient (every iteration, in this order)
1. Read `docs/milestones/v1-milestone-11-plan.md` IN FULL.
2. Re-read §"Design decisions (locked)" and §7 "Carry-over hazards & invariants". These are
   INVARIANTS you may not violate.
3. Skim the recipes it cites — 6.1–6.51 from prior plans (`v1-milestone-2..10-plan.md`) and this
   plan's new recipes 6.52–6.57. This milestone REUSES far more than it builds: the channel adapter
   `Registry` (`internal/channels/registry.go`), the `ports.ChannelAdapter` contract
   (`internal/ports/adapter.go`), the delivery state machine + both `DeliverNext` workers
   (`internal/campaigns/deliver.go`, `internal/journey/deliver.go`), `policy.Evaluate`
   (`internal/policy/policy.go`, suppression/consent/fatigue), Liquid `render.Render`, the public
   HMAC-token edge pattern (`internal/httpapi/form_submit.go` + `publicguard.go`), `Store.AcceptEvents`
   + the projector `ProjectEvent` switch, and the M10 identity/connector code (for the 16.0 fixes).
4. Run `git log --oneline -15` and `git status` to see what already exists on disk.
5. A task is DONE only if **its own** numbered line/block literally contains `[x]` or a `— done:`
   note. Check EACH task individually — a neighbouring task being done (e.g. `16.1.1` and `16.1.3`
   marked done) does **NOT** make the one between them (`16.1.2`) done. Work the **single first
   unchecked task in document order** and nothing else.
   - **NEVER skip a task or jump ahead.** Do not implement a later task while an earlier one is
     unchecked — not even if the later one looks easier or more "real". Tasks run strictly
     16.0 → 16.11 in order.
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
   **16.0 (the M10 security closeout) and 16.1 (in-app channel foundation: migration + store +
   adapter) must land first** — 16.0 fixes pre-existing M10 holes (an unaudited/irreversible multi-way
   merge, a ClickHouse-sink DNS-rebinding SSRF, overlapping scheduler runs) that the new event/identity
   paths would inherit, and no in-app message can be sent before 16.1's governed channel foundation
   exists. Document order already enforces this; do not reorder.

## STEP 2 — Do exactly ONE task
- Implement only that single sub-task. Follow its referenced Recipe: open the named existing file,
  copy it, rename, change the fields. DO NOT design from scratch or invent patterns — everything you
  need has a cited file:line in the plan.
- Honor these invariants every time they apply:
  - **Display-state is written ONLY by the projector.** The `inapp` channel adapter's `Send` is the
    ONLY creator of `inapp_messages` rows (the delivery); the `message.impression`/`message.clicked`/
    `message.dismissed` `ProjectEvent` cases are the ONLY mutators of `displayed_at`/`clicked_at`/
    `dismissed_at`/`status`. NO HTTP handler writes display-state directly — mirrors M10's
    "nothing writes profiles/identity_* outside the projector" rule.
  - **In-app rides the shared pipeline unchanged.** Do NOT special-case `channel=='in_app'` inside
    `DeliverNext`. In-app resolves `provider='inapp'` through the registry like every channel; the
    delivery state machine, `RenderedMessage`, render, and `policy.Evaluate` are UNTOUCHED. The adapter
    inherits `(tenant,workspace,app)` atomically from the target profile (`SELECT ... FROM profiles
    WHERE id=$endpoint`) because `SendingIdentity` has no `app_id`.
  - **The client edge is PUBLIC and IDOR-safe.** Anonymous inbox = high-entropy `anonymous_id`;
    known-subject (`external_id`) inbox REQUIRES a server-minted `SignInAppToken` (mirror
    `SignFormToken`, `publicguard.go:107`). A public key + a guessable `external_id` WITHOUT a valid
    token must NOT read or report on another user's inbox. Rate-limit via `IPRateLimiter`; derive the
    principal from the app, never from attacker JSON.
  - **Engagement is event-sourced.** Reports emit `message.*` accepted events via `Store.AcceptEvents`;
    the projector stamps display-state. `accepted_events` has no `event_type` CHECK, so new event types
    need only projector cases. Replays are idempotent at the event layer; state transitions are
    monotonic (never regress a later state).
  - **Web push v1 is a VAPID wake signal — no payload crypto, no new dependency.** It is a
    `provider='webpush'` UNDER the existing `push` channel (NOT a new channel), signed with a stdlib
    `crypto/ecdsa` P-256 JWT, delivered with no body; its Service Worker fetches content from the
    in-app inbox edge. Do NOT add a web-push or payload-encryption library; encrypted RFC 8291 payloads
    are out of scope as written.
  - **Frequency capping, consent, and suppression apply — no new bypass.** In-app rides
    `policy.Evaluate`: marketing in-app is fatigue-counted (`SentCountSince`, cross-channel);
    transactional in-app routes suppression-only (`deliver.go:261`); suppression is keyed by
    `(tenant,'in_app',profile_id)` (`suppressions.channel` is free text — no schema change); an
    unsubscribe suppresses future in-app.
  - **Governance wiring.** New scope (`messages:read`/`messages:write`) → add in ALL THREE places
    (`rbac.go` allowlist, the `api_keys` DEFAULT array RE-DECLARED in the new migration copying the
    current array from `043_connectors.sql:67-83`, the `s.authenticate("messages:...", ...)` routes).
    New channel/provider value (`in_app`/`inapp`/`webpush`) → widen the relevant CHECK (DROP/ADD, mirror
    `022_sms_push_channels.sql`). Enumerate EVERY value the code writes in any CHECK.
  - **M10 closeout (16.0) reconciles with RTBF erasure.** The `identity_merges` append-only trigger
    permits DELETE only when the erasure GUC is set (`SET LOCAL openjourney.erasure='on'` in the
    `operations.go:210` region); do NOT blanket-`REVOKE DELETE` (that breaks right-to-erasure). Every
    loser in a multi-way merge gets its own `identity_merges` row (unique key
    `(source_event_id, source_profile_id)`), and unmerge selects the LIVE merge (`undone_at IS NULL
    ORDER BY merged_at DESC LIMIT 1`).
  - **NO new dependency.** `go mod tidy` MUST show no additions; `web/package.json` and
    `sdk/javascript/package.json` unchanged. In-app is stdlib + existing render/store; web-push VAPID is
    stdlib crypto. A task that seems to need a library is the wake-signal / row-writing model as written.
  - New migration = next zero-padded number on disk (currently `047` then `048` — always use the next
    available, do not hard-code), `IF NOT EXISTS`, uuid PK, timestamptz.
  - New frontend: follow the existing section/editor patterns (the 6-point `web/src/App.tsx`
    registration + `requestJSON` wrappers in `web/src/api.ts`); add NO new npm dependency; theme-aware.

## STEP 3 — Verify (MANDATORY — do not mark a task done if any check fails)
- Go changes: `go build ./... && go vet ./... && go test ./...` (or the touched package).
  Run `go mod tidy` and confirm `git diff go.mod go.sum` shows NO new dependency.
- Migration: confirm it applies cleanly against a test DB, and that any widened/added CHECK ACCEPTS
  every value the code writes AND still REJECTS an unknown one.
- UI changes: `cd web && npm run typecheck && npm run build && npm test`.
- SDK changes: `cd sdk/javascript && npm run build && npm test`.
- The task's literal **"Done when"** condition MUST be observably satisfied. For the load-bearing
  properties (in-app-delivered-through-the-shared-pipeline, IDOR-safe-inbox-fetch/report,
  display-state-only-in-projector, engagement-event-sourced-and-idempotent, suppression/consent/fatigue
  enforced, VAPID-wake-signal-SSRF-safe, scopes-enforced, and the 16.0 fixes: merge-audit-reversible,
  ClickHouse-sink-rebind-blocked, scheduler-no-overlap, raw-secret-rejected) that means an actual test
  proving the property — not just `err == nil`.

## STEP 4 — Record & commit
- Edit `docs/milestones/v1-milestone-11-plan.md`: prefix the finished sub-task with `[x]` and
  append `— done: <one-line evidence>` (match the prior-plan style).
- Ensure you are on branch `phase11` (create it from the current branch if it doesn't exist).
  NEVER commit to `main`.
- Commit ONLY this task's changes with a conventional message, e.g.
  `feat(messaging): 16.1 add in-app channel adapter and inbox store`
  or `fix(identity): 16.0 record every loser in a multi-way merge`.
  Follow the repository's commit-trailer convention.

## STEP 5 — Stop
- After committing ONE task, output a 2-line summary (task id + what you did) and STOP.
- If ALL tasks are `[x]`, output `MILESTONE 11 COMPLETE` and STOP.
- If BLOCKED (real ambiguity, a failing build you can't fix within this task's scope, a missing
  prerequisite, a needed human approval, or anything that would require weakening an invariant —
  e.g. a display-state write outside the projector, a special-case for `in_app` inside `DeliverNext`,
  an IDOR-able inbox edge, a raw secret in config, a new dependency, a `REVOKE DELETE` that breaks RTBF
  erasure, or an unguarded push egress): do NOT hack around it and do NOT mark the task done. Append the
  blocker to `docs/milestones/BLOCKERS.md` (task id + what's blocking + what you need), commit that file
  only, and STOP.
```

---

**Runner:** loop this mission with a **fresh context each iteration**, capped at some max, and
stop when the output contains `MILESTONE 11 COMPLETE` or a new line lands in
`docs/milestones/BLOCKERS.md`.

**Note:** the M11 tasks are `[ ]` checkboxes. The runner (and `cmd/ralph`) treats "no `[x]` /
no `— done:`" as TODO. Work the first unchecked task each iteration; mark it `[x]` + `— done:` when
its *Done when* condition is observably satisfied.
