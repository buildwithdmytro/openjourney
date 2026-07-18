-- Migration 027: immutable AI activity audit, budget usage, and generation status.

-- Immutable, append-only AI-activity audit (distinct from audit_events).
CREATE TABLE IF NOT EXISTS ai_activity (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    actor_user_id uuid,
    action text NOT NULL,
    provider text NOT NULL,
    model text NOT NULL,
    prompt_version_id uuid REFERENCES prompt_versions(id),
    retrieval_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    tool_calls jsonb NOT NULL DEFAULT '[]'::jsonb,
    classification text,
    input_tokens integer NOT NULL DEFAULT 0,
    output_tokens integer NOT NULL DEFAULT 0,
    cost_cents bigint NOT NULL DEFAULT 0,
    latency_ms integer NOT NULL DEFAULT 0,
    policy_decision text NOT NULL,
    approver_user_id uuid,
    output_ref text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ai_activity_idx
    ON ai_activity (tenant_id, workspace_id, created_at DESC);

-- Per-tenant budget rollup used by the gateway's over-budget check.
CREATE TABLE IF NOT EXISTS ai_budget_usage (
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    period text NOT NULL,
    cost_cents bigint NOT NULL DEFAULT 0,
    input_tokens bigint NOT NULL DEFAULT 0,
    output_tokens bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, workspace_id, period)
);

-- Client-facing status resource for asynchronous generation.
CREATE TABLE IF NOT EXISTS ai_generation_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    requested_by uuid NOT NULL,
    task_type text NOT NULL,
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','processing','complete','failed')),
    result_ref text,
    error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);
