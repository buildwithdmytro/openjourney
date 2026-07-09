-- Migration 016: Journey participant state, timer/work queue, and transitions.
CREATE TABLE IF NOT EXISTS journey_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL,
    journey_id uuid NOT NULL REFERENCES journeys(id) ON DELETE CASCADE,
    journey_version_id uuid NOT NULL REFERENCES journey_versions(id),  -- PINNED version
    profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    subject_external_id text,                         -- for wait-for-event matching
    entry_key text NOT NULL,                          -- event id, or "sched:<ver>:<runid>"
    reentry_sequence integer NOT NULL DEFAULT 0,
    status text NOT NULL DEFAULT 'active'
        CHECK (status IN ('active','waiting','completed','exited','failed','canceled')),
    current_node_id text NOT NULL,
    state jsonb NOT NULL DEFAULT '{}'::jsonb,         -- accumulated context + split assignments
    wait_event_type text,                             -- set while status='waiting'
    wait_until timestamptz,                           -- timeout for the wait
    goal_reached boolean NOT NULL DEFAULT false,
    entered_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    UNIQUE (journey_version_id, profile_id, entry_key, reentry_sequence)  -- effectively-once
);
CREATE INDEX IF NOT EXISTS journey_runs_wait_idx
    ON journey_runs (tenant_id, wait_event_type, subject_external_id) WHERE status='waiting';
CREATE INDEX IF NOT EXISTS journey_runs_profile_idx ON journey_runs (tenant_id, profile_id);

-- The DURABLE TIMER / WORK QUEUE. Mirrors delivery_jobs (012_campaigns.sql:24).
CREATE TABLE IF NOT EXISTS journey_steps (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id uuid NOT NULL REFERENCES journey_runs(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    node_id text NOT NULL,                            -- node to execute when this fires
    kind text NOT NULL DEFAULT 'advance' CHECK (kind IN ('advance','timeout')),
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','processing','completed','failed','dead')),
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),  -- THE durable timer
    locked_until timestamptz,
    error_message text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS journey_steps_due_idx ON journey_steps (status, available_at);
-- At most one live step per run (prevents double-scheduling a participant).
CREATE UNIQUE INDEX IF NOT EXISTS journey_steps_one_live_per_run
    ON journey_steps (run_id) WHERE status IN ('pending','processing');

-- Causal history — explainability + replay comparison.
CREATE TABLE IF NOT EXISTS journey_transitions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id uuid NOT NULL REFERENCES journey_runs(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    from_node text,
    to_node text,
    node_type text NOT NULL,
    outcome text NOT NULL,                            -- e.g. 'advanced','branch:true','waited','sent','exited'
    detail jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS journey_transitions_run_idx ON journey_transitions (run_id, occurred_at);
