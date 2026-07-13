-- Migration 021: Bounded, workspace-scoped last-touch attribution lookups.
CREATE INDEX IF NOT EXISTS delivery_attempts_attribution_idx
    ON delivery_attempts (tenant_id, profile_id, attempted_at DESC)
    WHERE decision = 'sent';

CREATE INDEX IF NOT EXISTS journey_message_intents_attribution_idx
    ON journey_message_intents (tenant_id, workspace_id, profile_id, updated_at DESC)
    WHERE decision = 'sent';
