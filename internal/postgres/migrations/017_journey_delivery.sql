-- Message-node delivery intents (orchestration emits; delivery worker sends).
-- Carries the per-recipient explainable record (journey analog of delivery_attempts).
CREATE TABLE IF NOT EXISTS journey_message_intents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id uuid NOT NULL REFERENCES journey_runs(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    journey_id uuid NOT NULL,
    journey_version_id uuid NOT NULL,
    node_id text NOT NULL,
    profile_id uuid NOT NULL,
    template_id uuid NOT NULL REFERENCES templates(id),
    channel text NOT NULL,
    endpoint text NOT NULL,
    transactional boolean NOT NULL DEFAULT false,     -- bypass quiet-hours/fatigue + priority
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','processing','completed','failed','dead')),
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    locked_until timestamptz,
    decision text,                                    -- sent/suppressed/no_consent/fatigued/render_failed/send_failed
    reason text,
    provider_message_id text,
    policy_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
    error_message text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (run_id, node_id)                          -- effectively-once per node execution
);
CREATE INDEX IF NOT EXISTS journey_message_intents_due_idx
    ON journey_message_intents (status, transactional DESC, available_at);

-- Quiet hours + per-tenant journey caps (extend tenant_quotas, like 013_tenant_fatigue_quotas.sql).
ALTER TABLE tenant_quotas
ADD COLUMN IF NOT EXISTS quiet_hours_start integer CHECK (quiet_hours_start BETWEEN 0 AND 23),
ADD COLUMN IF NOT EXISTS quiet_hours_end integer CHECK (quiet_hours_end BETWEEN 0 AND 23),
ADD COLUMN IF NOT EXISTS default_timezone text NOT NULL DEFAULT 'UTC',
ADD COLUMN IF NOT EXISTS max_active_runs_per_profile integer NOT NULL DEFAULT 25 CHECK (max_active_runs_per_profile >= 0);
