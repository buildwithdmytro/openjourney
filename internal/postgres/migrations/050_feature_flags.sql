-- Migration 050: Feature flags foundation — environment-scoped flags with versioning.

CREATE TABLE IF NOT EXISTS feature_flags (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    app_id uuid NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    environment text NOT NULL CHECK (environment IN ('development','staging','production')),
    key text NOT NULL,
    name text,
    description text,
    flag_type text NOT NULL CHECK (flag_type IN ('boolean','string','number','json')),
    default_value jsonb NOT NULL,
    variants jsonb NOT NULL DEFAULT '[]'::jsonb,
    targeting_rules jsonb NOT NULL DEFAULT '[]'::jsonb,
    rollout_pct integer NOT NULL DEFAULT 0 CHECK (rollout_pct BETWEEN 0 AND 100),
    seed text NOT NULL,
    enabled boolean NOT NULL DEFAULT false,
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','disabled')),
    current_version_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, app_id, environment, key)
);

CREATE INDEX IF NOT EXISTS feature_flags_lookup_idx
    ON feature_flags (tenant_id, app_id, environment, status);

CREATE TABLE IF NOT EXISTS feature_flag_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    flag_id uuid NOT NULL REFERENCES feature_flags(id) ON DELETE CASCADE,
    version integer NOT NULL CHECK (version > 0),
    definition_key text NOT NULL,
    definition_sha text NOT NULL,
    definition jsonb NOT NULL,
    created_by_user_id uuid REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (flag_id, version)
);

CREATE INDEX IF NOT EXISTS feature_flag_versions_scope_idx
    ON feature_flag_versions (flag_id, version);

CREATE OR REPLACE FUNCTION feature_flag_versions_block_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'feature_flag_versions is append-only';
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS feature_flag_versions_no_update ON feature_flag_versions;
CREATE TRIGGER feature_flag_versions_no_update BEFORE UPDATE OR DELETE ON feature_flag_versions
    FOR EACH ROW EXECUTE FUNCTION feature_flag_versions_block_mutation();

REVOKE UPDATE, DELETE ON feature_flag_versions FROM PUBLIC;

CREATE TABLE IF NOT EXISTS feature_flag_exposures (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    app_id uuid NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    flag_id uuid NOT NULL REFERENCES feature_flags(id) ON DELETE CASCADE,
    environment text NOT NULL,
    variant text NOT NULL,
    exposures bigint NOT NULL DEFAULT 0,
    first_seen timestamptz,
    last_seen timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (flag_id, environment, variant)
);

CREATE INDEX IF NOT EXISTS feature_flag_exposures_lookup_idx
    ON feature_flag_exposures (tenant_id, app_id, flag_id);

-- Widen api_keys.scopes DEFAULT array to include flags:read and flags:write.
ALTER TABLE api_keys ALTER COLUMN scopes SET DEFAULT ARRAY[
    'events:write','profiles:read','schemas:read','schemas:write',
    'api_keys:read','api_keys:write','privacy:write','operations:read','operations:write',
    'users:read','users:write','roles:read','roles:write',
    'segments:read','segments:write','templates:read','templates:write',
    'campaigns:read','campaigns:write','suppressions:read','suppressions:write',
    'journeys:read','journeys:write','journeys:publish',
    'experiments:read','experiments:write','reports:read',
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
