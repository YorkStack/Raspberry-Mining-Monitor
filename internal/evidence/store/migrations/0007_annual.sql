-- Annual packages: a signed, self-contained roll-up of a tax year's monthly
-- evidence packages, for handing to the tax adviser. It documents facts and
-- integrity only; it does not compute or determine any tax result.

CREATE TABLE annual_packages (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    year               TEXT NOT NULL,        -- e.g. 2026
    package_dir        TEXT NOT NULL,
    annual_bundle_hash TEXT NOT NULL,        -- SHA-256 of the canonical annual manifest
    periods_included   INTEGER NOT NULL,     -- number of monthly reports rolled up
    all_verified       INTEGER NOT NULL,     -- 1 = every included package re-verified
    signing_key_id     TEXT,
    signature          TEXT,                 -- base64 detached signature over the manifest
    manifest_path      TEXT NOT NULL,
    software_version   TEXT,
    schema_version     INTEGER,
    created_at         TEXT NOT NULL
);
CREATE INDEX idx_annual_year ON annual_packages(year);
