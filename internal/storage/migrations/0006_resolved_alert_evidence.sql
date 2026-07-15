-- Rows resolved before resolution evidence was persisted still contain the
-- opening-condition evidence. Clear it so recovery APIs never mislabel the
-- original failure values as proof of recovery.
UPDATE alerts SET evidence_json='{}' WHERE state='resolved';
