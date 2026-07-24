-- Migration 064: Add BEFORE TRUNCATE triggers to audit_events and append-only tables (F7).

BEGIN;

CREATE OR REPLACE FUNCTION append_only_block_truncate() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION '% is append-only and cannot be truncated', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;

-- 1. audit_events
DROP TRIGGER IF EXISTS audit_events_no_truncate ON audit_events;
CREATE TRIGGER audit_events_no_truncate BEFORE TRUNCATE ON audit_events
    FOR EACH STATEMENT EXECUTE FUNCTION append_only_block_truncate();

-- 2. connector_runs
DROP TRIGGER IF EXISTS connector_runs_no_truncate ON connector_runs;
CREATE TRIGGER connector_runs_no_truncate BEFORE TRUNCATE ON connector_runs
    FOR EACH STATEMENT EXECUTE FUNCTION append_only_block_truncate();

-- 3. ai_activity
DROP TRIGGER IF EXISTS ai_activity_no_truncate ON ai_activity;
CREATE TRIGGER ai_activity_no_truncate BEFORE TRUNCATE ON ai_activity
    FOR EACH STATEMENT EXECUTE FUNCTION append_only_block_truncate();

-- 4. identity_merges
DROP TRIGGER IF EXISTS identity_merges_no_truncate ON identity_merges;
CREATE TRIGGER identity_merges_no_truncate BEFORE TRUNCATE ON identity_merges
    FOR EACH STATEMENT EXECUTE FUNCTION append_only_block_truncate();

-- 5. experiment_versions
DROP TRIGGER IF EXISTS experiment_versions_no_truncate ON experiment_versions;
CREATE TRIGGER experiment_versions_no_truncate BEFORE TRUNCATE ON experiment_versions
    FOR EACH STATEMENT EXECUTE FUNCTION append_only_block_truncate();

-- 6. prompt_versions
DROP TRIGGER IF EXISTS prompt_versions_no_truncate ON prompt_versions;
CREATE TRIGGER prompt_versions_no_truncate BEFORE TRUNCATE ON prompt_versions
    FOR EACH STATEMENT EXECUTE FUNCTION append_only_block_truncate();

-- 7. scoring_model_versions
DROP TRIGGER IF EXISTS scoring_model_versions_no_truncate ON scoring_model_versions;
CREATE TRIGGER scoring_model_versions_no_truncate BEFORE TRUNCATE ON scoring_model_versions
    FOR EACH STATEMENT EXECUTE FUNCTION append_only_block_truncate();

-- 8. ai_agent_runs
DROP TRIGGER IF EXISTS ai_agent_runs_no_truncate ON ai_agent_runs;
CREATE TRIGGER ai_agent_runs_no_truncate BEFORE TRUNCATE ON ai_agent_runs
    FOR EACH STATEMENT EXECUTE FUNCTION append_only_block_truncate();

-- 9. extension_activity
DROP TRIGGER IF EXISTS extension_activity_no_truncate ON extension_activity;
CREATE TRIGGER extension_activity_no_truncate BEFORE TRUNCATE ON extension_activity
    FOR EACH STATEMENT EXECUTE FUNCTION append_only_block_truncate();

-- 10. metric_definitions
DROP TRIGGER IF EXISTS metric_definitions_no_truncate ON metric_definitions;
CREATE TRIGGER metric_definitions_no_truncate BEFORE TRUNCATE ON metric_definitions
    FOR EACH STATEMENT EXECUTE FUNCTION append_only_block_truncate();

-- 11. connector_pipeline_versions
DROP TRIGGER IF EXISTS connector_pipeline_versions_no_truncate ON connector_pipeline_versions;
CREATE TRIGGER connector_pipeline_versions_no_truncate BEFORE TRUNCATE ON connector_pipeline_versions
    FOR EACH STATEMENT EXECUTE FUNCTION append_only_block_truncate();

-- 12. feature_flag_versions
DROP TRIGGER IF EXISTS feature_flag_versions_no_truncate ON feature_flag_versions;
CREATE TRIGGER feature_flag_versions_no_truncate BEFORE TRUNCATE ON feature_flag_versions
    FOR EACH STATEMENT EXECUTE FUNCTION append_only_block_truncate();

COMMIT;
