-- Foundation schema: versioned miner inventory, versioned documents, and the
-- append-only audit log with a hash chain.
--
-- Conventions: timestamps are ISO 8601 UTC text; money is euro cents (INTEGER);
-- hashrate is H/s (INTEGER); hashes are lowercase hex text. No floating point
-- for money or Bitcoin amounts anywhere in this schema.

-- Stable miner identity. Master data lives in miner_versions so history is kept.
CREATE TABLE miners (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    internal_id TEXT NOT NULL UNIQUE,
    created_at  TEXT NOT NULL
);

-- Append-only versioned miner master data. A change creates a new version and
-- supersedes the previous one; historical versions are never modified.
CREATE TABLE miner_versions (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    miner_id              INTEGER NOT NULL REFERENCES miners(id),
    version               INTEGER NOT NULL,
    valid_from            TEXT NOT NULL,
    superseded_at         TEXT,               -- NULL = current version
    manufacturer          TEXT,
    model                 TEXT,
    serial_number         TEXT,
    purchase_date         TEXT,
    purchase_price_cents  INTEGER,            -- euro cents
    invoice_number        TEXT,
    invoice_path          TEXT,
    invoice_sha256        TEXT,
    nominal_hashrate_hs   INTEGER,            -- H/s
    nominal_power_w       INTEGER,            -- watts
    location              TEXT,
    commissioning_date    TEXT,
    decommissioning_date  TEXT,
    firmware_version      TEXT,
    note                  TEXT,
    created_at            TEXT NOT NULL,
    UNIQUE(miner_id, version)
);
CREATE INDEX idx_miner_versions_miner ON miner_versions(miner_id);

-- Append-only versioned documents. Re-uploading the same logical document
-- creates a new version; originals are preserved. Files themselves live in the
-- evidence directory; this table holds metadata and the content hash.
CREATE TABLE evidence_documents (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    doc_uid           TEXT NOT NULL,          -- stable logical document id
    version           INTEGER NOT NULL,
    doc_type          TEXT NOT NULL,
    original_filename TEXT NOT NULL,
    safe_filename     TEXT NOT NULL,
    doc_date          TEXT,
    description       TEXT,
    miner_id          INTEGER REFERENCES miners(id),
    mime_type         TEXT,
    size_bytes        INTEGER NOT NULL,
    sha256            TEXT NOT NULL,
    uploaded_at       TEXT NOT NULL,
    uploaded_by       TEXT NOT NULL,
    storage_location  TEXT NOT NULL,
    UNIQUE(doc_uid, version)
);
CREATE INDEX idx_documents_uid ON evidence_documents(doc_uid);
CREATE INDEX idx_documents_miner ON evidence_documents(miner_id);

-- Append-only audit log with a hash chain. Each entry hashes the previous
-- entry's hash together with its own fields, so deleting or editing any entry
-- breaks the chain and is detectable.
CREATE TABLE audit_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    event_uid       TEXT NOT NULL UNIQUE,
    ts_utc          TEXT NOT NULL,
    ts_local        TEXT NOT NULL,
    actor           TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    entity          TEXT,
    entity_id       TEXT,
    prev_value_hash TEXT,
    new_value_hash  TEXT,
    reason          TEXT,
    prev_entry_hash TEXT NOT NULL,
    entry_hash      TEXT NOT NULL
);
