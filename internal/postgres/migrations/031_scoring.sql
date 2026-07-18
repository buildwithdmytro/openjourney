-- Migration 031: Create scoring tables, widen operation_jobs.job_type, and add scoring scopes.

CREATE TABLE IF NOT EXISTS scoring_models (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    name text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('expression','llm')),
    current_version_id uuid,
    latest_version integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, name)
);

CREATE TABLE IF NOT EXISTS scoring_model_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    scoring_model_id uuid NOT NULL REFERENCES scoring_models(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    version integer NOT NULL,
    score_name text NOT NULL,                 -- e.g. 'purchase_propensity'
    definition jsonb NOT NULL,                -- expression: {expr, inputs}; llm: {prompt_version_id}
    output_min numeric NOT NULL DEFAULT 0,
    output_max numeric NOT NULL DEFAULT 1,
    manifest_key text NOT NULL,               -- content-addressed frozen blob
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
    eval_status text NOT NULL DEFAULT 'pending' CHECK (eval_status IN ('pending','passed','failed')),
    published_by uuid,
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (scoring_model_id, version)
);

-- Per-profile computed scores (latest per model+score_name). Queryable by the audience compiler.
CREATE TABLE IF NOT EXISTS profile_scores (
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    app_id uuid NOT NULL,
    profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    scoring_model_id uuid NOT NULL REFERENCES scoring_models(id) ON DELETE CASCADE,
    score_name text NOT NULL,
    value numeric NOT NULL,
    model_version integer NOT NULL,           -- which immutable version produced it
    computed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, profile_id, scoring_model_id, score_name)
);
CREATE INDEX IF NOT EXISTS profile_scores_query_idx
    ON profile_scores (tenant_id, workspace_id, scoring_model_id, score_name, value);

ALTER TABLE operation_jobs DROP CONSTRAINT IF EXISTS operation_jobs_job_type_check;
ALTER TABLE operation_jobs ADD CONSTRAINT operation_jobs_job_type_check
    CHECK (job_type IN ('privacy.export','privacy.delete','profiles.replay','retention.enforce',
                        'ai.generate','scores.compute'));

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
    'scoring:read','scoring:write','scoring:compute'
];
