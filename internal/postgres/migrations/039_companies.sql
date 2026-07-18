-- Migration 039: company profiles and profile membership.

CREATE TABLE IF NOT EXISTS companies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    app_id uuid NOT NULL,
    external_id text,
    name text NOT NULL,
    attributes jsonb NOT NULL DEFAULT '{}'::jsonb,
    version integer NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, app_id, external_id)
);

CREATE TABLE IF NOT EXISTS company_members (
    tenant_id uuid NOT NULL,
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    role text,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (company_id, profile_id)
);

CREATE INDEX IF NOT EXISTS company_members_profile_idx
    ON company_members (tenant_id, profile_id);
