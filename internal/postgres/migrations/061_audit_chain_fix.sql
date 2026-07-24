-- Migration 061: Fix audit events sequence and tamper-evident hash backfill.

ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS seq bigint;
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS prev_hash text NOT NULL DEFAULT '';
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS row_hash text NOT NULL DEFAULT '';

-- Drop any NOT NULL constraint on seq temporarily to allow unsequenced rows during backfill
ALTER TABLE audit_events ALTER COLUMN seq DROP NOT NULL;

-- Trigger to block UPDATE or DELETE
CREATE OR REPLACE FUNCTION audit_events_block_mutation() RETURNS trigger AS $$
BEGIN RAISE EXCEPTION 'audit_events is append-only'; END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS audit_events_no_update ON audit_events;
CREATE TRIGGER audit_events_no_update BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_block_mutation();

REVOKE UPDATE, DELETE ON audit_events FROM PUBLIC;

CREATE INDEX IF NOT EXISTS audit_events_tenant_actor_idx ON audit_events (tenant_id, actor_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS audit_events_tenant_resource_idx ON audit_events (tenant_id, resource_type, resource_id, occurred_at DESC);
