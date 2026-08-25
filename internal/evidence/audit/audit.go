// Package audit is the append-only audit log with a hash chain.
//
// Each entry's hash is computed over the previous entry's hash plus the entry's
// own fields, so deleting or editing any entry breaks the chain from that point
// on and Verify detects it. Corrections never overwrite: a correction is a new
// entry that references the original.
package audit

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

// genesis is the previous-hash of the very first entry.
const genesis = "0000000000000000000000000000000000000000000000000000000000000000"

// Event is one auditable action. Value hashes are optional (hex of the
// serialised previous/new state where applicable).
type Event struct {
	EventUID      string
	TsUTC         time.Time
	Actor         string
	Type          string
	Entity        string
	EntityID      string
	PrevValueHash string
	NewValueHash  string
	Reason        string
}

// Log is the audit log over a database.
type Log struct {
	db  *sql.DB
	loc *time.Location
}

// New creates a log. loc is the local timezone recorded alongside UTC.
func New(db *sql.DB, loc *time.Location) *Log { return &Log{db: db, loc: loc} }

// entryHash chains the previous hash with the entry fields. Fields are
// length-prefixed so no combination of values can be confused for another.
func entryHash(prevEntryHash string, e Event) string {
	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(strconv.Itoa(len(s))))
		_, _ = h.Write([]byte(":"))
		_, _ = h.Write([]byte(s))
	}
	write(prevEntryHash)
	write(e.EventUID)
	write(e.TsUTC.UTC().Format(time.RFC3339Nano))
	write(e.Actor)
	write(e.Type)
	write(e.Entity)
	write(e.EntityID)
	write(e.PrevValueHash)
	write(e.NewValueHash)
	write(e.Reason)
	return hex.EncodeToString(h.Sum(nil))
}

// lastHash returns the most recent entry hash, or genesis when empty.
func (l *Log) lastHash(tx *sql.Tx) (string, error) {
	var hash string
	err := tx.QueryRow("SELECT entry_hash FROM audit_log ORDER BY id DESC LIMIT 1").Scan(&hash)
	if err == sql.ErrNoRows {
		return genesis, nil
	}
	if err != nil {
		return "", err
	}
	return hash, nil
}

// AppendTx records an event within the caller's transaction, so a data change
// and its audit entry commit atomically. It returns the new entry hash.
func (l *Log) AppendTx(tx *sql.Tx, e Event) (string, error) {
	prev, err := l.lastHash(tx)
	if err != nil {
		return "", err
	}
	hash := entryHash(prev, e)

	tsLocal := e.TsUTC.In(l.loc).Format(time.RFC3339)
	_, err = tx.Exec(`INSERT INTO audit_log
		(event_uid, ts_utc, ts_local, actor, event_type, entity, entity_id,
		 prev_value_hash, new_value_hash, reason, prev_entry_hash, entry_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.EventUID, e.TsUTC.UTC().Format(time.RFC3339Nano), tsLocal, e.Actor, e.Type,
		e.Entity, e.EntityID, e.PrevValueHash, e.NewValueHash, e.Reason, prev, hash)
	if err != nil {
		return "", fmt.Errorf("audit: append: %w", err)
	}
	return hash, nil
}

// Append records an event in its own transaction.
func (l *Log) Append(e Event) (string, error) {
	tx, err := l.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	hash, err := l.AppendTx(tx, e)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return hash, nil
}

// Verify walks the whole chain and recomputes each entry hash. It returns
// ok=false and the id of the first broken entry when the chain does not hold.
func (l *Log) Verify() (ok bool, brokenID int64, err error) {
	rows, err := l.db.Query(`SELECT id, event_uid, ts_utc, actor, event_type, entity, entity_id,
		prev_value_hash, new_value_hash, reason, prev_entry_hash, entry_hash
		FROM audit_log ORDER BY id ASC`)
	if err != nil {
		return false, 0, err
	}
	defer rows.Close()

	prev := genesis
	for rows.Next() {
		var (
			id                              int64
			tsUTC                           string
			e                               Event
			storedPrevHash, storedEntryHash string
		)
		if err := rows.Scan(&id, &e.EventUID, &tsUTC, &e.Actor, &e.Type, &e.Entity,
			&e.EntityID, &e.PrevValueHash, &e.NewValueHash, &e.Reason,
			&storedPrevHash, &storedEntryHash); err != nil {
			return false, 0, err
		}
		t, err := time.Parse(time.RFC3339Nano, tsUTC)
		if err != nil {
			return false, id, nil
		}
		e.TsUTC = t
		if storedPrevHash != prev {
			return false, id, nil
		}
		if entryHash(prev, e) != storedEntryHash {
			return false, id, nil
		}
		prev = storedEntryHash
	}
	if err := rows.Err(); err != nil {
		return false, 0, err
	}
	return true, 0, nil
}

// Count returns the number of entries.
func (l *Log) Count() (int, error) {
	var n int
	err := l.db.QueryRow("SELECT COUNT(*) FROM audit_log").Scan(&n)
	return n, err
}
