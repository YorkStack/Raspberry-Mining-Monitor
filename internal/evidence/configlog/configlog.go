// Package configlog records the effective miner configuration over time.
//
// It is append-only: a change closes the current record (valid_to) and inserts
// a new one, with an audit event. Recording an unchanged configuration is a
// no-op, so the history holds only real changes.
package configlog

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
)

// Config is one effective miner configuration.
type Config struct {
	MinerInternalID  string
	OperatingMode    string // SOLO / POOL / UNKNOWN
	PoolEndpoint     string
	PoolName         string
	PayoutScheme     string // SOLO / PPS / FPPS / PPLNS / UNKNOWN
	PayoutAddress    string
	FirmwareVersion  string
	Frequency        string
	Voltage          string
	PowerLimit       string
	FanSettings      string
	MonitorVersion   string
	MonitorGitCommit string
}

// Hash is the canonical content hash used to detect real changes.
func (c Config) Hash() string {
	h := sha256.New()
	for _, s := range []string{c.OperatingMode, c.PoolEndpoint, c.PoolName, c.PayoutScheme,
		c.PayoutAddress, c.FirmwareVersion, c.Frequency, c.Voltage, c.PowerLimit, c.FanSettings} {
		h.Write([]byte(strconv.Itoa(len(s))))
		h.Write([]byte(":"))
		h.Write([]byte(s))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Store records configuration history.
type Store struct {
	db  *sql.DB
	log *audit.Log
}

// New creates a store.
func New(db *sql.DB, log *audit.Log) *Store { return &Store{db: db, log: log} }

// currentHash returns the current config hash for a miner, or "" if none.
func (s *Store) currentHash(tx *sql.Tx, minerID string) (string, error) {
	var h string
	err := tx.QueryRow(`SELECT config_hash FROM miner_configurations
		WHERE miner_internal_id = ? AND valid_to IS NULL`, minerID).Scan(&h)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return h, err
}

// Record stores a new effective configuration if it differs from the current
// one. It returns changed=false when the configuration is unchanged.
func (s *Store) Record(c Config, reason, actor string, now time.Time) (changed bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	hash := c.Hash()
	cur, err := s.currentHash(tx, c.MinerInternalID)
	if err != nil {
		return false, err
	}
	if cur == hash {
		return false, nil // no real change
	}

	if cur != "" {
		if _, err := tx.Exec(`UPDATE miner_configurations SET valid_to = ?
			WHERE miner_internal_id = ? AND valid_to IS NULL`,
			now.UTC().Format(time.RFC3339), c.MinerInternalID); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(`INSERT INTO miner_configurations
		(miner_internal_id, valid_from, valid_to, operating_mode, pool_endpoint, pool_name,
		 payout_scheme, payout_address, firmware_version, frequency, voltage, power_limit,
		 fan_settings, config_hash, monitor_version, monitor_git_commit, change_reason, created_at, created_by)
		VALUES (?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.MinerInternalID, now.UTC().Format(time.RFC3339), c.OperatingMode, c.PoolEndpoint, c.PoolName,
		c.PayoutScheme, c.PayoutAddress, c.FirmwareVersion, c.Frequency, c.Voltage, c.PowerLimit,
		c.FanSettings, hash, c.MonitorVersion, c.MonitorGitCommit, reason,
		now.UTC().Format(time.RFC3339), actor); err != nil {
		return false, err
	}
	if _, err := s.log.AppendTx(tx, audit.Event{
		EventUID: "cfg-" + c.MinerInternalID + "-" + now.UTC().Format(time.RFC3339Nano),
		TsUTC:    now, Actor: actor, Type: "config.changed", Entity: "miner_config",
		EntityID: c.MinerInternalID, NewValueHash: hash, Reason: reason,
	}); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// Record in history: version count and current mode for tests/reports.
type Record struct {
	Config
	ValidFrom time.Time
	ValidTo   *time.Time
	Reason    string
}

// History returns every configuration for a miner, oldest first.
func (s *Store) History(minerID string) ([]Record, error) {
	rows, err := s.db.Query(`SELECT operating_mode, pool_endpoint, pool_name, payout_scheme,
		payout_address, firmware_version, valid_from, valid_to, change_reason
		FROM miner_configurations WHERE miner_internal_id = ? ORDER BY id ASC`, minerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var (
			r         Record
			validFrom string
			validTo   sql.NullString
		)
		if err := rows.Scan(&r.OperatingMode, &r.PoolEndpoint, &r.PoolName, &r.PayoutScheme,
			&r.PayoutAddress, &r.FirmwareVersion, &validFrom, &validTo, &r.Reason); err != nil {
			return nil, err
		}
		r.MinerInternalID = minerID
		r.ValidFrom, _ = time.Parse(time.RFC3339, validFrom)
		if validTo.Valid {
			t, _ := time.Parse(time.RFC3339, validTo.String)
			r.ValidTo = &t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
