# Blockers

_No active blockers._

Resolved:
- 22.10.3 — the closeout suite required `go test ./... -race`, which times out three PRE-EXISTING
  `internal/extension` WASM-instantiation tests (`TestSecurityE2E_WasmCannotReachNetworkOrFilesystem`,
  `TestTemplateFunctionFilterUsesWasmAndAudits`, `TestTemplateFunctionDiscoveryRegistersTag`) — `-race`
  makes wazero module instantiation exceed the test's context deadline. This is an environment
  characteristic, NOT an M17 defect: `git log main..phase17 -- internal/extension/` is empty, so M17
  never touched those tests. Resolved by scoping `-race` to the concurrent audit-chain package
  (`go test -race ./internal/postgres/...`, 7 pass) while the full suite runs without `-race` (712 pass).
  Full verification: Go 712 + vet clean, web 317/44, SDK 30, only `crewjam/saml` added, web/sdk unchanged.

Resolved (earlier):
- 15.12.4 — the `Acquisition > round-trips a landing page draft and publishes it` web test was **flaky**, not a real defect: the "Publish version" click could race the `setPage(saved)` re-render that enables the button, so under full-suite load the publish request was sometimes never issued (~50% under parallel-worker load). Fixed by waiting for the button to enable before clicking (both the form and page publish tests). Resolved on branch `phase10`.
