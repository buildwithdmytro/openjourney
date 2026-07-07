-- Projected from message.bounced / message.complained events and from manual admin action.
CREATE TABLE IF NOT EXISTS suppressions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    channel text NOT NULL,
    endpoint text NOT NULL,          -- email address (lowercased) or webhook target
    reason text NOT NULL CHECK (reason IN ('bounce','complaint','unsubscribe','admin')),
    source_event_id uuid REFERENCES accepted_events(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, channel, endpoint)
);
CREATE INDEX IF NOT EXISTS suppressions_lookup_idx
    ON suppressions (tenant_id, channel, endpoint);
