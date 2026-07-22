-- Migration 058: tenant-scoped SCIM bearer credentials.
CREATE TABLE IF NOT EXISTS scim_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    token_hash text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    disabled_at timestamptz
);
CREATE INDEX IF NOT EXISTS scim_tokens_tenant_idx ON scim_tokens(tenant_id);
