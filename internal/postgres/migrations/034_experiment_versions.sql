-- Migration 034: immutable experiment versions created by approved optimization.
CREATE TABLE IF NOT EXISTS experiment_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    experiment_id uuid NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    proposal_id uuid NOT NULL REFERENCES optimization_proposals(id),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    version integer NOT NULL CHECK (version > 0),
    seed text NOT NULL,
    holdout_pct integer NOT NULL CHECK (holdout_pct BETWEEN 0 AND 100),
    variants jsonb NOT NULL,
    approved_by uuid REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (experiment_id, version)
    ,UNIQUE (proposal_id)
);

CREATE INDEX IF NOT EXISTS experiment_versions_scope_idx
    ON experiment_versions (tenant_id, workspace_id, experiment_id, version);

CREATE OR REPLACE FUNCTION experiment_versions_block_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'experiment_versions are immutable';
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS experiment_versions_no_update ON experiment_versions;
CREATE TRIGGER experiment_versions_no_update BEFORE UPDATE OR DELETE ON experiment_versions
    FOR EACH ROW EXECUTE FUNCTION experiment_versions_block_mutation();
