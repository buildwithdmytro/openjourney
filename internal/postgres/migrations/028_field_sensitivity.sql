-- Migration 028: field-level sensitivity and model-egress policy.

CREATE TABLE IF NOT EXISTS field_classifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    entity_type text NOT NULL CHECK (entity_type IN ('profile','event')),
    field_path text NOT NULL,
    classification text NOT NULL
        CHECK (classification IN ('public','internal','confidential','restricted')),
    send_to_model text NOT NULL DEFAULT 'redact'
        CHECK (send_to_model IN ('allow','redact','tokenize','deny')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, entity_type, field_path)
);

CREATE INDEX IF NOT EXISTS field_classifications_scope_idx
    ON field_classifications (tenant_id, workspace_id, entity_type);
