ALTER TABLE projection_jobs ADD COLUMN IF NOT EXISTS sequence bigserial;
ALTER TABLE projection_jobs ALTER COLUMN sequence SET NOT NULL;
DROP INDEX IF EXISTS projection_jobs_due_idx;
CREATE INDEX projection_jobs_due_idx
    ON projection_jobs (status,available_at,sequence);
CREATE INDEX IF NOT EXISTS projection_jobs_partition_sequence_idx
    ON projection_jobs (tenant_id,partition_key,sequence);

