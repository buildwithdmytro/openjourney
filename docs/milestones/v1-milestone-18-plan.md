# Consolidation & Hardening Plan: Security, Data-Integrity, Audit-Completeness & CI-Visible Test Coverage (no new features)

Status: not started. A **hardening milestone** — no new product surface. After 17 fast milestones (M11–M17
built partly by weaker models via the autonomous loop), this pays down the accumulated debt found by a
three-dimension audit (security/correctness, test-coverage, data-layer/hygiene) plus the M17 review. Every
task fixes a **specific, confirmed finding** with a **proving test**; nothing is invented.

Delivers (all fixes to existing code — zero new features, **zero new dependencies**):
1. **Critical fixes** — a deployment-breaking audit-migration bug, a secret-exfiltration gap, and an
   API-key default-scope regression.
2. **Append-only audit integrity** — close the tables missing their trigger/REVOKE, and the TRUNCATE gap.
3. **Audit completeness & atomicity** — same-transaction audit writes; stop silently dropping audit rows.
4. **Maker-checker & SAML hardening** — fail-closed separation-of-duties, a dedicated SSO-admin scope,
   SAML replay protection.
5. **CI-visible security tests** — the SSRF-block, audit-tamper, and maker-checker properties are today
   covered only by DB-gated (CI-skipped) or *fake-validating* tests; add non-gated tests that actually
   assert the property.
6. **Coverage fill** — untested SCIM handlers and UI sections.
7. **Hygiene** — de-duplicate sentinel errors, guard a fragile `DROP CONSTRAINT`, add missing hot-path
   indexes, tighten in-statement tenant guards.

This is a **recipe book**, like the Phase 2–17 plans. Every task cites the finding's file:line, the fix,
and a **Done when** proving test. **Recipes are "open the cited file, apply the fix, add the test."** No
new recipes numbered — this milestone reuses the existing patterns (append-only trigger+REVOKE like
`045_connector_runs.sql`; the `*_ref` allowlist like `security.go`; same-tx writes; the standard test
shapes).

> **This is a security + integrity milestone — the fixes in `23.0`, `23.2`, and `23.3` touch auth, audit,
> and secret handling.** Correctness matters more than speed; each ships a test that would FAIL against the
> current (buggy) code. Treat `23.0`-green (the deployment-breaking audit migration fixed + verified on a
> seeded non-empty table, and the secret-exfil + scope regression closed) as the checkpoint.

> **`23.0` comes first.** It contains the deployment-breaking migration bug (059's backfill references a
> non-existent column and fails on any DB with existing audit rows) and the two other HIGH findings.

## Design decisions (locked)

1. **Every fix ships a test that fails against the current code.** A hardening milestone with green tests
   that don't exercise the bug is worthless. Each task's test must (a) reproduce the finding, (b) pass
   after the fix. Where a property is only covered by a DB-gated integration test today, add a NON-gated
   unit test (pure logic / in-memory / fake that exercises REAL code, not a re-implemented rule).
2. **Fix forward with new migrations; never edit an applied migration.** Migrations 001–060 are immutable.
   Corrections (the 059 backfill, the append-only gaps, the scope-array regression) land in NEW migrations
   `061`+, written to be idempotent (`IF NOT EXISTS`, `CREATE OR REPLACE`, guarded backfills) so they
   apply cleanly on both fresh and existing databases.
3. **Zero new dependencies.** All fixes are stdlib + existing patterns. `go mod tidy` shows no additions;
   `web/package.json`/`sdk/javascript` unchanged. (`crewjam/saml` from M17 stays; nothing new.)
4. **Fail closed on security decisions.** Maker-checker returns "denied" when the creator identity is
   unknown (not "allowed"); the connected-content secret ref must MATCH a positive allowlist (not merely
   "not match a bad prefix"); audit failures propagate (not `_ = err`).
5. **No behavior change beyond the fix.** Hardening must not regress features. The full suite
   (`go test ./...`, web, sdk) stays green; the `-race` flag is scoped to the concurrent audit package
   (the WASM `-race` timeout is pre-existing — see M17's `22.10.3`).

## 1. Findings → fixes (the evidence base)

| # | Severity | Finding | file:line | Task |
|---|---|---|---|---|
| F1 | HIGH | `059` audit backfill references non-existent `r.new_seq` → migration FAILS on any DB with existing audit rows (no-ops only on empty) | `059_audit_tamper_evidence.sql:17` | 23.0.1 |
| F2 | HIGH | Same backfill's hash canonical (no `seq`, no `\|`, fixed-precision ts) ≠ Go `ComputeAuditRowHash` → `verify` false-positives backfilled rows | `059:19,21` vs `admin.go:635` | 23.0.1 |
| F3 | HIGH | `auth_secret_ref` → `os.Getenv(ref)` with only a `secret:`-prefix block → a `catalogs:write` admin can exfiltrate ANY server env var (DATABASE_URL, keys) via a connected-content auth header to an allowlisted host | `fetcher.go:330`, `catalogs.go:244` | 23.0.2 |
| F4 | HIGH | Migration `060` re-declared the `api_keys.scopes` DEFAULT from scratch and dropped ~17–29 scopes (forms/pages/assets/connectors/messages/flags/catalogs/…) → new keys silently lose default access | `060_dsr_workflow.sql:30-42` | 23.0.3 |
| F5 | HIGH | `prompt_versions` + `scoring_model_versions` are append-only tables with NEITHER trigger NOR REVOKE (mutable/deletable) | `026_ai_registry.sql:17`, `031_scoring.sql:16` | 23.1.1 |
| F6 | MED | `ai_activity`, `identity_merges`, `experiment_versions` have the block trigger but NO REVOKE (defense-in-depth gap) | `030:16`, `047:62`, `034:27` | 23.1.1 |
| F7 | MED | Append-only guard is `BEFORE UPDATE OR DELETE` only → a TRUNCATE (by owner) erases the audit chain untripped | `059:45-48` | 23.1.2 |
| F8 | MED | `audit()` opens its OWN tx AFTER the mutation committed, and callers discard the error (`_ = s.audit(...)`) → audit rows silently dropped on any DB blip | `admin.go:777`, `scim.go:115` | 23.2.1 |
| F9 | MED | `audit()` inserts `app_id` as `NULLIF($4,'')::uuid`, but governed principals carry non-UUID app_ids (`"default"`, `"system"`) → cast error → that mutation is NEVER audited | `admin.go:817` | 23.2.2 |
| F10 | MED | Maker-checker is fail-OPEN: `creatorOrEditorID` inferred from the most-recent `audit_events` actor; on lookup miss/error → `nil` (approval allowed); also only blocks the last actor (a co-author self-approves) | `maker_checker.go:82-94` | 23.3.1 |
| F11 | MED | SAML provider CRUD (register IdP cert + `default_role_id`) is gated by `scim:manage` → a SCIM-provisioning key can register an attacker IdP that auto-provisions admins | `server.go:119-120` | 23.3.2 |
| F12 | LOW | SAML `AllowIDPInitiated:true` + no assertion-ID cache → a captured `SAMLResponse` replays within its validity window to mint a fresh session | `saml.go:44,220` | 23.3.3 |
| F13 | HIGH(test) | SSRF-block, audit-tamper, and maker-checker are covered ONLY by DB-gated tests; the SSRF test asserts only `Nil(res)` (passes even if the guard is deleted); the maker-checker CI test validates a re-implemented fake | `catalogs_security_e2e_integration_test.go`, `render_test.go`, `maker_checker_test.go` | 23.4 |
| F14 | MED | SCIM: only 2 of 12 handlers tested; UI sections `FeatureFlags`/`Messaging`/`Extensions` have no test; `Catalogs.test.tsx` is smoke-only | `scim.go:93-331`, `web/src/sections/*` | 23.5 |
| F15 | MED | Duplicated sentinel errors: `ErrBlobStoreRequired` ×5, `ErrApproverRequired` ×4, `ErrSelfApproval` ×2 → will drift | `publishing.go:15-16` + 4 others | 23.6.1 |
| F16 | LOW | `047:7` `DROP CONSTRAINT` without `IF EXISTS` (hardcoded auto-gen name) → fragile on renamed schemas; duplicate migration number `021` | `047:7`, `021_*` | 23.6.1 |
| F17 | MED | Unindexed hot paths: `inapp_messages` expiry sweep + admin list; `feature_flags` list-by-workspace | `messages.go:72,123`, `flags.go:158` | 23.6.2 |
| F18 | LOW | `UPDATE`/`DELETE`-by-id without in-statement tenant guard (rely on a prior scoped SELECT): `saml.go:133`, `scim.go:242,305` | — | 23.6.2 |

(Verified SAFE by the audits, not touched: inbox IDOR column-pin, fetcher SSRF dial guard, SNS/Twilio
callback signature checks, AI tool-runner least-privilege + bounded loop, SCIM bearer hashing + secret
redaction, insights grounding, principal non-spoofability.)

## 6. Task list

### Milestone 23.0 — Critical fixes (deployment + security) — DO FIRST
1. [ ] **Fix the audit-chain migration + canonical (F1, F2).** New migration `061`: a CORRECT, idempotent
   backfill of `audit_events.seq`/`prev_hash`/`row_hash` (the `059` one references non-existent `r.new_seq`
   and fails on non-empty tables). Align the hash canonical with `ComputeAuditRowHash` (`admin.go:635`) —
   preferably a Go-side backfill in `Store.Migrate` (single source of truth) OR fix both sides to a shared
   fixed format (add `seq` + `|` delimiters + a fixed-precision timestamp both Go and SQL agree on).
   *Done when:* a NON-gated regression test seeds ≥3 pre-existing `audit_events` rows (differing tenants),
   runs the corrected backfill, and asserts `VerifyAuditChain` returns valid (no false-positive); the
   migration applies cleanly on both an empty and a populated `audit_events`; `go test -race
   ./internal/postgres/...` green.
2. [ ] **Close the `auth_secret_ref` exfiltration (F3).** `resolveAuthSecret` (`fetcher.go:325`) + the
   validators (`catalogs.go:244,294`) require the ref to MATCH a positive allowlist (e.g.
   `^CC_SECRET_[A-Z0-9_]+$`), not merely reject a `secret:` prefix.
   *Done when:* a connected-content source with `auth_secret_ref="DATABASE_URL"` is REJECTED at create/
   update; a valid `CC_SECRET_*` ref resolves; a test proves a non-allowlisted env var can no longer be
   read via a fetch header.
3. [ ] **Restore the API-key default scopes (F4).** New migration `061` re-declares the `api_keys.scopes`
   DEFAULT array with the FULL current catalog (the `060` array dropped ~17–29 scopes). Add a test/assertion
   that the DEFAULT array == the `permissions` catalog == `rbac.go:allowedPermissions` (no drift).
   *Done when:* a newly created API key (no explicit scopes) has the full default scope set incl. forms/
   pages/assets/connectors/messages/flags/catalogs/reports:write; a drift test asserts the three lists match.

### Milestone 23.1 — Append-only audit integrity
1. [ ] **Harden the unprotected version tables (F5, F6).** Migration `061`: add a `BEFORE UPDATE OR DELETE`
   block-mutation trigger + `REVOKE UPDATE, DELETE` to `prompt_versions` and `scoring_model_versions`
   (neither today); add the missing `REVOKE` to `ai_activity`, `identity_merges`, `experiment_versions`
   (copy the `045_connector_runs.sql` pattern; keep `identity_merges`' GUC-erasure carve-out).
   *Done when:* `UPDATE`/`DELETE` on `prompt_versions` and `scoring_model_versions` is rejected; the three
   REVOKE-only gaps are closed; the RTBF erasure path still works on `identity_merges`; integration test
   covers each.
2. [ ] **Block TRUNCATE on audit tables (F7).** Add a `BEFORE TRUNCATE ... FOR EACH STATEMENT` trigger to
   `audit_events` (and the other append-only audit tables) so a TRUNCATE can't silently erase the chain.
   *Done when:* `TRUNCATE audit_events` is rejected; test proves it.

### Milestone 23.2 — Audit completeness & atomicity
1. [ ] **Same-transaction, error-propagating audit writes (F8).** Refactor `audit()` (`admin.go:777`) to
   write within the caller's mutation transaction and RETURN its error; update call sites to propagate it
   (stop `_ = s.audit(...)`).
   *Done when:* a forced audit-write failure aborts/errors the governed mutation (no silent drop); a test
   proves a mutation + its audit row are atomic (both or neither).
2. [ ] **Guard non-UUID app_ids in audit (F9).** `audit()`'s `app_id` insert (`admin.go:817`) handles
   non-UUID principal app_ids (`"default"`/`"system"`) — store NULL or a normalized value instead of
   `'default'::uuid` (which errors).
   *Done when:* a governed action by a `app_id="system"` principal writes an audit row (previously dropped
   on cast error); test covers a non-UUID app_id.

### Milestone 23.3 — Maker-checker & SAML hardening
1. [ ] **Fail-closed, multi-actor maker-checker (F10).** Persist the explicit creator/last-editor identity
   on governed resources (or derive reliably), compare the approver against it, and return DENIED when the
   creator is UNKNOWN (fail closed, not `nil`). Prevent a co-author from self-approving.
   *Done when:* an unknown-creator approval is DENIED (not allowed); a co-author who made the last edit
   cannot approve their own draft; a distinct approver still can; non-gated unit test proves the logic
   (not a fake).
2. [ ] **Dedicated `sso:manage` scope for SAML providers (F11).** Add `sso:manage` (in the FOUR places +
   catalog); move `/v1/auth/saml/providers` CRUD off `scim:manage` onto it.
   *Done when:* a `scim:manage`-only key can no longer register a SAML provider (403); an `sso:manage` key
   can; test covers the split.
3. [ ] **SAML assertion replay protection (F12).** Persist consumed assertion IDs (one-time-use) within the
   validity window; reject a replayed `SAMLResponse`.
   *Done when:* replaying the same signed assertion to `/acs` a second time is rejected; test proves it.

### Milestone 23.4 — CI-visible security tests (no DB gate)
1. [ ] **Real SSRF-block test (F13).** A non-gated test that drives the fetcher with an enabled source
   whose URL resolves to a private IP and asserts the audit DECISION is `ssrf_blocked` (not merely
   `res == nil`, which passes even if the guard is deleted). Extract the decision into a testable return
   if needed.
   *Done when:* the test FAILS if `IsSafeURL`/the ssrf branch is removed, and passes with the guard; runs
   without `OPENJOURNEY_TEST_DATABASE_URL`.
2. [ ] **Real audit-tamper + maker-checker tests (F13).** A non-gated unit test for the audit hash chain
   (`ComputeAuditRowHash` + `VerifyAuditChain` over an in-memory/fake row set: a tampered row is detected)
   and a non-gated maker-checker test that exercises the REAL enforcement logic (not the re-implemented
   fake in `maker_checker_test.go`).
   *Done when:* both run in normal CI (no DB gate) and FAIL against a broken hash-chain / broken
   self-approval rule respectively.

### Milestone 23.5 — Coverage fill
1. [ ] **SCIM handler tests (F14).** HTTP-level tests for `createSCIMUser`/`replaceSCIMUser`/`getSCIMUser`/
   `listSCIMUsers` and the group handlers incl. group→team mapping (`patchSCIMGroup`), using the fake store.
   *Done when:* each SCIM handler has a test asserting its behavior + tenant scoping + bearer gating;
   `go test ./internal/httpapi/...` green.
2. [ ] **Untested UI section tests (F14).** Co-located `*.test.tsx` for `FeatureFlags`, `Messaging`,
   `Extensions` (currently none) and a behavior test for `Catalogs` (currently smoke-only — cover tab
   switching + connected-content source create), on the vitest + `vi.fn()` fetch-stub pattern.
   *Done when:* `cd web && npm run typecheck && npm run build && npm test` green; each section has a
   behavior test (not just static-label smoke).

### Milestone 23.6 — Hygiene
1. [ ] **De-duplicate sentinels + guard the DROP (F15, F16).** Consolidate `ErrBlobStoreRequired` (×5),
   `ErrApproverRequired` (×4), `ErrSelfApproval` (×2) into one shared package (e.g. `internal/publishing`);
   replace the duplicates with references. Add `IF EXISTS` guidance/guard for the fragile
   `047`-style `DROP CONSTRAINT` (a forward note; do not edit `047`). Do NOT rename the applied duplicate
   `021` migrations (immutable) — document the collision.
   *Done when:* each sentinel is defined once and imported; `go build ./... && go vet ./...` green; no
   behavior change (tests pass).
2. [ ] **Missing indexes + in-statement tenant guards (F17, F18).** Migration `061`: add indexes for the
   `inapp_messages` expiry sweep (`expires_at`), the `inapp_messages` admin list (`…,created_at`), and
   `feature_flags` list-by-workspace (`…,workspace_id`). Add an explicit `tenant_id` predicate to the
   `saml.go:133` UPDATE and the `scim.go:242,305` DELETEs (defense in depth).
   *Done when:* the hot-path queries hit an index (EXPLAIN or index-presence assertion); the update/delete
   statements carry a tenant guard; tests green.

### Milestone 23.7 — Closeout
1. [ ] **Run the suite**: `go build ./... && go vet ./... && go test ./...` + `go test -race
   ./internal/postgres/...` (the audit chain), `go mod tidy` (**no new dependency**),
   `cd web && npm run typecheck && npm run build && npm test`, `cd sdk/javascript && npm run build &&
   npm test`.
   *Done when:* all green and `git diff go.mod go.sum web/package.json web/package-lock.json
   sdk/javascript/package.json` is empty of additions.
2. [ ] **Audit doc** `docs/milestones/v1-milestone-18-audit.md` in the M2–M17 table format, one row per
   `23.x` task mapping the finding (F#) → the fix → the proving test.
   *Done when:* the doc exists with a row per task, each citing the finding and its regression test.

## 7. Carry-over hazards & invariants

1. **Fix forward — never edit an applied migration.** All corrections land in new migrations `061`+,
   idempotent on both fresh and populated databases. The `059` backfill and `060` scope array are fixed by
   supersession, not editing.
2. **Every fix ships a test that fails against the current (buggy) code** and passes after — a hardening
   task with a test that doesn't exercise the bug is incomplete.
3. **Security decisions fail CLOSED** — maker-checker denies on unknown creator; the secret ref must match a
   positive allowlist; audit-write failures propagate.
4. **CI-visible**: the SSRF-block, audit-tamper, and maker-checker properties get NON-gated tests that
   actually assert the property (not `Nil(res)`, not a re-implemented fake).
5. **No new dependency; no feature change.** `go mod tidy` clean; the full suite stays green; `-race` scoped
   to `internal/postgres/...` (the WASM `-race` timeout is pre-existing).
6. **Append-only means trigger + REVOKE + TRUNCATE-guard.** Every audit/version table gets all three.

## 8. Open items to confirm before coding

1. **Audit backfill approach.** Go-side backfill in `Store.Migrate` (single source of truth, avoids
   replicating Go formatting in SQL) vs. a corrected SQL backfill with a shared fixed timestamp format.
   Defaulting to Go-side.
2. **Maker-checker creator source.** Persist an explicit `created_by`/`last_editor` on governed resources
   vs. a reliable audit-derived lookup. Persisting is more robust; confirm the columns to add.
3. **SSO scope split.** `sso:manage` as a new scope vs. reusing an existing admin scope. Defaulting to a
   new dedicated scope.
4. **Secret-ref allowlist shape.** `^CC_SECRET_[A-Z0-9_]+$` prefix vs. a per-source registered secret-name.
   Defaulting to the prefix allowlist.
5. **Scope of the append-only TRUNCATE guard.** Just `audit_events` vs. all append-only tables. Defaulting
   to all audit/version tables.
