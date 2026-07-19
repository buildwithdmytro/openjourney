-- Migration 043: Connector pipelines, immutable definitions, and connector job governance.

ALTER TABLE extension_versions DROP CONSTRAINT IF EXISTS extension_versions_transport_check;
ALTER TABLE extension_versions ADD CONSTRAINT extension_versions_transport_check
    CHECK (transport IN ('remote_http', 'wasm', 'native'));

CREATE TABLE IF NOT EXISTS connector_pipelines (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    app_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    connector_extension_id uuid NOT NULL REFERENCES extensions(id) ON DELETE RESTRICT,
    name text NOT NULL,
    direction text NOT NULL CHECK (direction IN ('source', 'sink', 'export')),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'enabled', 'disabled')),
    current_version_id uuid,
    schedule_enabled boolean NOT NULL DEFAULT false,
    schedule_interval_seconds integer,
    next_run_at timestamptz,
    last_run_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, app_id, name),
    CHECK (schedule_interval_seconds IS NULL OR schedule_interval_seconds > 0)
);

CREATE INDEX IF NOT EXISTS connector_pipelines_due_idx
    ON connector_pipelines (schedule_enabled, next_run_at);

CREATE TABLE IF NOT EXISTS connector_pipeline_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id uuid NOT NULL REFERENCES connector_pipelines(id) ON DELETE CASCADE,
    version integer NOT NULL CHECK (version > 0),
    mapping_key text NOT NULL,
    mapping jsonb NOT NULL,
    definition_sha text NOT NULL,
    created_by_user_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (pipeline_id, version)
);

CREATE OR REPLACE FUNCTION connector_pipeline_versions_block_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'connector_pipeline_versions is append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS connector_pipeline_versions_no_update ON connector_pipeline_versions;
CREATE TRIGGER connector_pipeline_versions_no_update
    BEFORE UPDATE OR DELETE ON connector_pipeline_versions
    FOR EACH ROW EXECUTE FUNCTION connector_pipeline_versions_block_mutation();

REVOKE UPDATE, DELETE ON connector_pipeline_versions FROM PUBLIC;

ALTER TABLE connector_pipelines
    DROP CONSTRAINT IF EXISTS connector_pipelines_current_version_fk;
ALTER TABLE connector_pipelines
    ADD CONSTRAINT connector_pipelines_current_version_fk
    FOREIGN KEY (current_version_id) REFERENCES connector_pipeline_versions(id);

ALTER TABLE operation_jobs DROP CONSTRAINT IF EXISTS operation_jobs_job_type_check;
ALTER TABLE operation_jobs ADD CONSTRAINT operation_jobs_job_type_check
    CHECK (job_type IN ('privacy.export', 'privacy.delete', 'profiles.replay', 'retention.enforce',
                        'ai.generate', 'scores.compute', 'profiles.import', 'connector.run',
                        'warehouse.sync', 'reverse_etl.run', 'export.replay'));

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
    'connectors:read','connectors:write','connectors:run'
];
