ALTER TABLE daily_summary ADD COLUMN ratio REAL;
ALTER TABLE daily_summary ADD COLUMN confidence_label TEXT CHECK(confidence_label IN ('low','medium','high'));
ALTER TABLE daily_summary ADD COLUMN analysis_evidence_json TEXT;
ALTER TABLE daily_summary ADD COLUMN analysis_qualifying INTEGER CHECK(analysis_qualifying IN (0,1));
ALTER TABLE daily_summary ADD COLUMN analyzed_at TEXT;
