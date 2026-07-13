-- Migration 021: Cover the scoped disposition reads used by campaign/journey reports.
CREATE INDEX IF NOT EXISTS delivery_attempts_report_idx
    ON delivery_attempts (tenant_id, campaign_id, decision, profile_id);

CREATE INDEX IF NOT EXISTS journey_message_intents_report_idx
    ON journey_message_intents (tenant_id, workspace_id, journey_id, decision, profile_id);
