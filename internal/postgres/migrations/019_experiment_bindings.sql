-- Migration 019: Bind experiments to campaigns and stamp assignments on dispositions.
ALTER TABLE campaigns
    ADD COLUMN IF NOT EXISTS experiment_id uuid REFERENCES experiments(id);

ALTER TABLE delivery_attempts
    ADD COLUMN IF NOT EXISTS experiment_id uuid,
    ADD COLUMN IF NOT EXISTS variant text;

ALTER TABLE journey_message_intents
    ADD COLUMN IF NOT EXISTS experiment_id uuid,
    ADD COLUMN IF NOT EXISTS variant text;

-- Preserve every terminal and reconciliation decision written by the campaign worker.
ALTER TABLE delivery_attempts DROP CONSTRAINT IF EXISTS delivery_attempts_decision_check;
ALTER TABLE delivery_attempts ADD CONSTRAINT delivery_attempts_decision_check
    CHECK (decision IN ('sent','suppressed','no_consent','fatigued','render_failed',
                        'send_failed','failed','holdout','processing','provider_sent',
                        'retryable_failed','event_emitted'));

-- Journey intents previously had no decision constraint. Include all worker decisions.
ALTER TABLE journey_message_intents DROP CONSTRAINT IF EXISTS journey_message_intents_decision_check;
ALTER TABLE journey_message_intents ADD CONSTRAINT journey_message_intents_decision_check
    CHECK (decision IS NULL OR decision IN ('sent','suppressed','no_consent','fatigued',
        'render_failed','send_failed','failed','holdout','processing','provider_sent',
        'retryable_failed'));
