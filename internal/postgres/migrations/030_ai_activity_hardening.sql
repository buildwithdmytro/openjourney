-- Migration 030: DB-level append-only audit + decision CHECK.

-- Constrain the previously free-form decision/action strings (enumerate every value the code writes).
ALTER TABLE ai_activity ADD CONSTRAINT ai_activity_policy_decision_check
    CHECK (policy_decision IN ('allowed','denied_policy','denied_budget','denied_scope',
                               'denied_input','schema_reject','execution_error'));

-- Application-convention "immutable" → DB-enforced append-only.
CREATE OR REPLACE FUNCTION ai_activity_block_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'ai_activity is append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS ai_activity_no_update ON ai_activity;
CREATE TRIGGER ai_activity_no_update BEFORE UPDATE OR DELETE ON ai_activity
    FOR EACH ROW EXECUTE FUNCTION ai_activity_block_mutation();
