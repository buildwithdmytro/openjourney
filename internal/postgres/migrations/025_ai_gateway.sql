-- Migration 025: Create ai_provider_configs table, widen operation_jobs.job_type, and add AI/prompts scopes.

CREATE TABLE IF NOT EXISTS ai_provider_configs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    provider text NOT NULL CHECK (provider IN ('fake','anthropic','openai')),
    is_default boolean NOT NULL DEFAULT false,
    config jsonb NOT NULL DEFAULT '{}'::jsonb,   -- {api_key_ref, base_url, default_model, cheap_model, params}
    endpoint_allowlist text[] NOT NULL DEFAULT '{}',   -- explicit local/self-host hosts (opt-in past SSRF guard)
    fallback_provider text,
    monthly_budget_cents bigint NOT NULL DEFAULT 0,     -- 0 = unlimited (dev only); enforced at gateway
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS ai_provider_default_idx
    ON ai_provider_configs (tenant_id, workspace_id) WHERE is_default;

-- Widen the generic job queue to admit AI generation jobs.
ALTER TABLE operation_jobs DROP CONSTRAINT IF EXISTS operation_jobs_job_type_check;
ALTER TABLE operation_jobs ADD CONSTRAINT operation_jobs_job_type_check
    CHECK (job_type IN ('privacy.export','privacy.delete','profiles.replay','retention.enforce','ai.generate'));

ALTER TABLE api_keys ALTER COLUMN scopes SET DEFAULT ARRAY[
    'events:write','profiles:read','schemas:read','schemas:write',
    'api_keys:read','api_keys:write','privacy:write','operations:read','operations:write',
    'users:read','users:write','roles:read','roles:write',
    'segments:read','segments:write','templates:read','templates:write',
    'campaigns:read','campaigns:write','suppressions:read','suppressions:write',
    'journeys:read','journeys:write','journeys:publish',
    'experiments:read','experiments:write','reports:read',
    'device_tokens:read','device_tokens:write',
    'ai:read','ai:configure','ai:invoke','prompts:read','prompts:write'
];
