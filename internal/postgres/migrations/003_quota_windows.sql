CREATE TABLE IF NOT EXISTS quota_windows (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    window_start timestamptz NOT NULL,
    event_count integer NOT NULL,
    PRIMARY KEY (tenant_id, window_start)
);

CREATE INDEX IF NOT EXISTS accepted_events_tenant_received_idx
    ON accepted_events (tenant_id, received_at DESC);

ALTER TABLE projection_jobs ADD COLUMN IF NOT EXISTS partition_key text;
UPDATE projection_jobs j
SET partition_key=COALESCE(NULLIF(e.external_id,''),NULLIF(e.anonymous_id,''),e.id::text)
FROM accepted_events e
WHERE e.id=j.event_id AND j.partition_key IS NULL;
ALTER TABLE projection_jobs ALTER COLUMN partition_key SET NOT NULL;
CREATE INDEX IF NOT EXISTS projection_jobs_partition_idx
    ON projection_jobs (tenant_id,partition_key,created_at);
