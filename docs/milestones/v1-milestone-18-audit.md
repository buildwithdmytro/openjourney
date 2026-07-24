# V1 Milestone 18 completion audit

Authoritative scope: Consolidation & Hardening — security, data-integrity, audit-completeness,
test-coverage, and hygiene fixes recorded in [`v1-milestone-18-plan.md`](./v1-milestone-18-plan.md).

| Task | Finding(s) | Fix and proving test |
|---|---|---|
| **23.0.1 Audit-chain migration and canonical** | F1, F2 | Complete; migration `061` supersedes the broken `059` backfill and `BackfillAuditChain` shares the Go hash canonical. `TestAuditChainBackfill_NonGated` and `TestAuditChainBackfill_SeededNonEmptyTable` in [`audit_tamper_evidence_test.go`](../../internal/postgres/audit_tamper_evidence_test.go) prove valid backfill on seeded rows. |
| **23.0.2 Connected-content secret references** | F3 | Complete; `auth_secret_ref` uses the positive `CC_SECRET_*` allowlist. `TestResolveAuthSecret_AllowlistExfiltrationPrevention` and `TestCreateConnectedContentSource_RejectsNonAllowlistedAuthSecretRef` prove environment-variable exfiltration is rejected. |
| **23.0.3 API-key default scopes** | F4 | Complete; migration `062` restores the full permission catalog and the catalog/default declarations remain aligned. `TestAPIKeyDefaultScopes_NoDrift` and `TestAPIKeyDefaultScopes_DBIntegration` prove the default scope set. |
| **23.1.1 Append-only version tables and REVOKEs** | F5, F6 | Complete; migration `063` adds mutation-blocking triggers and missing `REVOKE UPDATE, DELETE` protections. `TestAppendOnlyVersionTables_MigrationSQL` and `TestAppendOnlyVersionTables_DBIntegration` prove version-table immutability and the identity-merge erasure carve-out. |
| **23.1.2 Audit-table TRUNCATE guards** | F7 | Complete; migration `064` adds statement-level `BEFORE TRUNCATE` guards to append-only audit/version tables. `TestTruncateGuardAuditTables_MigrationSQL` and `TestTruncateGuardAuditTables_DBIntegration` prove truncation is rejected. |
| **23.2.1 Same-transaction audit writes** | F8 | Complete; audit writes use the mutation transaction and errors propagate. `TestSameTransactionAuditWrites_NonGated` and `TestSameTransactionAuditWrites_DBIntegration` prove mutation/audit atomicity. |
| **23.2.2 Non-UUID audit app IDs** | F9 | Complete; non-UUID principal app IDs are stored safely instead of being cast to UUID. `TestAuditNonUUIDAppID_NonGated` and `TestAuditNonUUIDAppID_DBIntegration` prove governed actions remain audited. |
| **23.3.1 Maker-checker enforcement** | F10 | Complete; creator identity is fail-closed and all relevant actors are considered. `TestMakerCheckerFailClosedAndMultiActor_NonGated`, `TestMakerCheckerEnforcementRejectsSelfApproval_NonGated`, and `TestMakerCheckerPoliciesAndEnforcement` prove unknown-creator denial, co-author denial, and distinct-approver success. |
| **23.3.2 Dedicated SSO-admin scope** | F11 | Complete; SAML provider CRUD requires `sso:manage`, separate from `scim:manage`. `TestSAMLProviderScopeSplit` and `TestEnterpriseSecurityE2E` prove the scope split. |
| **23.3.3 SAML assertion replay protection** | F12 | Complete; consumed assertion IDs are cached for their validity window. `TestSAMLAssertionReplayProtection` proves a replayed assertion is rejected. |
| **23.4.1 Real SSRF-block regression** | F13 | Complete; the real fetcher records the explicit `ssrf_blocked` decision. `TestFetcherSSRFBlockRecordsExactDecision` proves the test fails if the SSRF branch is removed and runs without a database. |
| **23.4.2 Real audit-tamper and maker-checker regressions** | F13 | Complete; non-gated tests exercise production hash verification and enforcement logic. `TestAuditHashChainDetectsTamperedRow_NonGated` and `TestMakerCheckerEnforcementRejectsSelfApproval_NonGated` prove tampering and self-approval are rejected. |
| **23.5.1 SCIM handler coverage** | F14 | Complete; HTTP-level fake-store tests cover user/group handlers, tenant scope, bearer gating, and group-to-team mapping. `TestSCIMHandlersPropagateTenantAndMapGroupPatch` proves the handler behavior. |
| **23.5.2 UI section coverage** | F14 | Complete; FeatureFlags, Messaging, Extensions, and Catalogs have behavior tests covering their interactions and connected-content creation. `FeatureFlags.test.tsx`, `Messaging.test.tsx`, `Extensions.test.tsx`, and `Catalogs.test.tsx` provide the proving coverage. |
| **23.6.1 Sentinel and migration hygiene** | F15, F16 | Complete; sentinel errors have one canonical identity and immutable migration hazards are documented. `TestPublishingSentinelsHaveOneCanonicalIdentity` proves no duplicate sentinel definitions. |
| **23.6.2 Indexes and tenant guards** | F17, F18 | Complete; migration `066` adds the hot-path indexes and SAML/SCIM mutations include in-statement tenant predicates. `TestHotPathIndexesAndTenantGuards` proves both the index presence and SQL guards. |
| **23.7.1 Full verification suite** | F1–F18 | Complete; Go build/vet/tests, scoped audit-chain race tests, web, SDK, and tidy/dependency checks are green with no new dependencies, as recorded in the 23.7.1 plan entry. |
| **23.7.2 Audit document** | F1–F18 | Complete; this document provides one evidence row per 23.x task, mapping each finding to its fix and proving regression test. |

**Summary**

Milestone 18 is complete. The audit migration, secret-reference validation, API-key scopes, append-only
integrity, audit atomicity, maker-checker and SAML controls, CI-visible security tests, coverage gaps, and
hygiene findings F1–F18 are recorded with regression evidence. No new dependencies were added.
