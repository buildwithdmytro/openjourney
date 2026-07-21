-- Migration 051: Analytics cost tracking — cost_micros on delivery and fact tables.

-- Add cost_micros to delivery_attempts (for campaigns)
ALTER TABLE delivery_attempts
    ADD COLUMN IF NOT EXISTS cost_micros bigint NOT NULL DEFAULT 0;

-- Add cost_micros to journey_message_intents (for journeys)
ALTER TABLE journey_message_intents
    ADD COLUMN IF NOT EXISTS cost_micros bigint NOT NULL DEFAULT 0;

-- Add cost_micros to engagement_facts (stamped at projection)
ALTER TABLE engagement_facts
    ADD COLUMN IF NOT EXISTS cost_micros bigint NOT NULL DEFAULT 0;

-- Add cost_micros to conversion_facts (stamped at projection)
ALTER TABLE conversion_facts
    ADD COLUMN IF NOT EXISTS cost_micros bigint NOT NULL DEFAULT 0;
