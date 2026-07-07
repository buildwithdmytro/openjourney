ALTER TABLE projection_jobs ADD COLUMN IF NOT EXISTS partition_key text;
UPDATE projection_jobs j
SET partition_key=COALESCE(NULLIF(e.external_id,''),NULLIF(e.anonymous_id,''),e.id::text)
FROM accepted_events e
WHERE e.id=j.event_id AND j.partition_key IS NULL;
ALTER TABLE projection_jobs ALTER COLUMN partition_key SET NOT NULL;
CREATE INDEX IF NOT EXISTS projection_jobs_partition_idx
    ON projection_jobs (tenant_id,partition_key,created_at);

