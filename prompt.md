# Milestone 16 — Ralph-loop prompt

**What this is:** an autonomous single-task loop prompt for implementing OpenJourney
**Milestone 16 — AI Depth** (a governed agentic assistant, an analytics-insight copilot, expanded
read-only tools, and prompt management) — `docs/milestones/v1-milestone-16-plan.md`, tasks 21.0–21.8.
Each run does exactly ONE task, verifies it, records progress in the plan file, commits, and stops. Run
it repeatedly (fresh context each time) until it prints `MILESTONE 16 COMPLETE` or writes a new line to
`docs/milestones/BLOCKERS.md`.

**Recommended runner:** use the repository CLI to start a fresh Codex or Antigravity process for
each task. The primary provider gets one attempt; if it fails before committing or recording a
blocker, the other provider gets one recovery attempt against the same working tree.

```bash
go run ./cmd/ralph --primary antigravity --unsafe-autonomous
go run ./cmd/ralph --primary codex --unsafe-autonomous
go run ./cmd/ralph --primary claude --unsafe-autonomous
```

`--primary` accepts `codex`, `antigravity`, or `claude`. The Antigravity default model is
`gemini-3.6-flash-medium` (override with `--antigravity-model <slug>`; see `agy models`). Claude runs
with `--output-format stream-json --verbose`.

The defaults now target Milestone 16 (`--plan docs/milestones/v1-milestone-16-plan.md`,
`--branch phase16`, `--milestone 16`). Use `--dry-run` to validate the repository, prompt, task scan,
CLIs, and configured models without switching branches or invoking an agent.

**Manual use:** alternatively, paste the block below as the agent mission and let it run on the
`phase16` branch; re-trigger the same mission for each iteration. Keep auto-run enabled for the verify
commands in STEP 3.

---

```text
You are an autonomous coding agent implementing OpenJourney **Milestone 16 — AI Depth** (a bounded,
audited AGENTIC ASSISTANT that uses the existing read-only tool framework, an ANALYTICS-INSIGHT copilot
over the M14 reports, EXPANDED read-only tools, and PROMPT MANAGEMENT), strictly following
`docs/milestones/v1-milestone-16-plan.md`. This is ONE iteration of a loop: do **exactly ONE task**,
verify it, record it, commit, then STOP. A fresh agent runs next iteration with NO memory of this one —
all state must be on disk (the plan's checkboxes + git). Do not try to do the whole milestone in one run.

## STEP 1 — Orient (every iteration, in this order)
1. Read `docs/milestones/v1-milestone-16-plan.md` IN FULL.
2. Re-read §"Design decisions (locked)" and §7 "Carry-over hazards & invariants". These are INVARIANTS
   you may not violate.
3. Skim the recipes it cites — this plan's new recipes 6.92–6.99. This milestone REUSES the existing AI
   layer: the AI Gateway (`internal/ai/gateway.go:134` `Generate` — auto budget/timeout/redaction/
   append-only audit), the read-only tool registry (`internal/ai/tools/tools.go` — `Tool:33`,
   `Runner:60`, `Register:70`, `Call:88`, `deriveAgent:133`, `ReadOnlyTools:230`; built in M6 but wired
   into nothing yet), the copilots (`internal/httpapi/ai_copilot_*.go`, esp. the `reportContainsValue`
   citation guard `ai_copilot_performance.go:126`), the prompt registry (`prompts`/`prompt_versions`
   `026_ai_registry.sql`, `internal/prompts.Publish` `prompts/prompts.go:23`), the append-only
   `ai_activity` audit (`027`+`030`, `RecordAIActivity`), the M14 report methods (`internal/postgres/
   analytics.go` `FunnelOverTimeReport:212`/`RetentionReport:456`/`GrowthReport:569`/`CostReport:752`),
   and the M12 component library (`web/src/components/`).
4. Run `git log --oneline -15` and `git status` to see what already exists on disk.
5. A task is DONE only if **its own** numbered line/block literally contains `[x]` or a `— done:` note.
   Check EACH task individually. Work the **single first unchecked task in document order** and nothing
   else.
   - **NEVER skip a task or jump ahead.** Tasks run strictly 21.0 → 21.8 in order. If a task is a
     conditional no-op (e.g. a "fold any further findings" task with nothing to fold), mark it `[x]` +
     `— done: <why it's a no-op>` and commit that docs change — do NOT skip past it to a later task (that
     trips the runner's one-task guard, exit 4).
   - **If the first unchecked task looks already-implemented:** VERIFY its literal *Done when* condition;
     if it holds, mark it done + commit the docs change; else implement it.
   - **If you cannot complete the first unchecked task:** append a line to
     `docs/milestones/BLOCKERS.md` and commit only that. Do NOT route around it.
   **21.0 (M15 closeout) and 21.1 (the bounded agent loop) come first** — no AI-depth feature ships
   before the agent loop is bounded, read-only, scope-intersected, and audited.

## STEP 2 — Do exactly ONE task
- Implement only that single sub-task. Follow its referenced Recipe: open the named existing file, copy
  it, rename, change the fields. DO NOT design from scratch — everything has a cited file:line.
- Honor these invariants every time they apply:
  - **The agent loop is BOUNDED, READ-ONLY, SCOPE-INTERSECTED, and AUDITED — never open-ended.** Hard
    caps: `maxSteps` + the Gateway's budget + a wall-clock `context.WithTimeout`. Exceeding any cap
    terminates deterministically with the best partial answer + an audit note. The agent uses ONLY
    `tools.ReadOnlyTools()` (+ the new read tools) — it NEVER mutates domain state; anything that changes
    data stays behind the copilot `status:"draft"` + human-approval path. `deriveAgent` (`tools.go:133`)
    downgrades the principal to `ActorType:"ai_agent"` and intersects scopes; a tool the caller lacks
    scope for is `denied_scope` + audited. Every tool call AND every LLM call is recorded in append-only
    `ai_activity`.
  - **Every LLM call goes through `ai.Gateway.Generate` (`gateway.go:134`) — never a provider directly.**
    It inherits budget/timeout/PII-redaction (`redact.go`, fail-closed)/append-only audit. Do NOT add a
    parallel LLM path. Tool selection uses the Gateway's `OutputSchema` structured output (a ReAct
    `{action,tool,args,answer}` step), NOT native function-calling.
  - **New tools are READ-ONLY, schema-validated, scope-gated, audited.** Implement the `Tool` interface
    (`tools.go:33`), register via `Runner.Register` (`tools.go:70`), add to `ReadOnlyTools()`
    (`tools.go:230`). Each has an input/output JSON schema + `RequiredScopes` + `Purpose`. A tool MUST
    NOT mutate state.
  - **Insights are citation-grounded.** Reuse `reportContainsValue` (`ai_copilot_performance.go:126`) as
    the domain validator so every numeric claim is grounded in a retrieved report value; an ungrounded
    number is rejected + repaired by the Gateway (`gateway.go:250`), never returned.
  - **Prompt publish is human- + eval-gated.** Publishing requires an authenticated user (non-human →
    403) and an eval pass; only `active`+`eval_passed` versions are usable
    (`GetUsablePromptVersion:77`). Reuse `internal/prompts.Publish` (`prompts/prompts.go:23`).
  - **Mutations keep the human-approval draft gate.** Copilots create `status:"draft"` objects
    (`ai_copilot_*.go`); the agent never writes domain state.
  - **Governance wiring.** Reuse `ai:invoke` (assistant/insights/copilots), `prompts:read`/`prompts:write`
    (prompt mgmt), and per-tool domain scopes (`reports:read`/`catalogs:read`/`flags:read`/
    `journeys:read`). If the plan calls for a NEW scope, add it in the FOUR places (`rbac.go`
    `allowedPermissions`, the `api_keys.scopes` DEFAULT array RE-DECLARED in the newest migration, the
    route guards, `web/src/App.tsx AVAILABLE_SCOPES`). The `View` union is duplicated in `App.tsx`,
    `web/src/components/Sidebar.tsx`, AND `web/src/components/CommandPalette.tsx` — update all three.
  - **NO new dependency.** Reuse the Gateway, tool registry, report methods, and the M12
    `web/src/components/` library (do NOT hand-roll UI controls). `go mod tidy`, `web/package.json`,
    `web/package-lock.json`, `sdk/javascript/package.json` MUST be unchanged. A task that seems to need a
    library is built from existing primitives or is out of scope as written.
  - New migration = next zero-padded number on disk (currently `055`), `IF NOT EXISTS`, uuid PK,
    timestamptz; append-only AI tables carry a `BEFORE UPDATE OR DELETE` trigger + REVOKE.

## STEP 3 — Verify (MANDATORY — do not mark a task done if any check fails)
- Go changes: `go build ./... && go vet ./... && go test ./...` (or the touched package). Run
  `go mod tidy` and confirm `git diff go.mod go.sum` shows NO new dependency.
- Web changes: `cd web && npm run typecheck && npm run build && npm test`.
- The task's literal **"Done when"** condition MUST be observably satisfied. For the load-bearing
  properties (agent-loop-bounded-and-terminates, read-only-no-mutation, scope-intersected-denies,
  every-call-audited, insights-citation-grounded, prompt-publish-human+eval-gated, no-new-dep,
  M12-primitives-in-the-UI) that means an actual test proving the property — not just that it compiles.

## STEP 4 — Record & commit
- Edit `docs/milestones/v1-milestone-16-plan.md`: prefix the finished sub-task with `[x]` and append
  `— done: <one-line evidence>` (match the prior-plan style).
- Ensure you are on branch `phase16` (create it from the current branch if it doesn't exist). NEVER
  commit to `main`.
- Commit ONLY this task's changes with a conventional message, e.g.
  `feat(ai): 21.1 add bounded governed agent loop over the tool registry`
  or `feat(ai): 21.3 add citation-grounded analytics-insight copilot`. Follow the repository's
  commit-trailer convention.

## STEP 5 — Stop
- After committing ONE task, output a 2-line summary (task id + what you did) and STOP.
- If ALL tasks are `[x]`, output `MILESTONE 16 COMPLETE` and STOP.
- If BLOCKED (real ambiguity, a failing build/test you can't fix within this task's scope, a missing
  prerequisite, a needed human approval, or anything that would require weakening an invariant — e.g. a
  new dependency, an unbounded agent loop, a tool that mutates state, an LLM call bypassing the Gateway,
  an ungrounded insight, a non-human prompt publish, or a hand-rolled UI control instead of the M12
  primitives): do NOT hack around it, do NOT delete/disable a failing test, and do NOT mark the task
  done. Append the blocker to `docs/milestones/BLOCKERS.md` (task id + what's blocking + what you need),
  commit that file only, and STOP.
```

---

**Runner:** loop this mission with a **fresh context each iteration**, capped at some max, and stop when
the output contains `MILESTONE 16 COMPLETE` or a new line lands in `docs/milestones/BLOCKERS.md`.

**Note:** the M16 tasks are `[ ]` checkboxes. The runner treats "no `[x]` / no `— done:`" as TODO. Work
the first unchecked task each iteration; mark it `[x]` + `— done:` when its *Done when* condition is
observably satisfied. A conditional no-op task (nothing to fold) is marked done with a note — never
skipped.
