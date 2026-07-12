-- Migration 018: Experiment definitions, variants, and authoritative assignments.
CREATE TABLE IF NOT EXISTS experiments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    name text NOT NULL,
    description text,
    subject_type text NOT NULL CHECK (subject_type IN ('campaign','journey')),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','running','completed','archived')),
    method text NOT NULL DEFAULT 'frequentist' CHECK (method IN ('frequentist')),
    seed text NOT NULL,
    holdout_pct integer NOT NULL DEFAULT 0 CHECK (holdout_pct BETWEEN 0 AND 100),
    primary_goal jsonb,
    guardrail_goals jsonb NOT NULL DEFAULT '[]'::jsonb,
    winner_variant text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS experiments_tenant_idx ON experiments (tenant_id, workspace_id);

CREATE TABLE IF NOT EXISTS experiment_variants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    experiment_id uuid NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    label text NOT NULL,
    weight integer NOT NULL CHECK (weight >= 0),
    is_control boolean NOT NULL DEFAULT false,
    template_id uuid REFERENCES templates(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (experiment_id, label)
);

CREATE TABLE IF NOT EXISTS experiment_assignments (
    experiment_id uuid NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    variant text NOT NULL,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (experiment_id, profile_id)
);

CREATE INDEX IF NOT EXISTS experiment_assignments_variant_idx
    ON experiment_assignments (experiment_id, variant);

ALTER TABLE api_keys ALTER COLUMN scopes SET DEFAULT ARRAY[
    'events:write','profiles:read','schemas:read','schemas:write',
    'api_keys:read','api_keys:write','privacy:write','operations:read','operations:write',
    'users:read','users:write','roles:read','roles:write',
    'segments:read','segments:write','templates:read','templates:write',
    'campaigns:read','campaigns:write','suppressions:read','suppressions:write',
    'journeys:read','journeys:write','journeys:publish',
    'experiments:read','experiments:write','reports:read'
];
