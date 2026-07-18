-- Migration 036: versioned landing pages and content-addressed assets.

CREATE TABLE IF NOT EXISTS landing_pages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    slug text NOT NULL,
    name text NOT NULL,
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','archived')),
    draft jsonb NOT NULL DEFAULT '{}'::jsonb,
    current_version_id uuid,
    latest_version integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, slug)
);

CREATE TABLE IF NOT EXISTS page_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    page_id uuid NOT NULL REFERENCES landing_pages(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    version integer NOT NULL,
    definition jsonb NOT NULL,
    manifest_key text NOT NULL,
    published_by uuid NOT NULL,
    published_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (page_id, version)
);

CREATE TABLE IF NOT EXISTS assets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    filename text NOT NULL,
    content_type text NOT NULL,
    blob_key text NOT NULL,
    size_bytes bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, blob_key)
);
