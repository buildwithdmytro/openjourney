CREATE TABLE IF NOT EXISTS schema_migrations (
    version text PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE accepted_events ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'api';
ALTER TABLE accepted_events ADD COLUMN IF NOT EXISTS source_event_id text;
ALTER TABLE accepted_events ADD COLUMN IF NOT EXISTS correlation_id text;
ALTER TABLE accepted_events ADD COLUMN IF NOT EXISTS causation_id text;
ALTER TABLE accepted_events ADD COLUMN IF NOT EXISTS traceparent text;
ALTER TABLE accepted_events ADD COLUMN IF NOT EXISTS data_classification text NOT NULL DEFAULT 'internal';
ALTER TABLE accepted_events ADD COLUMN IF NOT EXISTS consent_context jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE api_keys ALTER COLUMN scopes SET DEFAULT ARRAY[
    'events:write', 'profiles:read', 'schemas:read', 'schemas:write',
    'api_keys:read', 'api_keys:write', 'privacy:write', 'operations:read', 'operations:write',
    'users:read', 'users:write', 'roles:read', 'roles:write'
];

CREATE TABLE IF NOT EXISTS users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    oidc_issuer text NOT NULL,
    oidc_subject text NOT NULL,
    email text,
    display_name text,
    disabled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (oidc_issuer, oidc_subject)
);

CREATE TABLE IF NOT EXISTS roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL,
    permissions text[] NOT NULL,
    system boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS role_bindings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid REFERENCES workspaces(id),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, user_id, role_id)
);

CREATE TABLE IF NOT EXISTS event_schemas (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    event_type text NOT NULL,
    version integer NOT NULL CHECK (version > 0),
    schema jsonb NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deprecated')),
    compatibility text NOT NULL DEFAULT 'backward' CHECK (compatibility IN ('none', 'backward')),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, event_type, version)
);

CREATE TABLE IF NOT EXISTS tenant_quotas (
    tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
    events_per_minute integer NOT NULL DEFAULT 120000 CHECK (events_per_minute > 0),
    max_batch_size integer NOT NULL DEFAULT 75 CHECK (max_batch_size BETWEEN 1 AND 1000),
    retention_days integer NOT NULL DEFAULT 396 CHECK (retention_days > 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS quota_windows (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    window_start timestamptz NOT NULL,
    event_count integer NOT NULL,
    PRIMARY KEY (tenant_id, window_start)
);

CREATE TABLE IF NOT EXISTS identity_aliases (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    app_id uuid NOT NULL REFERENCES applications(id),
    namespace text NOT NULL,
    value text NOT NULL,
    profile_id uuid NOT NULL REFERENCES profiles(id),
    source_event_id uuid REFERENCES accepted_events(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, app_id, namespace, value)
);

CREATE TABLE IF NOT EXISTS identity_merges (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    app_id uuid NOT NULL REFERENCES applications(id),
    source_profile_id uuid NOT NULL,
    target_profile_id uuid NOT NULL REFERENCES profiles(id),
    source_event_id uuid NOT NULL REFERENCES accepted_events(id),
    policy_version text NOT NULL DEFAULT 'v1',
    merged_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_event_id)
);

CREATE TABLE IF NOT EXISTS outbox_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    topic text NOT NULL,
    partition_key text NOT NULL,
    event_id uuid NOT NULL REFERENCES accepted_events(id),
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'published', 'dead')),
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    locked_until timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    UNIQUE (topic, event_id)
);
CREATE INDEX IF NOT EXISTS outbox_events_due_idx
    ON outbox_events (status, available_at, created_at);

CREATE TABLE IF NOT EXISTS privacy_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    app_id uuid NOT NULL REFERENCES applications(id),
    external_id text NOT NULL,
    request_type text NOT NULL CHECK (request_type IN ('export', 'delete')),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'complete', 'failed')),
    requested_by text NOT NULL,
    artifact_key text,
    error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);

CREATE TABLE IF NOT EXISTS operation_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    job_type text NOT NULL CHECK (job_type IN ('privacy.export', 'privacy.delete', 'profiles.replay', 'retention.enforce')),
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'done', 'dead')),
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    locked_until timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);
CREATE INDEX IF NOT EXISTS operation_jobs_due_idx
    ON operation_jobs (status, available_at, created_at);
CREATE INDEX IF NOT EXISTS accepted_events_tenant_received_idx
    ON accepted_events (tenant_id, received_at DESC);

INSERT INTO tenant_quotas(tenant_id)
SELECT id FROM tenants
ON CONFLICT (tenant_id) DO NOTHING;
