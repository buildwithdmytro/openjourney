-- Migration 026: Create prompts, prompt_versions, and ai_budget_usage tables for AI registry and budget tracking.

CREATE TABLE IF NOT EXISTS prompts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    name text NOT NULL,
    task_type text NOT NULL CHECK (task_type IN
        ('content_draft','audience_dsl','journey_draft','performance_summary','moderation')),
    current_version_id uuid,
    latest_version integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, name)
);

CREATE TABLE IF NOT EXISTS prompt_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    prompt_id uuid NOT NULL REFERENCES prompts(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    version integer NOT NULL,
    template text NOT NULL,                    -- system+instruction template (Liquid)
    input_schema jsonb NOT NULL,               -- JSON Schema the tool input must satisfy
    output_schema jsonb NOT NULL,              -- JSON Schema the model output MUST conform to
    provider text NOT NULL,
    model text NOT NULL,
    params jsonb NOT NULL DEFAULT '{}'::jsonb,  -- temperature, max_tokens, ...
    safety_policy jsonb NOT NULL DEFAULT '{}'::jsonb,
    manifest_key text NOT NULL,                -- content-addressed blob of the frozen version
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
    eval_status text NOT NULL DEFAULT 'pending' CHECK (eval_status IN ('pending','passed','failed')),
    published_by uuid,                         -- human approver (NULL until published)
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (prompt_id, version)
);

-- Per-tenant budget rollup (cheap read for the gateway's over-budget check).
CREATE TABLE IF NOT EXISTS ai_budget_usage (
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    period text NOT NULL,                      -- 'YYYY-MM'
    cost_cents bigint NOT NULL DEFAULT 0,
    input_tokens bigint NOT NULL DEFAULT 0,
    output_tokens bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, workspace_id, period)
);
