CREATE TABLE tariff_proposals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    distributor TEXT NOT NULL,
    effective_from TEXT NOT NULL,
    effective_to TEXT NOT NULL,
    consumption_te_micros_per_kwh INTEGER NOT NULL CHECK(consumption_te_micros_per_kwh >= 0),
    consumption_tusd_micros_per_kwh INTEGER NOT NULL CHECK(consumption_tusd_micros_per_kwh >= 0),
    compensation_te_micros_per_kwh INTEGER NOT NULL CHECK(compensation_te_micros_per_kwh >= 0),
    compensation_tusd_micros_per_kwh INTEGER NOT NULL CHECK(compensation_tusd_micros_per_kwh >= 0),
    flag_micros_per_kwh INTEGER NOT NULL CHECK(flag_micros_per_kwh >= 0),
    availability_kwh INTEGER NOT NULL CHECK(availability_kwh IN (30, 50, 100)),
    cip_minor INTEGER NOT NULL CHECK(cip_minor >= 0),
    source_url TEXT NOT NULL,
    parser_version TEXT NOT NULL,
    retrieved_at TEXT NOT NULL,
    approved_at TEXT,
    approved_by TEXT REFERENCES users(id),
    CHECK(effective_to >= effective_from)
);

CREATE TRIGGER tariff_proposals_no_update_after_approval
BEFORE UPDATE ON tariff_proposals
WHEN OLD.approved_at IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'approved tariff proposal is immutable');
END;

CREATE TRIGGER tariff_proposals_no_delete_after_approval
BEFORE DELETE ON tariff_proposals
WHEN OLD.approved_at IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'approved tariff proposal is immutable');
END;

CREATE TABLE tariff_versions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    proposal_id INTEGER UNIQUE REFERENCES tariff_proposals(id),
    distributor TEXT NOT NULL,
    effective_from TEXT NOT NULL,
    effective_to TEXT NOT NULL,
    consumption_te_micros_per_kwh INTEGER NOT NULL CHECK(consumption_te_micros_per_kwh >= 0),
    consumption_tusd_micros_per_kwh INTEGER NOT NULL CHECK(consumption_tusd_micros_per_kwh >= 0),
    compensation_te_micros_per_kwh INTEGER NOT NULL CHECK(compensation_te_micros_per_kwh >= 0),
    compensation_tusd_micros_per_kwh INTEGER NOT NULL CHECK(compensation_tusd_micros_per_kwh >= 0),
    flag_micros_per_kwh INTEGER NOT NULL CHECK(flag_micros_per_kwh >= 0),
    availability_kwh INTEGER NOT NULL CHECK(availability_kwh IN (30, 50, 100)),
    cip_minor INTEGER NOT NULL CHECK(cip_minor >= 0),
    source_url TEXT NOT NULL,
    retrieved_at TEXT NOT NULL,
    approved_at TEXT NOT NULL,
    approved_by TEXT NOT NULL REFERENCES users(id),
    CHECK(effective_to >= effective_from)
);

CREATE TRIGGER tariff_versions_no_update
BEFORE UPDATE ON tariff_versions
BEGIN
    SELECT RAISE(ABORT, 'approved tariff version is immutable');
END;

CREATE TRIGGER tariff_versions_no_delete
BEFORE DELETE ON tariff_versions
BEGIN
    SELECT RAISE(ABORT, 'approved tariff version is immutable');
END;

CREATE TABLE billing_cycles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    reading_start TEXT NOT NULL,
    reading_end TEXT NOT NULL,
    active_consumption_kwh INTEGER NOT NULL CHECK(active_consumption_kwh >= 0),
    injected_kwh INTEGER NOT NULL CHECK(injected_kwh >= 0),
    credits_used_kwh INTEGER NOT NULL CHECK(credits_used_kwh >= 0),
    credit_balance_kwh INTEGER NOT NULL CHECK(credit_balance_kwh >= 0),
    total_paid_minor INTEGER NOT NULL CHECK(total_paid_minor >= 0),
    tariff_version_id INTEGER NOT NULL REFERENCES tariff_versions(id),
    created_at TEXT NOT NULL,
    CHECK(reading_end > reading_start),
    CHECK(credits_used_kwh <= active_consumption_kwh)
);

CREATE TABLE credit_lots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    origin_cycle_id INTEGER REFERENCES billing_cycles(id),
    available_kwh INTEGER NOT NULL CHECK(available_kwh >= 0),
    expires_at TEXT NOT NULL,
    is_partial INTEGER NOT NULL DEFAULT 0 CHECK(is_partial IN (0, 1)),
    created_at TEXT NOT NULL
);

CREATE TABLE bill_reconciliations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    billing_cycle_id INTEGER NOT NULL UNIQUE REFERENCES billing_cycles(id) ON DELETE CASCADE,
    projection_consumption_minor INTEGER NOT NULL CHECK(projection_consumption_minor >= 0),
    projection_compensation_minor INTEGER NOT NULL CHECK(projection_compensation_minor >= 0),
    projection_flag_minor INTEGER NOT NULL CHECK(projection_flag_minor >= 0),
    projection_taxes_minor INTEGER NOT NULL CHECK(projection_taxes_minor >= 0),
    projection_cip_minor INTEGER NOT NULL CHECK(projection_cip_minor >= 0),
    projection_total_minor INTEGER NOT NULL CHECK(projection_total_minor >= 0),
    without_solar_compensation_minor INTEGER NOT NULL CHECK(without_solar_compensation_minor >= 0),
    is_estimate INTEGER NOT NULL CHECK(is_estimate IN (0, 1)),
    calculated_at TEXT NOT NULL,
    reconciled_at TEXT
);

CREATE INDEX tariff_versions_effective_dates ON tariff_versions(effective_from, effective_to);
CREATE INDEX billing_cycles_reading_end ON billing_cycles(reading_end DESC);
CREATE INDEX credit_lots_expires_at ON credit_lots(expires_at, id);
