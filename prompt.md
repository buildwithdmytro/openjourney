# Milestone 17 — Ralph-loop prompt

**What this is:** an autonomous single-task loop prompt for implementing OpenJourney
**Milestone 17 — Enterprise Readiness** (SAML SSO, SCIM provisioning, custom roles + teams, maker-checker,
tamper-evident audit, and data-subject-request workflows) — `docs/milestones/v1-milestone-17-plan.md`,
tasks 22.0–22.10. Each run does exactly ONE task, verifies it, records progress in the plan file, commits,
and stops. Run it repeatedly (fresh context each time) until it prints `MILESTONE 17 COMPLETE` or writes a
new line to `docs/milestones/BLOCKERS.md`.

**Recommended runner:**

```bash
go run ./cmd/ralph --primary antigravity --unsafe-autonomous
go run ./cmd/ralph --primary codex --unsafe-autonomous
go run ./cmd/ralph --primary claude --unsafe-autonomous
```

`--primary` accepts `codex`, `antigravity`, or `claude`. The Antigravity default model is
`gemini-3.6-flash-medium` (override with `--antigravity-model <slug>`; see `agy models`).

The defaults now target Milestone 17 (`--plan docs/milestones/v1-milestone-17-plan.md`,
`--branch phase17`, `--milestone 17`). Use `--dry-run` to validate without invoking an agent.

**Manual use:** paste the block below as the agent mission and run it on the `phase17` branch; re-trigger
for each iteration.

---

```text
You are an autonomous coding agent implementing OpenJourney **Milestone 17 — Enterprise Readiness**
(SAML SSO, SCIM 2.0 provisioning, custom ROLES + TEAMS + a unified permission catalog, MAKER-CHECKER
separation of duties, a TAMPER-EVIDENT audit log, and GDPR/CCPA DATA-SUBJECT-REQUEST workflows), strictly
following `docs/milestones/v1-milestone-17-plan.md`. This is ONE iteration of a loop: do **exactly ONE
task**, verify it, record it, commit, then STOP. A fresh agent runs next iteration with NO memory of this
one — all state must be on disk (the plan's checkboxes + git). Do not try to do the whole milestone in one
run.

## STEP 1 — Orient (every iteration, in this order)
1. Read `docs/milestones/v1-milestone-17-plan.md` IN FULL.
2. Re-read §"Design decisions (locked)" and §7 "Carry-over hazards & invariants". These are INVARIANTS
   you may not violate. **This is the auth/authz surface — every task is security-critical.**
3. Skim the recipes it cites — this plan's new recipes 6.100–6.108. This milestone REUSES the existing
   identity model: users + `(oidc_issuer, oidc_subject)` link (`002_phase1.sql:19`), `roles`/`role_bindings`
   (`002_phase1.sql:32,42`), `CreateUser`/`EnsureLocalAdmin` upsert (`internal/postgres/rbac.go:96,204`),
   `CreateLocalSession`/`authenticateSession` (`internal/postgres/auth.go:47`, `store.go:186`), the
   `authenticate` middleware (`internal/httpapi/server.go:347`), the `allowedPermissions` catalog
   (`rbac.go:12-33`), the append-only audit hardening pattern (`045_connector_runs.sql` trigger + REVOKE),
   the `audit()` writer (`admin.go:560`), the RTBF erasure path (`internal/postgres/operations.go:172`,
   GUC-gated `047`), the `experiments` propose→approve template (`experiments.go:66`), and the M12
   component library (`web/src/components/`).
4. Run `git log --oneline -15` and `git status` to see what already exists on disk.
5. A task is DONE only if **its own** numbered line/block literally contains `[x]` or a `— done:` note.
   Check EACH task individually. Work the **single first unchecked task in document order** and nothing
   else.
   - **NEVER skip a task or jump ahead.** Tasks run strictly 22.0 → 22.10 in order. A conditional no-op
     task (e.g. "fold any further findings" with nothing to fold) is marked `[x]` + `— done: <why it's a
     no-op>` and committed — NEVER skipped past to a later task (that trips the runner's guard, exit 4).
   - **If the first unchecked task looks already-implemented:** VERIFY its literal *Done when*; if it
     holds, mark done + commit the docs change; else implement it.
   - **If you cannot complete the first unchecked task:** append a line to
     `docs/milestones/BLOCKERS.md` and commit only that. Do NOT route around it.
   **22.0 (M16 closeout) and 22.1 (permission catalog + role CRUD) come first** — the authz foundation
   teams/SCIM/maker-checker build on.

## STEP 2 — Do exactly ONE task
- Implement only that single sub-task. Follow its referenced Recipe: open the named existing file, copy
  it, rename, change the fields. DO NOT design from scratch — everything has a cited file:line.
- Honor these invariants every time they apply:
  - **Auth crypto is LIBRARY-OWNED, never hand-rolled.** SAML (task 22.4) uses the vetted SAML SP library
    (D.D. 1) for assertion parsing + XML-signature verification. Do NOT hand-parse or hand-verify the XML
    signature (XML-signature-wrapping is a classic exploit). The ACS maps the assertion NameID →
    `oidc_subject` and IdP entityID → `oidc_issuer`, upserts the user (`rbac.go:204` pattern), and mints a
    `user_sessions` row (`auth.go:47`) so it flows through the existing session auth. Validate audience +
    reject replayed/expired assertions (the library provides these — use them).
  - **SCIM is bearer-token-gated + tenant-scoped.** A bad/again SCIM token is 401; deprovision sets
    `disabled_at` (a disabled user fails auth immediately). SCIM is zero-dep JSON REST over `CreateUser`/
    `role_bindings`/teams.
  - **Maker-checker is enforced SERVER-SIDE from the authenticated principal — never a client-supplied
    approver id.** The approver is `principal.UserID`; reject self-approval (`principal.UserID` ==
    creator/last-editor) when the resource's policy requires a checker. The human-actor gate
    (`identity.go:85`, `publishing.go:33`) stays; maker-checker layers on top. `experiments.go:66` is the
    template.
  - **Audit is APPEND-ONLY (trigger + REVOKE) AND hash-chained.** `audit_events` gets a `BEFORE UPDATE OR
    DELETE` block-mutation trigger + `REVOKE UPDATE, DELETE` (copy `045_connector_runs.sql`) PLUS a
    per-tenant hash chain (`seq`/`prev_hash`/`row_hash = sha256(prev_hash||canonical row)`) computed in the
    `audit()` writer in-tx, serialized per tenant. Tampering must be detectable via a `verify` endpoint.
    No PII in `metadata`.
  - **DSR stays governed + audited.** Erasure stays behind the GUC-gated `identity_merges_guard` trigger
    (`047`, `SET LOCAL openjourney.erasure='on'`); export/erase emit an `audit_events` row on completion;
    downloads are authz-checked; requester verification precedes processing.
  - **Governance wiring — new scopes in FOUR places.** `audit:read`/`privacy:read`/`privacy:approve`/
    `teams:read`/`teams:write`/`scim:manage` → (a) `rbac.go` `allowedPermissions`/the new `permissions`
    catalog, (b) the `api_keys.scopes` DEFAULT array RE-DECLARED in the newest migration, (c) the route
    guards, (d) `web/src/App.tsx AVAILABLE_SCOPES`. The `View` union is TRIPLICATED in `App.tsx`,
    `web/src/components/Sidebar.tsx`, AND `web/src/components/CommandPalette.tsx` — update all three. Every
    `status` the code writes appears in a CHECK.
  - **NO new dependency — EXCEPT the ONE vetted SAML library in task 22.4.** That single dependency is the
    ONLY allowed addition to `go.mod`, and ONLY for the SAML task (D.D. 1). Every other task adds ZERO
    dependencies. `web/package.json`, `web/package-lock.json`, `sdk/javascript/package.json` MUST be
    unchanged throughout. Outside 22.4, `go mod tidy` MUST show no additions. UI reuses the M12
    `web/src/components/` library — do NOT hand-roll controls.
  - New migration = next zero-padded number on disk (currently `056`), `IF NOT EXISTS`, uuid PK,
    timestamptz; append-only tables carry a `BEFORE UPDATE OR DELETE` trigger + REVOKE.

## STEP 3 — Verify (MANDATORY — do not mark a task done if any check fails)
- Go changes: `go build ./... && go vet ./... && go test ./...` (add `-race` for the audit-chain task).
  Run `go mod tidy`: OUTSIDE task 22.4 confirm `git diff go.mod go.sum` shows NO addition; FOR 22.4
  confirm ONLY the SAML library + its transitive closure was added (nothing else, ever, in web/sdk).
- Migration: applies cleanly; CHECKs accept every value the code writes and reject an unknown one;
  `audit_events` rejects UPDATE/DELETE.
- Web changes: `cd web && npm run typecheck && npm run build && npm test`.
- The task's literal **"Done when"** condition MUST be observably satisfied. For the load-bearing
  properties (SAML-signature-verified-by-library + forged-rejected, SCIM-bearer-gated + deprovision-denies,
  maker-checker-self-approval-blocked, audit-append-only + hash-chain-detects-tampering, DSR-verified +
  audited + GUC-gated, new-scopes-enforced, only-SAML-dep-added) that means an actual test proving the
  property — not just that it compiles.

## STEP 4 — Record & commit
- Edit `docs/milestones/v1-milestone-17-plan.md`: prefix the finished sub-task with `[x]` and append
  `— done: <one-line evidence>`.
- Ensure you are on branch `phase17` (create it from the current branch if it doesn't exist). NEVER
  commit to `main`.
- Commit ONLY this task's changes with a conventional message, e.g.
  `feat(enterprise): 22.3 add SCIM 2.0 user provisioning` or
  `feat(security): 22.6 add append-only hash-chained audit log`. Follow the commit-trailer convention.

## STEP 5 — Stop
- After committing ONE task, output a 2-line summary (task id + what you did) and STOP.
- If ALL tasks are `[x]`, output `MILESTONE 17 COMPLETE` and STOP.
- If BLOCKED (real ambiguity, a failing build/test you can't fix within this task's scope, a missing
  prerequisite, a needed human approval, or anything that would require weakening an invariant — e.g. a
  dependency added OUTSIDE task 22.4, hand-rolled SAML signature verification, a client-supplied approver
  id, a writable/unchained audit log, an ungated SCIM endpoint, erasure outside the GUC trigger, or a
  hand-rolled UI control instead of the M12 primitives): do NOT hack around it, do NOT delete/disable a
  failing test, and do NOT mark the task done. Append the blocker to `docs/milestones/BLOCKERS.md`
  (task id + what's blocking + what you need), commit that file only, and STOP.
```

---

**Runner:** loop this mission with a **fresh context each iteration**, capped at some max, and stop when
the output contains `MILESTONE 17 COMPLETE` or a new line lands in `docs/milestones/BLOCKERS.md`.

**Note:** the M17 tasks are `[ ]` checkboxes. Work the first unchecked task each iteration; mark it `[x]`
+ `— done:` when its *Done when* condition is observably satisfied. A conditional no-op task is marked
done with a note — never skipped. **SAML (22.4) is the ONLY task that may add a dependency.**
