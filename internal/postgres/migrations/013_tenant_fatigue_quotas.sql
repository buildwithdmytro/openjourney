ALTER TABLE tenant_quotas
ADD COLUMN IF NOT EXISTS max_sends_24h integer NOT NULL DEFAULT 5 CHECK (max_sends_24h >= 0),
ADD COLUMN IF NOT EXISTS max_sends_7d integer NOT NULL DEFAULT 20 CHECK (max_sends_7d >= 0);
