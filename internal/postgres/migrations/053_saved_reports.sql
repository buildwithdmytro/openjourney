-- Migration 053: Saved reports CRUD — workspace-scoped report configurations.

CREATE TABLE IF NOT EXISTS saved_reports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    report_type text NOT NULL CHECK (report_type IN ('funnel', 'deliverability', 'retention', 'cohort', 'growth', 'cost', 'experiment')),
    query jsonb NOT NULL,
    created_by_user_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, name)
);

CREATE INDEX IF NOT EXISTS saved_reports_workspace_idx
    ON saved_reports (tenant_id, workspace_id);

CREATE INDEX IF NOT EXISTS saved_reports_created_idx
    ON saved_reports (created_at DESC);
