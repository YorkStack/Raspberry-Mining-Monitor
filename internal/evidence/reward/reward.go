// Package reward records potential mining rewards seen on watch-only addresses.
//
// It never stores private keys, seeds or passwords — only public addresses and
// public blockchain data. Every relevant timestamp is stored; no single one is
// treated as the authoritative tax receipt date. A reorganisation preserves the
// original event and adds a status event rather than deleting anything.
package reward

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
)

// Source classifications.
const (
	SourceDirectCoinbase = "DIRECT_COINBASE"
	SourceSoloPool       = "SOLO_POOL_PAYMENT"
	SourcePool           = "POOL_PAYMENT"
	SourceUnknown        = "UNKNOWN"
)

// Statuses.
const (
	StatusSeen      = "SEEN"
	StatusConfirmed = "CONFIRMED"
	StatusMature    = "MATURE"
	StatusReorged   = "REORGED"
)

// Event is a potential reward. Optional fields are pointers.
type Event struct {
	Address              string
	TxID                 string
	Vout                 int64
	AmountSat            int64
	BlockHeight          *int64
	BlockHash            string
	BlockTime            *time.Time
	FirstSeen            *time.Time
	FirstConfirmation    *time.Time
	Confirmations        *int64
	MaturityTime         *time.Time
	SpendableTime        *time.Time
	SourceClassification string
	GrossSat             *int64
	PoolFeeSat           *int64
	NetSat               *int64
	PoolName             string
	EvidenceSource       string
	Status               string
	Note                 string
}

// Store persists rewards and watched addresses.
type Store struct {
	db  *sql.DB
	log *audit.Log
}

// New creates a store.
func New(db *sql.DB, log *audit.Log) *Store { return &Store{db: db, log: log} }

// AddAddress starts watching an address.
func (s *Store) AddAddress(address, label, actor string, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO watched_addresses (address, label, added_at, added_by)
		VALUES (?, ?, ?, ?)`, address, label, now.UTC().Format(time.RFC3339), actor); err != nil {
		return err
	}
	if _, err := s.log.AppendTx(tx, audit.Event{
		EventUID: "watch-add-" + address + "-" + now.UTC().Format(time.RFC3339Nano),
		TsUTC:    now, Actor: actor, Type: "address.added", Entity: "address", EntityID: address,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveAddress soft-removes an address, keeping its history.
func (s *Store) RemoveAddress(address, actor string, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE watched_addresses SET removed_at = ? WHERE address = ? AND removed_at IS NULL`,
		now.UTC().Format(time.RFC3339), address); err != nil {
		return err
	}
	if _, err := s.log.AppendTx(tx, audit.Event{
		EventUID: "watch-rm-" + address + "-" + now.UTC().Format(time.RFC3339Nano),
		TsUTC:    now, Actor: actor, Type: "address.removed", Entity: "address", EntityID: address,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// ActiveAddresses returns addresses currently watched.
func (s *Store) ActiveAddresses() ([]string, error) {
	rows, err := s.db.Query("SELECT address FROM watched_addresses WHERE removed_at IS NULL ORDER BY address")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func ts(p *time.Time) any {
	if p == nil {
		return nil
	}
	return p.UTC().Format(time.RFC3339)
}
func i(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// RecordReward stores a reward, keeping the raw evidence and its hash. It is
// idempotent per (txid, vout): a second call leaves the original untouched.
func (s *Store) RecordReward(e Event, rawResponse []byte, now time.Time) (inserted bool, err error) {
	sum := sha256.Sum256(rawResponse)
	hash := hex.EncodeToString(sum[:])
	if e.Status == "" {
		e.Status = StatusSeen
	}
	res, err := s.db.Exec(`INSERT OR IGNORE INTO reward_events
		(address, txid, vout, amount_sat, block_height, block_hash, block_time, first_seen,
		 first_confirmation, confirmations, maturity_time, spendable_time, source_classification,
		 gross_sat, pool_fee_sat, net_sat, pool_name, evidence_source, raw_response, raw_sha256, status, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Address, e.TxID, e.Vout, e.AmountSat, i(e.BlockHeight), e.BlockHash, ts(e.BlockTime), ts(e.FirstSeen),
		ts(e.FirstConfirmation), i(e.Confirmations), ts(e.MaturityTime), ts(e.SpendableTime), e.SourceClassification,
		i(e.GrossSat), i(e.PoolFeeSat), i(e.NetSat), e.PoolName, e.EvidenceSource, string(rawResponse), hash,
		e.Status, e.Note, now.UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// UpdateConfirmations advances the confirmation-related fields of a known
// reward as the chain matures. The immutable core (amount, block, first-seen,
// raw evidence) is not touched.
func (s *Store) UpdateConfirmations(txid string, vout, confirmations int64, maturity, spendable *time.Time, status string) error {
	_, err := s.db.Exec(`UPDATE reward_events
		SET confirmations = ?, maturity_time = COALESCE(?, maturity_time),
		    spendable_time = COALESCE(?, spendable_time), status = ?
		WHERE txid = ? AND vout = ?`,
		confirmations, ts(maturity), ts(spendable), status, txid, vout)
	return err
}

// MarkReorg records that a reward was reorganised out of the chain. The original
// reward_event is preserved; a status event and a REORGED status are added.
func (s *Store) MarkReorg(txid string, vout int64, reason string, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("UPDATE reward_events SET status = ? WHERE txid = ? AND vout = ?",
		StatusReorged, txid, vout); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO reward_status_events (txid, vout, status, ts_utc, reason, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, txid, vout, StatusReorged, now.UTC().Format(time.RFC3339), reason,
		now.UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

// Get returns the stored reward id, amount, status and raw hash for (txid,vout).
func (s *Store) Get(txid string, vout int64) (id, amountSat int64, status, rawHash string, confirmations *int64, err error) {
	var conf sql.NullInt64
	err = s.db.QueryRow(`SELECT id, amount_sat, status, raw_sha256, confirmations
		FROM reward_events WHERE txid = ? AND vout = ?`, txid, vout).Scan(&id, &amountSat, &status, &rawHash, &conf)
	if conf.Valid {
		confirmations = &conf.Int64
	}
	return
}
