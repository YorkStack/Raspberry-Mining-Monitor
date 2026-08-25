// Package miner is the versioned miner inventory. Master data is append-only:
// a change supersedes the current version and inserts a new one, so history is
// always preserved. Every change writes an audit event in the same transaction.
package miner

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
)

// Master is the miner master data. Optional numeric fields are pointers so an
// unknown value is never recorded as a confident zero.
type Master struct {
	Manufacturer         string
	Model                string
	SerialNumber         string
	PurchaseDate         string // ISO date
	PurchasePriceCents   *int64 // euro cents
	InvoiceNumber        string
	InvoicePath          string
	InvoiceSHA256        string
	NominalHashrateHs    *int64 // H/s
	NominalPowerW        *int64 // watts
	Location             string
	CommissioningDate    string
	DecommissioningDate  string
	FirmwareVersion      string
	Note                 string
}

// Version is one stored version of a miner's master data.
type Version struct {
	InternalID   string
	Version      int
	ValidFrom    time.Time
	SupersededAt *time.Time
	Master
}

// Store is the miner inventory over a database and audit log.
type Store struct {
	db  *sql.DB
	log *audit.Log
}

// New creates a store.
func New(db *sql.DB, log *audit.Log) *Store { return &Store{db: db, log: log} }

func nullInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func (s *Store) insertVersion(tx *sql.Tx, minerID int64, version int, m Master, validFrom, now time.Time) error {
	_, err := tx.Exec(`INSERT INTO miner_versions
		(miner_id, version, valid_from, superseded_at, manufacturer, model, serial_number,
		 purchase_date, purchase_price_cents, invoice_number, invoice_path, invoice_sha256,
		 nominal_hashrate_hs, nominal_power_w, location, commissioning_date, decommissioning_date,
		 firmware_version, note, created_at)
		VALUES (?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		minerID, version, validFrom.UTC().Format(time.RFC3339), m.Manufacturer, m.Model, m.SerialNumber,
		m.PurchaseDate, nullInt(m.PurchasePriceCents), m.InvoiceNumber, m.InvoicePath, m.InvoiceSHA256,
		nullInt(m.NominalHashrateHs), nullInt(m.NominalPowerW), m.Location, m.CommissioningDate,
		m.DecommissioningDate, m.FirmwareVersion, m.Note, now.UTC().Format(time.RFC3339))
	return err
}

// Create registers a new miner with version 1.
func (s *Store) Create(internalID string, m Master, actor string, now time.Time) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec("INSERT INTO miners (internal_id, created_at) VALUES (?, ?)",
		internalID, now.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("miner: create %q: %w", internalID, err)
	}
	minerID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := s.insertVersion(tx, minerID, 1, m, now, now); err != nil {
		return 0, err
	}
	if _, err := s.log.AppendTx(tx, audit.Event{
		EventUID: "miner-create-" + internalID + "-" + now.UTC().Format(time.RFC3339Nano),
		TsUTC:    now, Actor: actor, Type: "miner.created", Entity: "miner", EntityID: internalID,
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return minerID, nil
}

// Update supersedes the current version and inserts a new one.
func (s *Store) Update(internalID string, m Master, actor, reason string, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var minerID int64
	var curVersion int
	err = tx.QueryRow(`SELECT mv.miner_id, mv.version FROM miner_versions mv
		JOIN miners m ON m.id = mv.miner_id
		WHERE m.internal_id = ? AND mv.superseded_at IS NULL`, internalID).Scan(&minerID, &curVersion)
	if err == sql.ErrNoRows {
		return fmt.Errorf("miner: %q not found", internalID)
	}
	if err != nil {
		return err
	}

	if _, err := tx.Exec(`UPDATE miner_versions SET superseded_at = ?
		WHERE miner_id = ? AND superseded_at IS NULL`, now.UTC().Format(time.RFC3339), minerID); err != nil {
		return err
	}
	if err := s.insertVersion(tx, minerID, curVersion+1, m, now, now); err != nil {
		return err
	}
	if _, err := s.log.AppendTx(tx, audit.Event{
		EventUID: "miner-update-" + internalID + "-" + now.UTC().Format(time.RFC3339Nano),
		TsUTC:    now, Actor: actor, Type: "miner.updated", Entity: "miner", EntityID: internalID,
		Reason:   reason,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func scanVersion(rows interface{ Scan(...any) error }, internalID string) (Version, error) {
	var (
		v            Version
		validFrom    string
		superseded   sql.NullString
		price, hs, w sql.NullInt64
	)
	err := rows.Scan(&v.Version, &validFrom, &superseded, &v.Manufacturer, &v.Model, &v.SerialNumber,
		&v.PurchaseDate, &price, &v.InvoiceNumber, &v.InvoicePath, &v.InvoiceSHA256,
		&hs, &w, &v.Location, &v.CommissioningDate, &v.DecommissioningDate, &v.FirmwareVersion, &v.Note)
	if err != nil {
		return Version{}, err
	}
	v.InternalID = internalID
	v.ValidFrom, _ = time.Parse(time.RFC3339, validFrom)
	if superseded.Valid {
		t, _ := time.Parse(time.RFC3339, superseded.String)
		v.SupersededAt = &t
	}
	if price.Valid {
		v.PurchasePriceCents = &price.Int64
	}
	if hs.Valid {
		v.NominalHashrateHs = &hs.Int64
	}
	if w.Valid {
		v.NominalPowerW = &w.Int64
	}
	return v, nil
}

const versionCols = `mv.version, mv.valid_from, mv.superseded_at, mv.manufacturer, mv.model, mv.serial_number,
	mv.purchase_date, mv.purchase_price_cents, mv.invoice_number, mv.invoice_path, mv.invoice_sha256,
	mv.nominal_hashrate_hs, mv.nominal_power_w, mv.location, mv.commissioning_date, mv.decommissioning_date,
	mv.firmware_version, mv.note`

// Current returns the miner's current (non-superseded) version.
func (s *Store) Current(internalID string) (Version, error) {
	row := s.db.QueryRow(`SELECT `+versionCols+` FROM miner_versions mv
		JOIN miners m ON m.id = mv.miner_id
		WHERE m.internal_id = ? AND mv.superseded_at IS NULL`, internalID)
	return scanVersion(row, internalID)
}

// History returns every version of a miner, oldest first.
func (s *Store) History(internalID string) ([]Version, error) {
	rows, err := s.db.Query(`SELECT `+versionCols+` FROM miner_versions mv
		JOIN miners m ON m.id = mv.miner_id
		WHERE m.internal_id = ? ORDER BY mv.version ASC`, internalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Version
	for rows.Next() {
		v, err := scanVersion(rows, internalID)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
