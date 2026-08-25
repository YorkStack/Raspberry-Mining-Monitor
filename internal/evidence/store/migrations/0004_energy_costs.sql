-- Energy measurement and cost records. Energy is Wh, money is euro cents, both
-- INTEGER. The software documents facts; it never determines deductibility.

-- Energy measurements. Physically measured and estimated consumption are kept
-- distinct (the `measured` flag). Estimates must state their method and author.
-- Measurement gaps are recorded via completeness, never silently interpolated.
CREATE TABLE energy_measurements (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    miner_internal_id   TEXT,
    measurement_start   TEXT NOT NULL,
    measurement_end     TEXT NOT NULL,
    start_reading_wh    INTEGER,
    end_reading_wh      INTEGER,
    energy_wh           INTEGER NOT NULL,
    avg_power_w         REAL,
    min_power_w         REAL,
    max_power_w         REAL,
    completeness_pct    REAL,
    measured            INTEGER NOT NULL,   -- 1 = physically measured, 0 = estimated
    source              TEXT,               -- adapter / meter source
    meter_device_id     TEXT,
    meter_serial        TEXT,
    energy_source       TEXT,               -- GRID / SOLAR / MIXED / UNKNOWN
    solar_production_wh INTEGER,
    grid_import_wh      INTEGER,
    grid_export_wh      INTEGER,
    estimation_method   TEXT,               -- required when measured = 0
    estimated_by        TEXT,
    original_gap        TEXT,
    note                TEXT,
    created_at          TEXT NOT NULL,
    created_by          TEXT NOT NULL
);
CREATE INDEX idx_energy_miner ON energy_measurements(miner_internal_id);

-- Cost records. Preliminary summaries may be computed, but the software never
-- determines immediate deduction, depreciation, business/private classification
-- or VAT deduction — that is for the tax adviser.
CREATE TABLE cost_records (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    cost_date           TEXT NOT NULL,
    description         TEXT NOT NULL,
    category            TEXT NOT NULL,
    gross_cents         INTEGER NOT NULL,
    net_cents           INTEGER,
    vat_cents           INTEGER,
    currency            TEXT NOT NULL,
    payment_method      TEXT,
    miner_internal_id   TEXT,
    reporting_period    TEXT,               -- e.g. 2026-08
    invoice_document_id INTEGER REFERENCES evidence_documents(id),
    invoice_sha256      TEXT,
    allocation_pct      REAL,               -- percent attributed to mining
    note                TEXT,
    created_at          TEXT NOT NULL,
    created_by          TEXT NOT NULL
);
CREATE INDEX idx_cost_period ON cost_records(reporting_period);

-- Tax-adviser cost adjustments as separate versioned records; the original
-- cost_record is preserved.
CREATE TABLE cost_adjustments (
    id                      INTEGER PRIMARY KEY AUTOINCREMENT,
    cost_id                 INTEGER NOT NULL REFERENCES cost_records(id),
    adjusted_gross_cents    INTEGER,
    adjusted_net_cents      INTEGER,
    adjusted_vat_cents      INTEGER,
    adjusted_allocation_pct REAL,
    reason                  TEXT NOT NULL,
    adjusted_by             TEXT NOT NULL,
    adjusted_at             TEXT NOT NULL,
    created_at              TEXT NOT NULL
);
CREATE INDEX idx_cost_adj ON cost_adjustments(cost_id);
