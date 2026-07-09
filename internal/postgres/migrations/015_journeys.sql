-- Migration 015: Journey draft containers and immutable published versions.
CREATE TABLE IF NOT EXISTS journeys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    name text NOT NULL,
    description text,
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','archived')),
    graph jsonb NOT NULL DEFAULT '{}'::jsonb,
    latest_version integer NOT NULL DEFAULT 0,
    current_version_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS journeys_tenant_idx ON journeys (tenant_id, workspace_id);

CREATE TABLE IF NOT EXISTS journey_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    journey_id uuid NOT NULL REFERENCES journeys(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    version integer NOT NULL,
    graph jsonb NOT NULL,
    manifest_key text,
    entry_kind text NOT NULL CHECK (entry_kind IN ('event','scheduled')),
    entry_event_type text,
    entry_segment_id uuid REFERENCES segments(id),
    entry_schedule text,
    reentry_policy text NOT NULL DEFAULT 'once' CHECK (reentry_policy IN ('once','always','after_exit')),
    max_reentries integer NOT NULL DEFAULT 0,
    late_policy text NOT NULL DEFAULT 'run' CHECK (late_policy IN ('run','skip','reschedule')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','paused','archived')),
    published_by uuid,
    published_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (journey_id, version)
);

CREATE INDEX IF NOT EXISTS journey_versions_active_event_idx
    ON journey_versions (tenant_id, entry_event_type) WHERE status='active' AND entry_kind='event';
CREATE INDEX IF NOT EXISTS journey_versions_scheduled_idx
    ON journey_versions (status, entry_kind) WHERE entry_kind='scheduled';

ALTER TABLE api_keys ALTER COLUMN scopes SET DEFAULT ARRAY[
    'events:write','profiles:read','schemas:read','schemas:write',
    'api_keys:read','api_keys:write','privacy:write','operations:read','operations:write',
    'users:read','users:write','roles:read','roles:write',
    'segments:read','segments:write','templates:read','templates:write',
    'campaigns:read','campaigns:write','suppressions:read','suppressions:write',
    'journeys:read','journeys:write','journeys:publish'
];
