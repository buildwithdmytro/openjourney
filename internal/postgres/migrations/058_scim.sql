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

CREATE TABLE IF NOT EXISTS scim_group_mappings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    external_group text NOT NULL,
    team_id uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, external_group)
);
CREATE INDEX IF NOT EXISTS scim_group_mappings_tenant_idx ON scim_group_mappings(tenant_id);

CREATE TABLE IF NOT EXISTS saml_providers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    idp_entity_id text NOT NULL,
    idp_sso_url text NOT NULL,
    idp_cert text NOT NULL,
    sp_entity_id text NOT NULL,
    default_role_id uuid REFERENCES roles(id) ON DELETE SET NULL,
    enabled bool NOT NULL DEFAULT true,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('draft', 'active', 'disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, idp_entity_id)
);
CREATE INDEX IF NOT EXISTS saml_providers_tenant_idx ON saml_providers(tenant_id);


