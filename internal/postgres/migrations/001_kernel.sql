CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS tenants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS workspaces (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS applications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS api_keys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    app_id uuid NOT NULL REFERENCES applications(id),
    name text NOT NULL,
    key_hash bytea NOT NULL UNIQUE,
    scopes text[] NOT NULL DEFAULT ARRAY[
        'events:write', 'profiles:read', 'schemas:read', 'schemas:write',
        'api_keys:read', 'api_keys:write', 'privacy:write', 'operations:read', 'operations:write',
        'users:read', 'users:write', 'roles:read', 'roles:write'
    ],
    expires_at timestamptz,
    revoked_at timestamptz,
    last_used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS accepted_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    app_id uuid NOT NULL REFERENCES applications(id),
    event_type text NOT NULL,
    schema_version integer NOT NULL CHECK (schema_version > 0),
    external_id text,
    anonymous_id text,
    idempotency_key text NOT NULL,
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    payload jsonb NOT NULL,
    UNIQUE (tenant_id, app_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS accepted_events_subject_idx
    ON accepted_events (tenant_id, app_id, external_id, occurred_at DESC);

CREATE TABLE IF NOT EXISTS projection_jobs (
    event_id uuid PRIMARY KEY REFERENCES accepted_events(id) ON DELETE CASCADE,
    sequence bigserial NOT NULL,
    tenant_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'done', 'dead')),
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    locked_until timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);
CREATE INDEX IF NOT EXISTS projection_jobs_due_idx
    ON projection_jobs (status, available_at, sequence);

CREATE TABLE IF NOT EXISTS profiles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    app_id uuid NOT NULL REFERENCES applications(id),
    external_id text,
    anonymous_id text,
    attributes jsonb NOT NULL DEFAULT '{}'::jsonb,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS profiles_external_id_idx
    ON profiles (tenant_id, app_id, external_id) WHERE external_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS profiles_anonymous_id_idx
    ON profiles (tenant_id, app_id, anonymous_id) WHERE anonymous_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS consent_ledger (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    app_id uuid NOT NULL REFERENCES applications(id),
    profile_id uuid NOT NULL REFERENCES profiles(id),
    source_event_id uuid NOT NULL REFERENCES accepted_events(id),
    channel text NOT NULL,
    topic text NOT NULL DEFAULT 'marketing',
    state text NOT NULL CHECK (state IN ('subscribed', 'unsubscribed')),
    occurred_at timestamptz NOT NULL,
    evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_event_id, channel, topic)
);
CREATE INDEX IF NOT EXISTS consent_ledger_profile_idx
    ON consent_ledger (tenant_id, profile_id, channel, topic, occurred_at DESC);

CREATE TABLE IF NOT EXISTS audit_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    app_id uuid,
    actor_type text NOT NULL,
    actor_id text NOT NULL,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id text,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_events_tenant_idx
    ON audit_events (tenant_id, occurred_at DESC);
