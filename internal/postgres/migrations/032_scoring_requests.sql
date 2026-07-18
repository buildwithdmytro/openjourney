-- Migration 032: Create scoring_requests table.

CREATE TABLE IF NOT EXISTS scoring_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    requested_by uuid NOT NULL,
    scoring_model_id uuid NOT NULL REFERENCES scoring_models(id) ON DELETE CASCADE,
    segment_id uuid NOT NULL REFERENCES segments(id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','complete','failed')),
    error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);
