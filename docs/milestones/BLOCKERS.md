# Blockers

_No active blockers._

Resolved:
- 15.12.4 — the `Acquisition > round-trips a landing page draft and publishes it` web test was **flaky**, not a real defect: the "Publish version" click could race the `setPage(saved)` re-render that enables the button, so under full-suite load the publish request was sometimes never issued (~50% under parallel-worker load). Fixed by waiting for the button to enable before clicking (both the form and page publish tests). Resolved on branch `phase10`.
