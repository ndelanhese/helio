ALTER TABLE telemetry_minute
ADD COLUMN observed_at_utc TEXT NOT NULL DEFAULT '0001-01-01T00:00:00.000000000Z';

-- Version 1 stored only the minute bucket. Treat that bucket as the best known
-- source time and canonicalize both old RFC3339 forms to fixed-width UTC text.
UPDATE telemetry_minute
SET observed_at_utc = COALESCE(
    strftime('%Y-%m-%dT%H:%M:%f', observed_at) || '000000Z',
    '0001-01-01T00:00:00.000000000Z'
);

-- Normalize pre-v2 bucket keys as well; v1 used a valid but variable-width
-- RFC3339 representation which is not safe for SQLite TEXT range predicates.
UPDATE telemetry_minute
SET observed_at = COALESCE(
    strftime('%Y-%m-%dT%H:%M:%f', observed_at) || '000000Z',
    observed_at
);
