-- Migration 052: Metric-definition registry — versioned, immutable metric definitions.

CREATE TABLE IF NOT EXISTS metric_definitions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NULL REFERENCES tenants(id) ON DELETE CASCADE,
    key text NOT NULL,
    version integer NOT NULL CHECK (version > 0),
    title text NOT NULL,
    semantics text NOT NULL,
    unit text NOT NULL CHECK (unit IN ('count', 'rate', 'currency', 'ratio')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (COALESCE(tenant_id, '00000000-0000-0000-0000-000000000000'), key, version)
);

CREATE INDEX IF NOT EXISTS metric_definitions_lookup_idx
    ON metric_definitions (COALESCE(tenant_id, '00000000-0000-0000-0000-000000000000'), key, version);

-- Block UPDATE/DELETE on metric_definitions (append-only).
CREATE OR REPLACE FUNCTION metric_definitions_block_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'metric_definitions is append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS metric_definitions_no_update ON metric_definitions;
CREATE TRIGGER metric_definitions_no_update BEFORE UPDATE OR DELETE ON metric_definitions
    FOR EACH ROW EXECUTE FUNCTION metric_definitions_block_mutation();

REVOKE UPDATE, DELETE ON metric_definitions FROM PUBLIC;

-- Seed canonical metric definitions at version 1.
INSERT INTO metric_definitions (tenant_id, key, version, title, semantics, unit, created_at)
VALUES
    (NULL, 'delivered', 1, 'Delivered', 'Messages successfully sent to the destination channel', 'count', now()),
    (NULL, 'opened', 1, 'Opened', 'Messages opened by recipients', 'count', now()),
    (NULL, 'clicked', 1, 'Clicked', 'Links clicked within messages', 'count', now()),
    (NULL, 'converted', 1, 'Converted', 'Conversions attributed to the campaign', 'count', now()),
    (NULL, 'bounce_rate', 1, 'Bounce Rate', 'Percentage of messages that bounced', 'rate', now()),
    (NULL, 'complaint_rate', 1, 'Complaint Rate', 'Percentage of messages marked as complaints', 'rate', now()),
    (NULL, 'conversion_rate', 1, 'Conversion Rate', 'Percentage of sent messages that converted', 'rate', now()),
    (NULL, 'spend', 1, 'Spend', 'Total cost of campaign delivery', 'currency', now())
ON CONFLICT DO NOTHING;

-- Widen api_keys.scopes DEFAULT array to include reports:write.
ALTER TABLE api_keys ALTER COLUMN scopes SET DEFAULT ARRAY[
    'events:write','profiles:read','schemas:read','schemas:write',
    'api_keys:read','api_keys:write','privacy:write','operations:read','operations:write',
    'users:read','users:write','roles:read','roles:write',
    'segments:read','segments:write','templates:read','templates:write',
    'campaigns:read','campaigns:write','suppressions:read','suppressions:write',
    'journeys:read','journeys:write','journeys:publish',
    'experiments:read','experiments:write','reports:read','reports:write',
    'device_tokens:read','device_tokens:write',
    'ai:read','ai:configure','ai:invoke','prompts:read','prompts:write',
    'scoring:read','scoring:write','scoring:compute',
    'forms:read','forms:write','forms:publish','pages:read','pages:write','pages:publish',
    'assets:read','assets:write','links:read','links:write',
    'companies:read','companies:write','stages:read','stages:write','imports:read','imports:write',
    'extensions:read','extensions:write','extensions:install',
    'connectors:read','connectors:write','connectors:run',
    'messages:read','messages:write',
    'flags:read','flags:write'
];
