-- Reporting periods and reports. A closed report is never overwritten;
-- corrections create a new revision that references the original.

CREATE TABLE reporting_periods (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    period     TEXT NOT NULL UNIQUE,   -- e.g. 2026-08
    status     TEXT NOT NULL,          -- OPEN / CLOSING / CLOSED / CLOSED_WITH_WARNINGS / REVISED
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE reports (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id            TEXT NOT NULL UNIQUE,   -- MINING-2026-08-ORIGINAL / -REVISION-001
    period               TEXT NOT NULL,
    revision             INTEGER NOT NULL,       -- 0 = original
    supersedes_report_id TEXT,                   -- original or previous revision
    reason               TEXT,
    evidence_bundle_hash TEXT NOT NULL,
    warnings_json        TEXT,                   -- acknowledged warnings, included in the report
    package_dir          TEXT NOT NULL,
    status               TEXT NOT NULL,          -- CLOSED / CLOSED_WITH_WARNINGS / REVISED
    software_version     TEXT,
    schema_version       INTEGER,
    created_at           TEXT NOT NULL,
    created_by           TEXT NOT NULL
);
CREATE INDEX idx_reports_period ON reports(period);
