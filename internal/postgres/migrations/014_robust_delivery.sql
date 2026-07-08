-- Migration 014: Expand delivery attempts states for robust state machine
ALTER TABLE delivery_attempts DROP CONSTRAINT IF EXISTS delivery_attempts_decision_check;
ALTER TABLE delivery_attempts ADD CONSTRAINT delivery_attempts_decision_check CHECK (decision IN ('sent', 'suppressed', 'no_consent', 'fatigued', 'render_failed', 'send_failed', 'failed', 'processing', 'provider_sent', 'event_emitted'));
