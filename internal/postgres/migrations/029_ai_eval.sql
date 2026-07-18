-- Migration 029: offline AI evaluation datasets, cases, and run verdicts.

CREATE TABLE IF NOT EXISTS eval_datasets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    task_type text NOT NULL,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, name)
);

CREATE TABLE IF NOT EXISTS eval_cases (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    dataset_id uuid NOT NULL REFERENCES eval_datasets(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    input jsonb NOT NULL,
    expectations jsonb NOT NULL
);

CREATE TABLE IF NOT EXISTS eval_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    prompt_version_id uuid NOT NULL REFERENCES prompt_versions(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    dataset_id uuid NOT NULL,
    passed integer NOT NULL DEFAULT 0,
    failed integer NOT NULL DEFAULT 0,
    verdict text NOT NULL CHECK (verdict IN ('passed','failed')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS eval_datasets_scope_idx
    ON eval_datasets (tenant_id, workspace_id);
CREATE INDEX IF NOT EXISTS eval_cases_dataset_idx
    ON eval_cases (tenant_id, dataset_id);
CREATE INDEX IF NOT EXISTS eval_runs_scope_idx
    ON eval_runs (tenant_id, dataset_id, created_at DESC);
