-- Migration 020: Frozen conversion goals and projection-maintained analytics facts.
ALTER TABLE campaigns
    ADD COLUMN IF NOT EXISTS conversion_goal jsonb,
    ADD COLUMN IF NOT EXISTS attribution_window interval;

ALTER TABLE journey_versions
    ADD COLUMN IF NOT EXISTS conversion_goal jsonb,
    ADD COLUMN IF NOT EXISTS attribution_window interval;

-- experiments.primary_goal is created by 018_experiments.sql.

CREATE TABLE IF NOT EXISTS engagement_facts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    source_type text NOT NULL CHECK (source_type IN ('campaign','journey')),
    source_id uuid NOT NULL,
    node_id text,
    experiment_id uuid,
    variant text,
    profile_id uuid,
    channel text,
    event_type text NOT NULL
        CHECK (event_type IN ('delivered','opened','clicked','bounced','complained')),
    occurred_at timestamptz NOT NULL,
    source_event_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_event_id, event_type)
);

CREATE INDEX IF NOT EXISTS engagement_facts_report_idx
    ON engagement_facts (tenant_id, workspace_id, source_type, source_id, variant, event_type);

CREATE TABLE IF NOT EXISTS conversion_facts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    source_type text NOT NULL CHECK (source_type IN ('campaign','journey')),
    source_id uuid NOT NULL,
    experiment_id uuid,
    variant text,
    profile_id uuid NOT NULL,
    goal_name text NOT NULL,
    value numeric NOT NULL DEFAULT 0,
    occurred_at timestamptz NOT NULL,
    attributed_send_at timestamptz NOT NULL,
    source_event_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_event_id, goal_name)
);

CREATE INDEX IF NOT EXISTS conversion_facts_report_idx
    ON conversion_facts (tenant_id, workspace_id, source_type, source_id, variant, goal_name);
