-- Migration 060: Data-subject-request workflow enhancement, maker-checker separation of duties policies, permissions catalog update, and default API key scopes.
CREATE TABLE IF NOT EXISTS maker_checker_policies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    resource_type text NOT NULL,
    require_checker boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, resource_type)
);

ALTER TABLE privacy_requests
    ADD COLUMN IF NOT EXISTS verification_status text DEFAULT 'unverified' CHECK (verification_status IN ('unverified','verified','rejected')),
    ADD COLUMN IF NOT EXISTS verification_token_hash text,
    ADD COLUMN IF NOT EXISTS sla_due_at timestamptz;

ALTER TABLE privacy_requests DROP CONSTRAINT IF EXISTS privacy_requests_status_check;
ALTER TABLE privacy_requests ADD CONSTRAINT privacy_requests_status_check
    CHECK (status IN ('pending','in_progress','completed','failed','rejected'));

INSERT INTO permissions (key, resource, verb, description, system) VALUES
    ('audit:read', 'audit', 'read', 'Read tamper-evident audit logs', true),
    ('privacy:read', 'privacy', 'read', 'Read privacy data-subject requests', true),
    ('privacy:approve', 'privacy', 'approve', 'Approve/verify privacy requests', true),
    ('teams:read', 'teams', 'read', 'Read teams and team memberships', true),
    ('teams:write', 'teams', 'write', 'Manage teams and team memberships', true),
    ('scim:manage', 'scim', 'manage', 'Manage SCIM 2.0 provisioning tokens and sync', true)
ON CONFLICT (key) DO NOTHING;

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
    'teams:read','teams:write','scim:manage','audit:read','privacy:read','privacy:approve'
];
