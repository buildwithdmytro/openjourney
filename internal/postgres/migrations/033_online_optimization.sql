-- Migration 033: Store advisory online-optimization proposals.

-- A proposal is advisory until a human approves it into a new immutable version.
CREATE TABLE IF NOT EXISTS optimization_proposals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    experiment_id uuid NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('reallocate','winner')),
    report_snapshot jsonb NOT NULL,           -- the ExperimentReport that justified it
    proposed_weights jsonb,                    -- reallocate: {variant: weight}
    winner_variant text,                       -- winner
    rationale text NOT NULL,
    status text NOT NULL DEFAULT 'proposed'
        CHECK (status IN ('proposed','approved','rejected','superseded')),
    approved_by uuid,                          -- human approver; NULL until approved
    approved_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS optimization_proposals_idx
    ON optimization_proposals (tenant_id, workspace_id, experiment_id, status);
