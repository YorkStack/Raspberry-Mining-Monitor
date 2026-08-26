-- Digital signing keys, final manifests, and backup runs.

-- Dedicated evidence-signing public keys. Old keys are kept for verification
-- after rotation; exactly one is active for signing. Private keys live outside
-- the database, in a protected file.
CREATE TABLE signing_keys (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    key_id     TEXT NOT NULL UNIQUE,
    public_key TEXT NOT NULL,        -- base64
    algorithm  TEXT NOT NULL,        -- ed25519
    active     INTEGER NOT NULL,     -- 1 = current signing key
    created_at TEXT NOT NULL
);

-- Stage-2 final manifests: hashes of the final export files (and the PDF, once
-- it exists), signed with a dedicated key.
CREATE TABLE final_manifests (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id            TEXT NOT NULL,
    evidence_bundle_hash TEXT,
    final_pdf_sha256     TEXT,
    manifest_sha256      TEXT NOT NULL,
    signing_key_id       TEXT,
    signature            TEXT,        -- base64 detached signature
    manifest_path        TEXT NOT NULL,
    created_at           TEXT NOT NULL
);

-- Backup runs and their verification result.
CREATE TABLE backup_runs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id    TEXT,
    target       TEXT NOT NULL,
    files_copied INTEGER,
    verified     INTEGER,            -- 1 = every copied file matched the manifest
    result       TEXT,
    created_at   TEXT NOT NULL
);
