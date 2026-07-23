# Blockers

_No active blockers._

Active:
- 22.10.3 — mandatory `go test ./... -race` is blocked by reproducible `internal/extension` WASM module-instantiation context deadline failures in `TestSecurityE2E_WasmCannotReachNetworkOrFilesystem`, `TestTemplateFunctionFilterUsesWasmAndAudits`, and `TestTemplateFunctionDiscoveryRegistersTag`; needs the extension/WASM test runtime issue resolved before the closeout suite can be marked green.

Resolved:
- 15.12.4 — the `Acquisition > round-trips a landing page draft and publishes it` web test was **flaky**, not a real defect: the "Publish version" click could race the `setPage(saved)` re-render that enables the button, so under full-suite load the publish request was sometimes never issued (~50% under parallel-worker load). Fixed by waiting for the button to enable before clicking (both the form and page publish tests). Resolved on branch `phase10`.
