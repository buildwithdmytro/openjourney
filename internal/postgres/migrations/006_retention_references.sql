ALTER TABLE consent_ledger DROP CONSTRAINT IF EXISTS consent_ledger_source_event_id_fkey;
ALTER TABLE consent_ledger ALTER COLUMN source_event_id DROP NOT NULL;
ALTER TABLE consent_ledger ADD CONSTRAINT consent_ledger_source_event_id_fkey
    FOREIGN KEY (source_event_id) REFERENCES accepted_events(id) ON DELETE SET NULL;

ALTER TABLE identity_aliases DROP CONSTRAINT IF EXISTS identity_aliases_source_event_id_fkey;
ALTER TABLE identity_aliases ADD CONSTRAINT identity_aliases_source_event_id_fkey
    FOREIGN KEY (source_event_id) REFERENCES accepted_events(id) ON DELETE SET NULL;

ALTER TABLE identity_merges DROP CONSTRAINT IF EXISTS identity_merges_source_event_id_fkey;
ALTER TABLE identity_merges ALTER COLUMN source_event_id DROP NOT NULL;
ALTER TABLE identity_merges ADD CONSTRAINT identity_merges_source_event_id_fkey
    FOREIGN KEY (source_event_id) REFERENCES accepted_events(id) ON DELETE SET NULL;

ALTER TABLE outbox_events DROP CONSTRAINT IF EXISTS outbox_events_event_id_fkey;
ALTER TABLE outbox_events ADD CONSTRAINT outbox_events_event_id_fkey
    FOREIGN KEY (event_id) REFERENCES accepted_events(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS accepted_events_retention_idx
    ON accepted_events (tenant_id, received_at, id);
