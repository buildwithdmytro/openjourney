-- Migration 044: Harden the extension_activity append-only audit log (Milestone 15.0.3).
-- The BEFORE UPDATE OR DELETE trigger from migration 042 is the primary barrier
-- against mutation; this adds REVOKE as defense-in-depth so the application role
-- cannot UPDATE or DELETE audit rows even if the trigger is later disabled.
-- Mirrors the append-only hardening applied to connector_pipeline_versions in 043.

REVOKE UPDATE, DELETE ON extension_activity FROM PUBLIC;
