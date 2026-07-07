CREATE TYPE segment_type AS ENUM ('static', 'dynamic', 'snapshot');

CREATE TABLE IF NOT EXISTS segments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    name text NOT NULL,
    description text,
    type segment_type NOT NULL DEFAULT 'dynamic',
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
    dsl jsonb NOT NULL DEFAULT '{}'::jsonb,
    version integer NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS segment_members (
    segment_id uuid NOT NULL REFERENCES segments(id) ON DELETE CASCADE,
    profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    membership text NOT NULL DEFAULT 'include' CHECK (membership IN ('include','exclude')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (segment_id, profile_id)
);

CREATE INDEX IF NOT EXISTS segment_members_profile_idx
    ON segment_members (tenant_id, profile_id);

ALTER TABLE api_keys ALTER COLUMN scopes SET DEFAULT ARRAY[
    'events:write','profiles:read','schemas:read','schemas:write',
    'api_keys:read','api_keys:write','privacy:write','operations:read','operations:write',
    'users:read','users:write','roles:read','roles:write',
    'segments:read','segments:write','templates:read','templates:write',
    'campaigns:read','campaigns:write','suppressions:read','suppressions:write'
];
