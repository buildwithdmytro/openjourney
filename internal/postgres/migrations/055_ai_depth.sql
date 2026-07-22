-- Migration 055: AI Depth — Governed Agentic Assistant runs table.

CREATE TABLE IF NOT EXISTS ai_agent_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    question text NOT NULL,
    steps jsonb NOT NULL DEFAULT '[]'::jsonb,
    answer text,
    final_activity_id uuid,
    status text NOT NULL CHECK (status IN ('completed', 'max_steps_exceeded', 'budget_exceeded', 'timeout', 'error')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ai_agent_runs_workspace_idx
    ON ai_agent_runs (tenant_id, workspace_id, created_at DESC);

CREATE OR REPLACE FUNCTION ai_agent_runs_block_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'ai_agent_runs is append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS ai_agent_runs_no_update ON ai_agent_runs;
CREATE TRIGGER ai_agent_runs_no_update BEFORE UPDATE OR DELETE ON ai_agent_runs
    FOR EACH ROW EXECUTE FUNCTION ai_agent_runs_block_mutation();
