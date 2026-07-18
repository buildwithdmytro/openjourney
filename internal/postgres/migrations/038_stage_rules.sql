-- Migration 038: lifecycle stage rules.

CREATE TABLE IF NOT EXISTS stage_rules (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    stage text NOT NULL,
    segment_id uuid REFERENCES segments(id),
    priority integer NOT NULL DEFAULT 0,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, stage)
);

CREATE INDEX IF NOT EXISTS stage_rules_scope_idx
    ON stage_rules (tenant_id, workspace_id, enabled, priority DESC);
