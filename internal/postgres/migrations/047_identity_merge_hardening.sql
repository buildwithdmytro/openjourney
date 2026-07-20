-- Migration 047: Identity merge audit + reversibility hardening
-- Fixes findings 1, 3, 6 from M10 security review

BEGIN;

-- Drop the old single-event unique constraint that loses reversibility on multi-way merges
ALTER TABLE identity_merges DROP CONSTRAINT identity_merges_source_event_id_key;

-- Add a new unique constraint keyed on (source_event_id, source_profile_id)
-- so each profile in a multi-way merge gets its own reversible row
ALTER TABLE identity_merges ADD CONSTRAINT identity_merges_event_source_key
  UNIQUE (source_event_id, source_profile_id);

-- Add undone_at column if it doesn't exist (in case earlier migrations added it)
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name='identity_merges' AND column_name='undone_at'
  ) THEN
    ALTER TABLE identity_merges ADD COLUMN undone_at timestamptz;
  END IF;
END $$;

-- Create trigger to enforce append-only semantics with erasure GUC gate
-- - Block UPDATE unless only undone_at changed
-- - Block DELETE unless openjourney.erasure='on' (set only by RTBF path)
CREATE OR REPLACE FUNCTION identity_merges_guard() RETURNS TRIGGER AS $$
BEGIN
  IF TG_OP = 'UPDATE' THEN
    -- Allow UPDATE only if changing undone_at
    IF NEW.source_event_id != OLD.source_event_id OR
       NEW.source_profile_id != OLD.source_profile_id OR
       NEW.target_profile_id != OLD.target_profile_id OR
       NEW.tenant_id != OLD.tenant_id OR
       NEW.app_id != OLD.app_id OR
       NEW.policy_version != OLD.policy_version OR
       NEW.winner_policy != OLD.winner_policy OR
       NEW.reversible != OLD.reversible OR
       NEW.reversal_ref != OLD.reversal_ref OR
       NEW.actor_user_id IS DISTINCT FROM OLD.actor_user_id OR
       NEW.actor_type != OLD.actor_type THEN
      RAISE EXCEPTION 'identity_merges is append-only: cannot modify fields other than undone_at';
    END IF;
    RETURN NEW;
  ELSIF TG_OP = 'DELETE' THEN
    -- Allow DELETE only when erasure GUC is set
    IF COALESCE(current_setting('openjourney.erasure', true), 'off') != 'on' THEN
      RAISE EXCEPTION 'identity_merges can only be deleted during RTBF erasure';
    END IF;
    RETURN OLD;
  END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- Drop trigger if it exists from a prior run
DROP TRIGGER IF EXISTS identity_merges_guard_trigger ON identity_merges;

-- Create the trigger
CREATE TRIGGER identity_merges_guard_trigger
  BEFORE UPDATE OR DELETE ON identity_merges
  FOR EACH ROW
  EXECUTE FUNCTION identity_merges_guard();

COMMIT;
