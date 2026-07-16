ALTER TABLE billing_cycles ADD COLUMN flag_charge_minor INTEGER NOT NULL DEFAULT 0 CHECK(flag_charge_minor >= 0);
