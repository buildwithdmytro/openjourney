-- Migration 046: namespaced identity configuration and reversible merge provenance.

CREATE TABLE IF NOT EXISTS identity_namespaces (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    app_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    namespace text NOT NULL,
    priority integer NOT NULL,
    is_unique boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, app_id, namespace)
);

-- Existing tenants receive the v1 defaults; tenant-specific configuration remains editable.
INSERT INTO identity_namespaces (tenant_id, app_id, namespace, priority, is_unique)
SELECT t.id, a.id, defaults.namespace, defaults.priority, true
FROM tenants AS t
JOIN applications AS a ON a.tenant_id = t.id
CROSS JOIN (VALUES ('user_id', 10), ('email', 20), ('phone', 30)) AS defaults(namespace, priority)
ON CONFLICT (tenant_id, app_id, namespace) DO NOTHING;

ALTER TABLE profiles ADD COLUMN IF NOT EXISTS merged_into uuid;
CREATE INDEX IF NOT EXISTS profiles_merged_into_idx ON profiles (merged_into);

ALTER TABLE identity_merges ADD COLUMN IF NOT EXISTS winner_policy text NOT NULL DEFAULT 'v1';
ALTER TABLE identity_merges ADD COLUMN IF NOT EXISTS reversible boolean NOT NULL DEFAULT true;
ALTER TABLE identity_merges ADD COLUMN IF NOT EXISTS reversal_ref text;
ALTER TABLE identity_merges ADD COLUMN IF NOT EXISTS undone_at timestamptz;
ALTER TABLE identity_merges ADD COLUMN IF NOT EXISTS actor_user_id uuid;
ALTER TABLE identity_merges ADD COLUMN IF NOT EXISTS actor_type text;

CREATE INDEX IF NOT EXISTS identity_aliases_lookup_idx
    ON identity_aliases (tenant_id, app_id, namespace, value);
