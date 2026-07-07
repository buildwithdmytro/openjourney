-- Campaigns schema: campaigns, delivery_jobs, and delivery_attempts
CREATE TABLE IF NOT EXISTS campaigns (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    name text NOT NULL,
    description text,
    segment_id uuid NOT NULL REFERENCES segments(id),
    template_id uuid NOT NULL REFERENCES templates(id),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'scheduled', 'building', 'sending', 'paused', 'completed', 'failed', 'archived')),
    scheduled_at timestamptz,
    manifest_key text,
    segment_version integer NOT NULL DEFAULT 1,
    template_version integer NOT NULL DEFAULT 1,
    evaluated_at timestamptz,
    recipient_count integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS campaigns_tenant_idx ON campaigns(tenant_id, workspace_id);
CREATE INDEX IF NOT EXISTS campaigns_status_scheduled_idx ON campaigns(status, scheduled_at);

CREATE TABLE IF NOT EXISTS delivery_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id uuid NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    shard integer NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    recipients jsonb NOT NULL DEFAULT '[]'::jsonb,
    claimed_at timestamptz,
    claimed_by text,
    error_message text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS delivery_jobs_status_idx ON delivery_jobs(status);
CREATE INDEX IF NOT EXISTS delivery_jobs_campaign_idx ON delivery_jobs(campaign_id);

CREATE TABLE IF NOT EXISTS delivery_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id uuid NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    profile_id uuid NOT NULL,
    channel text NOT NULL,
    endpoint text NOT NULL,
    decision text NOT NULL CHECK (decision IN ('sent', 'suppressed', 'no_consent', 'fatigued', 'failed')),
    reason text,
    provider_message_id text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (campaign_id, profile_id, channel)
);

CREATE INDEX IF NOT EXISTS delivery_attempts_lookup_idx ON delivery_attempts(campaign_id, profile_id);
