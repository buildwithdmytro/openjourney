-- Migration 049: Add display_rule field to inapp_messages for fetch-time eligibility.

ALTER TABLE inapp_messages ADD COLUMN display_rule jsonb;

-- Index for efficient filtering in fetch queries
CREATE INDEX IF NOT EXISTS inapp_messages_display_rule_idx
    ON inapp_messages (tenant_id, app_id, profile_id)
    WHERE display_rule IS NULL OR display_rule != '{}'::jsonb;
