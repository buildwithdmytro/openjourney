-- Migration 054: Catalogs & Connected Content — reference-data catalogs and governed external-data fetch sources.

CREATE TABLE IF NOT EXISTS catalogs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    app_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    key text NOT NULL,
    name text,
    description text,
    item_key_field text NOT NULL DEFAULT 'id',
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    item_count bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, app_id, key)
);

CREATE INDEX IF NOT EXISTS catalogs_workspace_idx
    ON catalogs (tenant_id, workspace_id);

CREATE TABLE IF NOT EXISTS catalog_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    catalog_id uuid NOT NULL REFERENCES catalogs(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    app_id uuid NOT NULL,
    item_key text NOT NULL,
    payload jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (catalog_id, item_key)
);

CREATE INDEX IF NOT EXISTS catalog_items_lookup_idx
    ON catalog_items (catalog_id, item_key);

CREATE TABLE IF NOT EXISTS connected_content_sources (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    allowed_host text NOT NULL,
    auth_header_name text,
    auth_secret_ref text,
    default_ttl_seconds int NOT NULL DEFAULT 300 CHECK (default_ttl_seconds BETWEEN 0 AND 86400),
    timeout_ms int NOT NULL DEFAULT 2000 CHECK (timeout_ms BETWEEN 100 AND 10000),
    enabled bool NOT NULL DEFAULT false,
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'disabled')),
    created_by_user_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, name)
);

CREATE INDEX IF NOT EXISTS connected_content_sources_tenant_idx
    ON connected_content_sources (tenant_id, workspace_id);

-- Update api_keys.scopes DEFAULT array to include catalogs:read and catalogs:write.
-- Re-declare the entire current array from 052_metric_definitions.sql with the new scopes added.
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
    'flags:read','flags:write',
    'catalogs:read','catalogs:write'
];
