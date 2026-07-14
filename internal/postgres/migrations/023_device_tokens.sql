-- Migration 023: Create device_tokens table and add device_tokens:read/write scopes.

CREATE TABLE IF NOT EXISTS device_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    app_id uuid NOT NULL,
    profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    platform text NOT NULL CHECK (platform IN ('ios','android','web')),
    provider text NOT NULL CHECK (provider IN ('fcm','apns','http','fake')),
    token text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','retired')),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, app_id, token)         -- one row per physical token; upsert on re-register
);

CREATE INDEX IF NOT EXISTS device_tokens_active_idx
    ON device_tokens (tenant_id, workspace_id, profile_id, status) WHERE status = 'active';

ALTER TABLE api_keys ALTER COLUMN scopes SET DEFAULT ARRAY[
    'events:write','profiles:read','schemas:read','schemas:write',
    'api_keys:read','api_keys:write','privacy:write','operations:read','operations:write',
    'users:read','users:write','roles:read','roles:write',
    'segments:read','segments:write','templates:read','templates:write',
    'campaigns:read','campaigns:write','suppressions:read','suppressions:write',
    'journeys:read','journeys:write','journeys:publish',
    'experiments:read','experiments:write','reports:read',
    'device_tokens:read','device_tokens:write'
];
