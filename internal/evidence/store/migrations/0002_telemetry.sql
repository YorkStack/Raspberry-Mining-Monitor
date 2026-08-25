-- Telemetry persistence, aggregates, network snapshots and immutable
-- contemporaneous expected-value snapshots.
--
-- Physical measurements (hashrate, difficulty, temperature, power) are stored as
-- REAL or as H/s integers; they are not money. Monetary and Bitcoin amounts are
-- always integers: satoshi and euro cents. Network hashrate is REAL because it
-- exceeds the int64 range in H/s.

-- Raw per-poll telemetry. Retention is short (default 30 days); aggregates are
-- kept for years.
CREATE TABLE telemetry_raw (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    miner_internal_id  TEXT NOT NULL,
    ts_utc             TEXT NOT NULL,
    online             INTEGER NOT NULL,     -- 0/1
    hashrate_hs        INTEGER,              -- instantaneous, H/s
    hashrate_1h_hs     INTEGER,
    accepted_shares    INTEGER,
    rejected_shares    INTEGER,
    hw_errors          INTEGER,
    best_difficulty    REAL,
    asic_temp_c        REAL,
    vrm_temp_c         REAL,
    fan_rpm            INTEGER,
    power_w            REAL,
    uptime_s           INTEGER,
    api_available      INTEGER,              -- 0/1
    data_quality       TEXT
);
CREATE INDEX idx_telem_raw_miner_ts ON telemetry_raw(miner_internal_id, ts_utc);

-- Hourly aggregates with data-completeness accounting.
CREATE TABLE telemetry_hourly (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    miner_internal_id  TEXT NOT NULL,
    hour_start_utc     TEXT NOT NULL,
    avg_hashrate_hs    INTEGER,
    min_hashrate_hs    INTEGER,
    max_hashrate_hs    INTEGER,
    avg_power_w        REAL,
    min_temp_c         REAL,
    max_temp_c         REAL,
    energy_wh          INTEGER,
    accepted_delta     INTEGER,
    rejected_delta     INTEGER,
    online_minutes     INTEGER,
    offline_minutes    INTEGER,
    expected_samples   INTEGER,
    received_samples   INTEGER,
    completeness_pct   REAL,
    gaps               TEXT,
    created_at         TEXT NOT NULL,
    UNIQUE(miner_internal_id, hour_start_utc)
);

-- Daily aggregates (rolled up from hourly in a later step; table defined now).
CREATE TABLE telemetry_daily (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    miner_internal_id  TEXT NOT NULL,
    day_start_utc      TEXT NOT NULL,
    avg_hashrate_hs    INTEGER,
    min_hashrate_hs    INTEGER,
    max_hashrate_hs    INTEGER,
    energy_wh          INTEGER,
    accepted_delta     INTEGER,
    rejected_delta     INTEGER,
    online_minutes     INTEGER,
    offline_minutes    INTEGER,
    completeness_pct   REAL,
    created_at         TEXT NOT NULL,
    UNIQUE(miner_internal_id, day_start_utc)
);

-- Bitcoin network snapshots. Append-only: historical snapshots are never
-- modified when later data changes. The raw API response and its hash are kept.
CREATE TABLE network_snapshots (
    snapshot_uid         TEXT PRIMARY KEY,
    ts_utc               TEXT NOT NULL,
    ts_local             TEXT NOT NULL,
    block_height         INTEGER,
    difficulty           REAL,
    network_hashrate_hs  REAL,             -- exceeds int64 in H/s, stored as REAL
    subsidy_sat          INTEGER,          -- satoshi
    avg_tx_fees_sat      INTEGER,          -- estimated, satoshi
    reward_per_block_sat INTEGER,          -- subsidy + fees, satoshi
    source               TEXT,
    source_endpoint      TEXT,
    api_retrieved_at     TEXT,
    raw_response         TEXT,
    raw_sha256           TEXT,
    data_quality         TEXT,
    created_at           TEXT NOT NULL
);

-- Contemporaneous expected-value snapshots. One row per (network snapshot,
-- formula version). Immutable: never recalculated or overwritten when
-- difficulty, hashrate, price or subsidy later change. A formula change creates
-- a new formula_version rather than editing existing rows.
CREATE TABLE expected_value_snapshots (
    id                          INTEGER PRIMARY KEY AUTOINCREMENT,
    network_snapshot_uid        TEXT NOT NULL REFERENCES network_snapshots(snapshot_uid),
    ts_utc                      TEXT NOT NULL,
    formula_version             INTEGER NOT NULL,
    -- frozen inputs
    miner_hashrate_hs           REAL NOT NULL,
    network_hashrate_hs         REAL NOT NULL,
    difficulty                  REAL NOT NULL,
    reward_per_block_sat        INTEGER NOT NULL,
    btc_price_cents             INTEGER,          -- EUR cents per BTC
    electricity_price_cents_kwh INTEGER,
    pool_fee_ppm                INTEGER,          -- parts per million
    -- outputs: Bitcoin in satoshi, money in euro cents, time in seconds
    expected_sat_day            INTEGER NOT NULL,
    expected_sat_month          INTEGER NOT NULL,
    expected_sat_year           INTEGER NOT NULL,
    expected_eur_cents_day      INTEGER,
    expected_eur_cents_month    INTEGER,
    expected_eur_cents_year     INTEGER,
    prob_block_day              REAL NOT NULL,
    prob_block_month            REAL NOT NULL,
    prob_block_year             REAL NOT NULL,
    mean_seconds_to_block       REAL NOT NULL,
    expected_energy_wh_day      INTEGER,
    expected_electricity_cents_day INTEGER,
    expected_pool_fee_cents_day INTEGER,
    expected_net_cents_day      INTEGER,
    created_at                  TEXT NOT NULL,
    UNIQUE(network_snapshot_uid, formula_version)
);
