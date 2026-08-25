// Package network stores Bitcoin network snapshots as append-only evidence.
//
// Each snapshot keeps the raw API response and its SHA-256, so the exact data
// seen at the time is preserved. Historical snapshots are never modified when
// later data changes; re-recording the same snapshot UID is a no-op.
package network

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"
)

// Snapshot is one network observation. Subsidy/fees/reward are satoshi;
// difficulty and network hashrate are physical values stored as REAL.
type Snapshot struct {
	UID               string
	TsUTC             time.Time
	BlockHeight       int64
	Difficulty        float64
	NetworkHashrateHs float64
	SubsidySat        int64
	AvgTxFeesSat      int64
	RewardPerBlockSat int64
	Source            string
	SourceEndpoint    string
	APIRetrievedAt    time.Time
	DataQuality       string
}

// Store persists network snapshots.
type Store struct {
	db  *sql.DB
	loc *time.Location
}

// New creates a store.
func New(db *sql.DB, loc *time.Location) *Store { return &Store{db: db, loc: loc} }

// Record stores a snapshot with the raw response and its hash. It is
// append-only: if a snapshot with this UID already exists it is left untouched
// and inserted=false is returned.
func (s *Store) Record(snap Snapshot, rawResponse []byte, now time.Time) (bool, error) {
	sum := sha256.Sum256(rawResponse)
	hash := hex.EncodeToString(sum[:])

	res, err := s.db.Exec(`INSERT OR IGNORE INTO network_snapshots
		(snapshot_uid, ts_utc, ts_local, block_height, difficulty, network_hashrate_hs,
		 subsidy_sat, avg_tx_fees_sat, reward_per_block_sat, source, source_endpoint,
		 api_retrieved_at, raw_response, raw_sha256, data_quality, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snap.UID, snap.TsUTC.UTC().Format(time.RFC3339), snap.TsUTC.In(s.loc).Format(time.RFC3339),
		snap.BlockHeight, snap.Difficulty, snap.NetworkHashrateHs,
		snap.SubsidySat, snap.AvgTxFeesSat, snap.RewardPerBlockSat,
		snap.Source, snap.SourceEndpoint, snap.APIRetrievedAt.UTC().Format(time.RFC3339),
		string(rawResponse), hash, snap.DataQuality, now.UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RawHash returns the stored SHA-256 of a snapshot's raw response.
func (s *Store) RawHash(uid string) (string, bool, error) {
	var h string
	err := s.db.QueryRow("SELECT raw_sha256 FROM network_snapshots WHERE snapshot_uid = ?", uid).Scan(&h)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return h, err == nil, err
}

// Reward returns the stored reward_per_block_sat for a snapshot.
func (s *Store) Reward(uid string) (int64, bool, error) {
	var v int64
	err := s.db.QueryRow("SELECT reward_per_block_sat FROM network_snapshots WHERE snapshot_uid = ?", uid).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	return v, err == nil, err
}
