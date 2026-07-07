ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash text;

CREATE UNIQUE INDEX IF NOT EXISTS users_local_email_idx
    ON users (tenant_id, lower(email))
    WHERE password_hash IS NOT NULL AND disabled_at IS NULL;

CREATE TABLE IF NOT EXISTS user_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    app_id uuid NOT NULL REFERENCES applications(id),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    last_used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS user_sessions_lookup_idx
    ON user_sessions (token_hash)
    WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS user_sessions_user_idx
    ON user_sessions (tenant_id, user_id, created_at DESC);
