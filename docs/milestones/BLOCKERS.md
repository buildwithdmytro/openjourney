# Blockers

- 15.12.4 — `cd web && npm test` is not green: `src/sections/Acquisition.test.tsx` fails “round-trips a landing page draft and publishes it” because the test expects `/v1/pages/page-1/publish`, while the current component emits only the draft `/api/v1/pages` request. Resolve the pre-existing Acquisition route/test mismatch, then rerun the full closeout suite.
