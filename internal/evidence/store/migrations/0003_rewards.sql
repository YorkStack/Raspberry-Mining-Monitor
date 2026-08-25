-- Miner configuration history, watch-only reward detection, and EUR valuation.
-- All Bitcoin amounts are satoshi, all money is euro cents, both INTEGER.

-- Append-only effective miner configuration. A change closes the previous row
-- (valid_to) and inserts a new one; history is never rewritten.
CREATE TABLE miner_configurations (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    miner_internal_id  TEXT NOT NULL,
    valid_from         TEXT NOT NULL,
    valid_to           TEXT,                 -- NULL = current
    operating_mode     TEXT NOT NULL,        -- SOLO / POOL / UNKNOWN
    pool_endpoint      TEXT,
    pool_name          TEXT,
    payout_scheme      TEXT,                 -- SOLO / PPS / FPPS / PPLNS / UNKNOWN
    payout_address     TEXT,
    firmware_version   TEXT,
    frequency          TEXT,
    voltage            TEXT,
    power_limit        TEXT,
    fan_settings       TEXT,
    config_hash        TEXT,
    monitor_version    TEXT,
    monitor_git_commit TEXT,
    change_reason      TEXT,
    created_at         TEXT NOT NULL,
    created_by         TEXT NOT NULL
);
CREATE INDEX idx_cfg_miner ON miner_configurations(miner_internal_id);

-- Watch-only addresses. Private keys, seeds and passwords are never stored.
CREATE TABLE watched_addresses (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    address    TEXT NOT NULL UNIQUE,
    label      TEXT,
    added_at   TEXT NOT NULL,
    removed_at TEXT,                         -- soft remove; history kept
    added_by   TEXT NOT NULL
);

-- Potential mining rewards seen on watched addresses. All relevant timestamps
-- are stored; no single timestamp is hard-coded as the authoritative receipt
-- date. Unique per (txid, vout).
CREATE TABLE reward_events (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    address               TEXT NOT NULL,
    txid                  TEXT NOT NULL,
    vout                  INTEGER NOT NULL,
    amount_sat            INTEGER NOT NULL,
    block_height          INTEGER,
    block_hash            TEXT,
    block_time            TEXT,
    first_seen            TEXT,
    first_confirmation    TEXT,
    confirmations         INTEGER,
    maturity_time         TEXT,              -- coinbase maturity / 100-conf, where applicable
    spendable_time        TEXT,
    source_classification TEXT,              -- DIRECT_COINBASE / SOLO_POOL_PAYMENT / POOL_PAYMENT / UNKNOWN
    gross_sat             INTEGER,
    pool_fee_sat          INTEGER,
    net_sat               INTEGER,
    pool_name             TEXT,
    evidence_source       TEXT,
    raw_response          TEXT,
    raw_sha256            TEXT,
    status                TEXT,              -- SEEN / CONFIRMED / MATURE / REORGED
    note                  TEXT,
    created_at            TEXT NOT NULL,
    UNIQUE(txid, vout)
);
CREATE INDEX idx_reward_address ON reward_events(address);

-- Append-only status changes for rewards (e.g. reorganisation). The original
-- reward_event is preserved; a reorg adds a status event, never a delete.
CREATE TABLE reward_status_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    txid       TEXT NOT NULL,
    vout       INTEGER NOT NULL,
    status     TEXT NOT NULL,
    ts_utc     TEXT NOT NULL,
    reason     TEXT,
    created_at TEXT NOT NULL
);

-- Versioned EUR valuation policy per tax year.
CREATE TABLE valuation_policies (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    tax_year          INTEGER NOT NULL,
    version           INTEGER NOT NULL,
    currency          TEXT NOT NULL,
    primary_source    TEXT,
    fallback_source   TEXT,
    method            TEXT,                  -- SPOT / OPEN / CLOSE / AVERAGE / MANUAL
    timezone          TEXT,
    rounding          TEXT,
    decimal_precision INTEGER,
    created_at        TEXT NOT NULL,
    created_by        TEXT NOT NULL,
    UNIQUE(tax_year, version)
);

-- Valuation of a reward. Rates are EUR cents per whole BTC; the reward's EUR
-- value is cents. A manual correction inserts a new row referencing the
-- original via supersedes_id; the original is preserved.
CREATE TABLE valuation_snapshots (
    id                      INTEGER PRIMARY KEY AUTOINCREMENT,
    reward_event_id         INTEGER NOT NULL REFERENCES reward_events(id),
    policy_year             INTEGER,
    policy_version          INTEGER,
    method                  TEXT,
    spot_rate_cents         INTEGER,
    open_rate_cents         INTEGER,
    close_rate_cents        INTEGER,
    average_rate_cents      INTEGER,
    selected_rate_cents     INTEGER NOT NULL,
    primary_source          TEXT,
    fallback_source         TEXT,
    fallback_reason         TEXT,
    api_endpoint            TEXT,
    api_retrieved_at        TEXT,
    raw_response            TEXT,
    raw_sha256              TEXT,
    amount_sat              INTEGER NOT NULL,
    amount_eur_cents        INTEGER NOT NULL,
    rounding                TEXT,
    manual_adjustment_cents INTEGER,
    adjustment_reason       TEXT,
    adjusted_by             TEXT,
    adjusted_at             TEXT,
    supersedes_id           INTEGER,
    created_at              TEXT NOT NULL
);
CREATE INDEX idx_valuation_reward ON valuation_snapshots(reward_event_id);
