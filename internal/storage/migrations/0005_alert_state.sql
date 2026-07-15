CREATE TABLE alert_rule_state (
    rule TEXT PRIMARY KEY,
    state_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
