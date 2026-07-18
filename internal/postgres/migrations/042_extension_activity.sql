-- Migration 042: Create extension activity audit log, subscriptions, and health tables.

CREATE TABLE IF NOT EXISTS extension_activity (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    extension_id uuid NOT NULL REFERENCES extensions(id) ON DELETE CASCADE,
    extension_version integer NOT NULL,
    kind text NOT NULL,                         -- channel_provider | journey_action | ...
    invocation text NOT NULL,                   -- e.g. 'send', 'decide', 'transform'
    derived_scopes text[] NOT NULL DEFAULT '{}',
    input_ref text, output_ref text,
    latency_ms integer NOT NULL DEFAULT 0,
    cost_cents bigint NOT NULL DEFAULT 0,
    policy_decision text NOT NULL
        CHECK (policy_decision IN ('allowed','denied_scope','denied_budget','denied_rate',
                                   'timeout','circuit_open','error','fallback')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS extension_activity_idx
    ON extension_activity (tenant_id, workspace_id, extension_id, created_at DESC);

CREATE OR REPLACE FUNCTION extension_activity_block_mutation() RETURNS trigger AS $$
BEGIN RAISE EXCEPTION 'extension_activity is append-only'; END; $$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS extension_activity_no_update ON extension_activity;
CREATE TRIGGER extension_activity_no_update BEFORE UPDATE OR DELETE ON extension_activity
    FOR EACH ROW EXECUTE FUNCTION extension_activity_block_mutation();

-- Per-extension event subscription (which event types an ingestion-transform / connector wants).
CREATE TABLE IF NOT EXISTS extension_subscriptions (
    extension_id uuid NOT NULL REFERENCES extensions(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    event_type text NOT NULL,
    PRIMARY KEY (extension_id, event_type)
);

-- Circuit-breaker / health state per extension (mutable operational state, not audit).
CREATE TABLE IF NOT EXISTS extension_health (
    extension_id uuid PRIMARY KEY REFERENCES extensions(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    state text NOT NULL DEFAULT 'closed' CHECK (state IN ('closed','open','half_open')),
    consecutive_failures integer NOT NULL DEFAULT 0,
    opened_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);
