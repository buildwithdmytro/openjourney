-- Migration 040: CSV import requests and the leased import job type.

CREATE TABLE IF NOT EXISTS import_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    app_id uuid NOT NULL,
    requested_by uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('profiles','companies','suppressions')),
    source_blob_key text NOT NULL,
    mapping jsonb NOT NULL,
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','processing','complete','failed')),
    total_rows integer NOT NULL DEFAULT 0,
    imported_rows integer NOT NULL DEFAULT 0,
    failed_rows integer NOT NULL DEFAULT 0,
    result_ref text,
    error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);

CREATE INDEX IF NOT EXISTS import_requests_scope_idx
    ON import_requests (tenant_id, workspace_id, created_at DESC);

ALTER TABLE operation_jobs DROP CONSTRAINT IF EXISTS operation_jobs_job_type_check;
ALTER TABLE operation_jobs ADD CONSTRAINT operation_jobs_job_type_check
    CHECK (job_type IN ('privacy.export','privacy.delete','profiles.replay','retention.enforce',
                        'ai.generate','scores.compute','profiles.import'));

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
    'forms:read','forms:write','forms:publish',
    'pages:read','pages:write','pages:publish',
    'assets:read','assets:write','links:read','links:write',
    'companies:read','companies:write','stages:read','stages:write',
    'imports:read','imports:write'
];
