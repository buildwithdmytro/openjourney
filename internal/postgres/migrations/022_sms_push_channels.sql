-- Migration 022: Widen channel/provider enums to include sms and push channels.
-- Adds title_template and push_data columns to templates; widens body-presence CHECK.

-- Widen sending_identities.channel to include sms and push.
ALTER TABLE sending_identities DROP CONSTRAINT IF EXISTS sending_identities_channel_check;
ALTER TABLE sending_identities ADD CONSTRAINT sending_identities_channel_check
    CHECK (channel IN ('email','webhook','sms','push'));

-- Widen sending_identities.provider to include twilio, fcm, apns, http, fake.
ALTER TABLE sending_identities DROP CONSTRAINT IF EXISTS sending_identities_provider_check;
ALTER TABLE sending_identities ADD CONSTRAINT sending_identities_provider_check
    CHECK (provider IN ('ses','webhook','twilio','fcm','apns','http','fake'));

-- from_address is already NULLable and doubles as the SMS sender-id;
-- push leaves it NULL. The existing UNIQUE(tenant_id, channel, from_address) still applies
-- (Postgres treats NULL from_address rows as distinct, so multiple push identities are allowed).

-- Widen templates.channel to include sms and push.
ALTER TABLE templates DROP CONSTRAINT IF EXISTS templates_channel_check;
ALTER TABLE templates ADD CONSTRAINT templates_channel_check
    CHECK (channel IN ('email','webhook','sms','push'));

-- Add push-specific columns.
ALTER TABLE templates ADD COLUMN IF NOT EXISTS title_template text;   -- push notification title
ALTER TABLE templates ADD COLUMN IF NOT EXISTS push_data jsonb;       -- {actions,image,url,collapse_key,badge,sound}

-- The body-presence CHECK is an unnamed inline constraint whose generated name is `templates_check`.
-- Confirmed via \d templates. Drop and recreate with sms/push arms.
ALTER TABLE templates DROP CONSTRAINT IF EXISTS templates_check;
ALTER TABLE templates ADD CONSTRAINT templates_body_presence_check CHECK (
       (channel='email'   AND html_template IS NOT NULL)
    OR (channel='webhook' AND body_template IS NOT NULL)
    OR (channel='sms'     AND COALESCE(text_template, body_template) IS NOT NULL)
    OR (channel='push'    AND title_template IS NOT NULL AND COALESCE(body_template, text_template) IS NOT NULL)
);
