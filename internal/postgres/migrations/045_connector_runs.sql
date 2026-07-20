-- Migration 045: append-only connector execution audit.
CREATE TABLE IF NOT EXISTS connector_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    app_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    pipeline_id uuid NOT NULL REFERENCES connector_pipelines(id) ON DELETE CASCADE,
    pipeline_version_id uuid NOT NULL REFERENCES connector_pipeline_versions(id) ON DELETE RESTRICT,
    job_type text NOT NULL CHECK (job_type IN ('warehouse.sync','reverse_etl.run','export.replay')),
    status text NOT NULL CHECK (status IN ('running','succeeded','failed','dead')),
    cursor text,
    rows_in bigint NOT NULL DEFAULT 0,
    rows_out bigint NOT NULL DEFAULT 0,
    rows_rejected bigint NOT NULL DEFAULT 0,
    reject_blob_key text,
    error text,
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);
CREATE INDEX IF NOT EXISTS connector_runs_pipeline_idx ON connector_runs (pipeline_id, started_at DESC);
CREATE OR REPLACE FUNCTION connector_runs_block_mutation() RETURNS trigger AS $$
BEGIN RAISE EXCEPTION 'connector_runs is append-only'; END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS connector_runs_no_update ON connector_runs;
CREATE TRIGGER connector_runs_no_update BEFORE UPDATE OR DELETE ON connector_runs
    FOR EACH ROW EXECUTE FUNCTION connector_runs_block_mutation();
REVOKE UPDATE, DELETE ON connector_runs FROM PUBLIC;
