CREATE TABLE daily_analysis (
    day TEXT PRIMARY KEY,
    expected_wh REAL NOT NULL,
    actual_wh REAL NOT NULL,
    ratio REAL NOT NULL,
    confidence TEXT NOT NULL CHECK(confidence IN ('low','medium','high')),
    evidence_json TEXT NOT NULL,
    qualifying INTEGER NOT NULL CHECK(qualifying IN (0,1)),
    updated_at TEXT NOT NULL
);
