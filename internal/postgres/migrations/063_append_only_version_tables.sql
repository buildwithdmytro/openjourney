-- Migration 063: Harden prompt_versions and scoring_model_versions with append-only triggers,
-- and add missing REVOKE UPDATE, DELETE on prompt_versions, scoring_model_versions,
-- ai_activity, identity_merges, experiment_versions (F5, F6).

BEGIN;

-- 1. prompt_versions append-only trigger and REVOKE
CREATE OR REPLACE FUNCTION prompt_versions_block_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'prompt_versions is append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS prompt_versions_no_update ON prompt_versions;
CREATE TRIGGER prompt_versions_no_update BEFORE UPDATE OR DELETE ON prompt_versions
    FOR EACH ROW EXECUTE FUNCTION prompt_versions_block_mutation();

REVOKE UPDATE, DELETE ON prompt_versions FROM PUBLIC;

-- 2. scoring_model_versions append-only trigger and REVOKE
CREATE OR REPLACE FUNCTION scoring_model_versions_block_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'scoring_model_versions is append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS scoring_model_versions_no_update ON scoring_model_versions;
CREATE TRIGGER scoring_model_versions_no_update BEFORE UPDATE OR DELETE ON scoring_model_versions
    FOR EACH ROW EXECUTE FUNCTION scoring_model_versions_block_mutation();

REVOKE UPDATE, DELETE ON scoring_model_versions FROM PUBLIC;

-- 3. Add REVOKE UPDATE, DELETE on tables that already had triggers but lacked REVOKE
REVOKE UPDATE, DELETE ON ai_activity FROM PUBLIC;
REVOKE UPDATE, DELETE ON identity_merges FROM PUBLIC;
REVOKE UPDATE, DELETE ON experiment_versions FROM PUBLIC;

COMMIT;
