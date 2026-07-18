-- Migration 037: generalized short links with UTM attribution.

CREATE TABLE IF NOT EXISTS short_links (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    slug text NOT NULL,
    destination_url text NOT NULL,
    utm jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, slug)
);
