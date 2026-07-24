# V1 Milestone 19 completion audit

Authoritative scope: UX, Stability & Usability Round 2, as recorded in
[`v1-milestone-19-plan.md`](./v1-milestone-19-plan.md). Each row maps a confirmed audit finding to
the scoped fix and its proving test or verification.

| Task | Finding(s) | Fix and proving test |
|---|---|---|
| **24.0.1 Propagate remaining audit-write errors** | F8 | Complete; all audit-write returns are propagated so failed audit writes abort governed mutations. `TestSameTransactionAuditWrites_NonGated` and `TestSameTransactionAuditWrites_DBIntegration` prove propagation and rollback. |
| **24.1.1 Panic recovery and backoff on worker loops** | S1, S10 | Complete; per-message panics in delivery, journey, dispatcher, and projector paths are failed/dead-lettered through the existing DLQ path, and error paths back off. `TestDeliverNextRecoversPanicAndFailsDeliveryJob`, `TestDeliverNextRecoversPanicAndDeadLettersIntent`, `TestDrainRecoversPanickingPublisherAndDeadLettersEvent`, and `TestDrainRecoversPanickingProjectorAndDeadLettersEvent` prove poison-message recovery. |
| **24.1.2 Bound unbounded queries** | S6, S11 | Complete; profile resolution uses keyset paging and short-link listing has a 1000-row limit. `TestBoundedProfileResolutionAndShortLinkList` proves page-boundary resolution and list capping. |
| **24.1.3 Guard unchecked type assertions** | S7, S9 | Complete; missing principals return 401 and malformed journey/experiment configuration returns validation errors. `TestGetProfileWithoutAuthMiddlewareReturnsUnauthorized` and `TestValidateMalformedNodeConfigReturnsError` prove fail-safe handling. |
| **24.2.1 Root error boundary** | S5 | Complete; `RootErrorBoundary` guards the app chrome and renders recovery UI on render failure. `RootErrorBoundary.test.tsx` forces a pre-auth render throw and verifies the alert/retry fallback. |
| **24.2.2 Toast timer and cap** | S4 | Complete; toast dismissal callbacks remain stable and visible toasts are capped at five. `ToastProvider.test.tsx` proves capped bursts and independent auto-dismissal. |
| **24.2.3 Remove fabricated data and guard nested access** | S2, S8 | Complete; the synthetic Assistant sparkline was removed and partial Analytics/Reports payloads are optional-chained. Assistant rendering and `Analytics > renders a partial funnel payload without throwing` / `renders a partial deliverability payload without throwing` prove the findings. |
| **24.2.4 Double-submit guards** | S3 | Complete; listed mutation triggers are disabled while requests are in flight. `Messaging section > disables message creation while the request is in flight` plus affected-section tests prove disable/re-enable behavior. |
| **24.3.1 Success toasts on mutations** | U1 | Complete; create/update/publish/delete handlers across App and covered sections emit success feedback. `App > creates API keys with optional expiration`, `Acquisition > round-trips a form draft and publishes it`, `Catalogs section > switches tabs and creates a connected-content source`, and `Messaging section > creates an in-app message and refreshes the list` assert the feedback. |
| **24.3.2 Actionable empty states** | U2 | Complete; covered empty states use actionable CTAs, including the cited App and section empties. `App > renders an empty suppressions response without crashing` asserts a reachable CTA role. |
| **24.4.1 Standardize destructive confirms** | U3 | Complete; native confirms were replaced and identity unmerge now requires `ConfirmDialog`. `Connectors > requires confirmation before unmerging identities and supports cancel` and `Analytics > deletes a saved report` prove cancel/confirm behavior. |
| **24.4.2 Single-source navigation config** | U4 | Complete; App, AppShell, Sidebar, and CommandPalette consume shared navigation metadata, with Messaging split into appropriate groups. `navigation.test.ts` verifies all 29 views are unique and grouped. |
| **24.4.3 Palette actions and category search** | U5, U8 | Complete; palette actions execute, search includes categories/keywords, the input is labelled, and the drawer control targets the real drawer. `CommandPalette > runs a create action with the keyboard`, `CommandPalette > finds views by category keywords`, and `AppShell > traps focus within mobile nav drawer` prove the behavior. |
| **24.4.4 Validate connector mapping input** | U6 | Complete; mapping JSON uses `JsonField` validation. `Connectors > validates mapping JSON inline before publishing` proves malformed input errors and valid-input recovery. |
| **24.5.1 PageHeader adoption and token detox** | X1, X4 | Complete; section headers use `PageHeader` and covered offenders use design tokens instead of hardcoded colors. The AppShell header assertion and covered-file hex/`rgba(` grep, plus web typecheck/build/tests, prove adoption and hygiene. |
| **24.5.2 Unify UX-state primitives** | X2, X3 | Complete; hand-rolled errors/loading and `ui-crash` markup use `ErrorState`, `Spinner`, or `Skeleton`. Covered-file grep is clean and `Overview > handles fetch error` verifies the ErrorState path. |
| **24.5.3 Card and DataTable adoption** | X3, X5 | Complete; least-migrated sections use `Card`/`DataTable`, and legacy resource tables/raw tables were removed. `Scoring > creates a versioned model and inspects profile scores` verifies rendered tables use `data-table`. |
| **24.5.4 Field forms and inline validation** | U7 | Complete; covered forms use `Field`, with `useForm` validity gates and inline errors in representative forms. `Scoring > shows inline validation and gates an invalid definition` proves the error and disabled submit. |
| **24.6.1 Run the integration suite** | S1–S11, U1–U8, X1–X5 | Complete; Go build/vet/tests, scoped postgres race tests, web typecheck/build/tests, SDK build/tests, and `go mod tidy` passed; dependency manifests remained unchanged. |
| **24.6.2 Audit document** | S1–S11, U1–U8, X1–X5, F8 | Complete; this document contains one evidence row for every 24.x task and maps each finding to a fix and verifying test or suite check. The row-count and task-ID audit below verifies the required coverage. |

**Verification**

The table contains exactly one row for each task 24.0.1–24.6.2 (20 rows), and every row cites at
least one finding key plus a test or verification. The preceding plan entries record the corresponding
suite results and completion evidence.

**Summary**

Milestone 19’s confirmed UX-polish, stability, usability, and M18 residual findings are recorded with
scoped fixes and regression evidence. No new dependencies were added.
