-- Migration 066: hot-path indexes and defense-in-depth tenant guards.

CREATE INDEX IF NOT EXISTS inapp_messages_expiry_idx
    ON inapp_messages (tenant_id, expires_at)
    WHERE expires_at IS NOT NULL AND status NOT IN ('expired', 'dismissed');

CREATE INDEX IF NOT EXISTS inapp_messages_admin_list_idx
    ON inapp_messages (tenant_id, workspace_id, app_id, created_at DESC);

CREATE INDEX IF NOT EXISTS feature_flags_workspace_list_idx
    ON feature_flags (tenant_id, workspace_id, environment, key);
