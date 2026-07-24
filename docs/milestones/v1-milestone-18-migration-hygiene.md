# Milestone 18 migration hygiene

Applied migrations are immutable. Migration 047 contains a historical, hardcoded
`DROP CONSTRAINT identity_merges_source_event_id_key`; future migrations that
replace constraints must use `DROP CONSTRAINT IF EXISTS` (and should prefer
catalog-driven names where schemas may have been renamed). Migration 047 is not
edited retroactively.

The two existing `021_*.sql` files are an immutable migration-number collision:
`021_attribution_lookup_indexes.sql` and `021_report_disposition_indexes.sql`.
They must not be renamed; new migrations use the next unused zero-padded number.
