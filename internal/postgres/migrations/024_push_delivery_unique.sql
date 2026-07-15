-- Migration 024: Widen delivery attempts and journey message intents unique constraints to include endpoint.
-- This allows a profile with multiple active device tokens to have a separate send/disposition row per token.

-- Update delivery_attempts UNIQUE constraint
ALTER TABLE delivery_attempts DROP CONSTRAINT IF EXISTS delivery_attempts_campaign_id_profile_id_channel_key;
ALTER TABLE delivery_attempts ADD CONSTRAINT delivery_attempts_campaign_id_profile_id_channel_endpoint_key UNIQUE (campaign_id, profile_id, channel, endpoint);

-- Update journey_message_intents UNIQUE constraint
ALTER TABLE journey_message_intents DROP CONSTRAINT IF EXISTS journey_message_intents_run_id_node_id_key;
ALTER TABLE journey_message_intents ADD CONSTRAINT journey_message_intents_run_id_node_id_endpoint_key UNIQUE (run_id, node_id, endpoint);
