-- Migration 041: Create extension registry tables and add extensions scopes.

CREATE TABLE IF NOT EXISTS extensions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    name text NOT NULL,
    publisher text NOT NULL,
    current_version_id uuid,
    latest_version integer NOT NULL DEFAULT 0,
    status text NOT NULL DEFAULT 'installed' CHECK (status IN ('installed','enabled','disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, name)
);

CREATE TABLE IF NOT EXISTS extension_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    extension_id uuid NOT NULL REFERENCES extensions(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    version integer NOT NULL,
    kind text NOT NULL CHECK (kind IN
        ('channel_provider','journey_action','journey_condition','ai_provider',
         'ingestion_transform','template_function','connector')),
    transport text NOT NULL CHECK (transport IN ('remote_http','wasm')),
    manifest jsonb NOT NULL,                    -- full §10 manifest (capabilities, io schemas, limits, ...)
    requested_scopes text[] NOT NULL DEFAULT '{}',
    signature text NOT NULL,                    -- publisher JWS over the canonical manifest
    signing_key_id text NOT NULL,               -- which trusted publisher key verified it
    wasm_blob_key text,                         -- content-addressed module (transport='wasm')
    manifest_key text NOT NULL,                 -- content-addressed frozen manifest
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
    installed_by uuid,                          -- human approver
    installed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (extension_id, version)
);

-- Per-extension config + secret refs + network allowlist + limits (copy of ai_provider_configs shape).
CREATE TABLE IF NOT EXISTS extension_configs (
    extension_id uuid PRIMARY KEY REFERENCES extensions(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    config jsonb NOT NULL DEFAULT '{}'::jsonb,  -- {..., *_ref secret names, base_url}
    endpoint_allowlist text[] NOT NULL DEFAULT '{}',
    timeout_ms integer NOT NULL DEFAULT 2000,
    max_memory_mb integer NOT NULL DEFAULT 64,  -- wasm only
    monthly_budget_cents bigint NOT NULL DEFAULT 0,
    rate_per_min integer NOT NULL DEFAULT 600,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Granted scopes (a tenant admin grants a subset the extension asked for).
CREATE TABLE IF NOT EXISTS extension_grants (
    extension_id uuid NOT NULL REFERENCES extensions(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    scope text NOT NULL,
    granted_by uuid NOT NULL,
    granted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (extension_id, scope)
);

-- Outbound connector job type.
ALTER TABLE operation_jobs DROP CONSTRAINT IF EXISTS operation_jobs_job_type_check;
ALTER TABLE operation_jobs ADD CONSTRAINT operation_jobs_job_type_check
    CHECK (job_type IN ('privacy.export','privacy.delete','profiles.replay','retention.enforce',
                        'ai.generate','scores.compute','profiles.import','connector.run'));

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
    'extensions:read','extensions:write','extensions:install'
];
