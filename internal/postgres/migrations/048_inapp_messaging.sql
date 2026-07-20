-- Migration 048: In-app messaging foundation.
-- Adds in_app channel, inapp/webpush providers, and inapp_messages table.

-- Widen sending_identities.channel to include in_app.
ALTER TABLE sending_identities DROP CONSTRAINT IF EXISTS sending_identities_channel_check;
ALTER TABLE sending_identities ADD CONSTRAINT sending_identities_channel_check
    CHECK (channel IN ('email','webhook','sms','push','in_app'));

-- Widen sending_identities.provider to include inapp and webpush.
ALTER TABLE sending_identities DROP CONSTRAINT IF EXISTS sending_identities_provider_check;
ALTER TABLE sending_identities ADD CONSTRAINT sending_identities_provider_check
    CHECK (provider IN ('ses','webhook','twilio','fcm','apns','http','fake','inapp','webpush'));

-- Widen templates.channel to include in_app.
ALTER TABLE templates DROP CONSTRAINT IF EXISTS templates_channel_check;
ALTER TABLE templates ADD CONSTRAINT templates_channel_check
    CHECK (channel IN ('email','webhook','sms','push','in_app'));

-- Add in_app arm to body-presence CHECK.
ALTER TABLE templates DROP CONSTRAINT IF EXISTS templates_body_presence_check;
ALTER TABLE templates ADD CONSTRAINT templates_body_presence_check CHECK (
       (channel='email'   AND html_template IS NOT NULL)
    OR (channel='webhook' AND body_template IS NOT NULL)
    OR (channel='sms'     AND COALESCE(text_template, body_template) IS NOT NULL)
    OR (channel='push'    AND title_template IS NOT NULL AND COALESCE(body_template, text_template) IS NOT NULL)
    OR (channel='in_app'  AND (title_template IS NOT NULL OR body_template IS NOT NULL OR html_template IS NOT NULL))
);

-- Widen device_tokens.provider to include webpush.
ALTER TABLE device_tokens DROP CONSTRAINT IF EXISTS device_tokens_provider_check;
ALTER TABLE device_tokens ADD CONSTRAINT device_tokens_provider_check
    CHECK (provider IN ('fcm','apns','http','fake','webpush'));

-- Create inapp_messages table.
CREATE TABLE IF NOT EXISTS inapp_messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    app_id uuid NOT NULL,
    profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    template_id uuid,
    campaign_id uuid,
    journey_run_id uuid,
    delivery_attempt_id uuid,
    message_type text NOT NULL DEFAULT 'modal' CHECK (message_type IN ('modal','banner','fullscreen','card')),
    content jsonb NOT NULL,
    rank int NOT NULL DEFAULT 0,
    categories text[] NOT NULL DEFAULT '{}',
    start_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz,
    idempotency_key text,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','delivered','displayed','clicked','dismissed','expired')),
    delivered_at timestamptz,
    displayed_at timestamptz,
    clicked_at timestamptz,
    dismissed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, profile_id, idempotency_key)
);

-- Inbox fetch index: undismissed, in-window, by rank.
CREATE INDEX IF NOT EXISTS inapp_messages_inbox_idx
    ON inapp_messages (tenant_id, app_id, profile_id, rank DESC)
    WHERE dismissed_at IS NULL;

-- Widen api_keys.scopes DEFAULT array to include messages:read and messages:write.
ALTER TABLE api_keys ALTER COLUMN scopes SET DEFAULT ARRAY[
    'events:write','profiles:read','schemas:read','schemas:write',
    'api_keys:read','api_keys:write','privacy:write','operations:read','operations:write',
    'users:read','users:write','roles:read','roles:write',
    'segments:read','segments:write','templates:read','templates:write',
    'campaigns:read','campaigns:write','suppressions:read','suppressions:write',
    'journeys:read','journeys:write','journeys:publish',
    'experiments:read','experiments:write','reports:read',
    'device_tokens:read','device_tokens:write',
    'ai:read','ai:configure','ai:invoke','prompts:read','prompts:write',
    'scoring:read','scoring:write','scoring:compute',
    'forms:read','forms:write','forms:publish','pages:read','pages:write','pages:publish',
    'assets:read','assets:write','links:read','links:write',
    'companies:read','companies:write','stages:read','stages:write','imports:read','imports:write',
    'extensions:read','extensions:write','extensions:install',
    'connectors:read','connectors:write','connectors:run',
    'messages:read','messages:write'
];
