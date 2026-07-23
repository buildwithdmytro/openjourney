-- Migration 059: Tamper-evident append-only audit events with hash chaining.

ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS seq bigint;
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS prev_hash text NOT NULL DEFAULT '';
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS row_hash text NOT NULL DEFAULT '';

DO $$
DECLARE
    t_id uuid;
    r RECORD;
    p_hash text;
BEGIN
    FOR t_id IN SELECT DISTINCT tenant_id FROM audit_events LOOP
        p_hash := '';
        FOR r IN SELECT * FROM audit_events WHERE tenant_id = t_id ORDER BY occurred_at ASC, id ASC LOOP
            UPDATE audit_events
            SET seq = COALESCE(r.seq, r.new_seq),
                prev_hash = p_hash,
                row_hash = encode(sha256((p_hash || r.id::text || r.tenant_id::text || r.workspace_id::text || COALESCE(r.app_id::text, '') || r.actor_type || r.actor_id || r.action || r.resource_type || COALESCE(r.resource_id, '') || r.metadata::text || to_char(r.occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))::bytea), 'hex')
            WHERE id = r.id;
            p_hash := encode(sha256((p_hash || r.id::text || r.tenant_id::text || r.workspace_id::text || COALESCE(r.app_id::text, '') || r.actor_type || r.actor_id || r.action || r.resource_type || COALESCE(r.resource_id, '') || r.metadata::text || to_char(r.occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))::bytea), 'hex');
        END LOOP;
    END LOOP;
END $$;

WITH ranked AS (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY tenant_id ORDER BY occurred_at ASC, id ASC) as new_seq
    FROM audit_events
    WHERE seq IS NULL
)
UPDATE audit_events a
SET seq = r.new_seq
FROM ranked r
WHERE a.id = r.id;

ALTER TABLE audit_events ALTER COLUMN seq SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS audit_events_tenant_seq_idx ON audit_events (tenant_id, seq);

-- Trigger to block UPDATE or DELETE
CREATE OR REPLACE FUNCTION audit_events_block_mutation() RETURNS trigger AS $$
BEGIN RAISE EXCEPTION 'audit_events is append-only'; END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS audit_events_no_update ON audit_events;
CREATE TRIGGER audit_events_no_update BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_block_mutation();

REVOKE UPDATE, DELETE ON audit_events FROM PUBLIC;

-- Filter indexes
CREATE INDEX IF NOT EXISTS audit_events_tenant_actor_idx ON audit_events (tenant_id, actor_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS audit_events_tenant_resource_idx ON audit_events (tenant_id, resource_type, resource_id, occurred_at DESC);
