// Package report closes reporting periods and produces evidence packages.
//
// Closing runs validation checks and lists warnings; a period with warnings can
// only be closed when the operator acknowledges them, and the acknowledged
// warnings are stored with the report. A closed original report is never
// overwritten: a later correction creates a numbered revision that references
// the original, which stays intact.
package report

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/export"
)

// Statuses.
const (
	StatusClosed             = "CLOSED"
	StatusClosedWithWarnings = "CLOSED_WITH_WARNINGS"
	StatusRevised            = "REVISED"
)

// Warning is a validation finding surfaced before closing.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Store closes reporting periods.
type Store struct {
	db              *sql.DB
	log             *audit.Log
	reportsDir      string
	softwareVersion string
}

// New creates a store.
func New(db *sql.DB, log *audit.Log, reportsDir, softwareVersion string) *Store {
	return &Store{db: db, log: log, reportsDir: reportsDir, softwareVersion: softwareVersion}
}

func (s *Store) schemaVersion() int {
	var v sql.NullInt64
	_ = s.db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&v)
	return int(v.Int64)
}

// Validate runs the pre-close checks and returns any warnings.
func (s *Store) Validate(period string) ([]Warning, error) {
	var out []Warning
	add := func(code, msg string) { out = append(out, Warning{Code: code, Message: msg}) }

	count := func(query string, args ...any) (int, error) {
		var n int
		err := s.db.QueryRow(query, args...).Scan(&n)
		return n, err
	}

	if n, err := count(`SELECT COUNT(*) FROM miner_versions WHERE superseded_at IS NULL
		AND (serial_number IS NULL OR serial_number = '')`); err != nil {
		return nil, err
	} else if n > 0 {
		add("missing_serial", fmt.Sprintf("%d miner(s) have no serial number", n))
	}
	if n, err := count(`SELECT COUNT(*) FROM miner_versions WHERE superseded_at IS NULL
		AND (invoice_number IS NULL OR invoice_number = '')`); err != nil {
		return nil, err
	} else if n > 0 {
		add("missing_invoice", fmt.Sprintf("%d miner(s) have no invoice number", n))
	}
	if n, err := count(`SELECT COUNT(*) FROM watched_addresses WHERE removed_at IS NULL`); err != nil {
		return nil, err
	} else if n == 0 {
		add("no_watched_address", "no payout address is being watched")
	}
	if n, err := count(`SELECT COUNT(*) FROM reward_events WHERE status = 'SEEN'`); err != nil {
		return nil, err
	} else if n > 0 {
		add("unconfirmed_rewards", fmt.Sprintf("%d reward(s) are still unconfirmed", n))
	}
	if n, err := count(`SELECT COUNT(*) FROM telemetry_hourly WHERE completeness_pct < 100`); err != nil {
		return nil, err
	} else if n > 0 {
		add("telemetry_gaps", fmt.Sprintf("%d hour(s) have incomplete telemetry", n))
	}
	if n, err := count(`SELECT COUNT(*) FROM miner_configurations WHERE valid_to IS NULL AND operating_mode = 'UNKNOWN'`); err != nil {
		return nil, err
	} else if n > 0 {
		add("unknown_pool_config", fmt.Sprintf("%d miner(s) have an unknown operating mode", n))
	}
	return out, nil
}

// latestRevision returns the highest revision number and its report id for a
// period, or (-1, "") if none.
func (s *Store) latestRevision(period string) (int, string, error) {
	var (
		rev sql.NullInt64
		id  sql.NullString
	)
	err := s.db.QueryRow(`SELECT revision, report_id FROM reports WHERE period = ?
		ORDER BY revision DESC LIMIT 1`, period).Scan(&rev, &id)
	if err == sql.ErrNoRows {
		return -1, "", nil
	}
	if err != nil {
		return -1, "", err
	}
	return int(rev.Int64), id.String, nil
}

func (s *Store) writeReport(period, reportID string, revision int, supersedes, reason, bundleHash, packageDir, status string, warnings []Warning, actor string, now time.Time) error {
	wj, _ := json.Marshal(warnings)
	// Read the schema version before opening the transaction: querying s.db
	// while a tx holds the single connection would deadlock.
	schemaVersion := s.schemaVersion()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO reporting_periods (period, status, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(period) DO UPDATE SET status = excluded.status, updated_at = excluded.updated_at`,
		period, status, now.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO reports
		(report_id, period, revision, supersedes_report_id, reason, evidence_bundle_hash, warnings_json,
		 package_dir, status, software_version, schema_version, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		reportID, period, revision, nullStr(supersedes), nullStr(reason), bundleHash, string(wj),
		packageDir, status, s.softwareVersion, schemaVersion, now.UTC().Format(time.RFC3339), actor); err != nil {
		return err
	}
	if _, err := s.log.AppendTx(tx, audit.Event{
		EventUID: "report-" + reportID + "-" + now.UTC().Format(time.RFC3339Nano),
		TsUTC:    now, Actor: actor, Type: "report.generated", Entity: "report", EntityID: reportID,
		NewValueHash: bundleHash, Reason: reason,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// Close closes a period's original report. It refuses if the original already
// exists (use Revise), and refuses a period with warnings unless acknowledged.
func (s *Store) Close(period string, acknowledge bool, actor string, now time.Time) (reportID, bundleHash string, warnings []Warning, err error) {
	rev, _, err := s.latestRevision(period)
	if err != nil {
		return "", "", nil, err
	}
	if rev >= 0 {
		return "", "", nil, fmt.Errorf("report: period %s already closed; use a revision", period)
	}

	warnings, err = s.Validate(period)
	if err != nil {
		return "", "", nil, err
	}
	if len(warnings) > 0 && !acknowledge {
		return "", "", warnings, fmt.Errorf("report: period %s has %d warning(s); acknowledge to close", period, len(warnings))
	}

	reportID = "MINING-" + period + "-ORIGINAL"
	dir := filepath.Join(s.reportsDir, period, "original")
	bundleHash, _, err = export.WriteEvidencePackage(s.db, dir)
	if err != nil {
		return "", "", nil, err
	}
	status := StatusClosed
	if len(warnings) > 0 {
		status = StatusClosedWithWarnings
	}
	if err := s.writeReport(period, reportID, 0, "", "", bundleHash, dir, status, warnings, actor, now); err != nil {
		return "", "", nil, err
	}
	return reportID, bundleHash, warnings, nil
}

// Revise creates the next numbered revision of a closed period, referencing the
// original (or previous revision). The earlier reports and packages are kept.
func (s *Store) Revise(period, reason, actor string, now time.Time) (reportID, bundleHash string, err error) {
	rev, prevID, err := s.latestRevision(period)
	if err != nil {
		return "", "", err
	}
	if rev < 0 {
		return "", "", fmt.Errorf("report: period %s has no original to revise", period)
	}
	newRev := rev + 1
	reportID = fmt.Sprintf("MINING-%s-REVISION-%03d", period, newRev)
	dir := filepath.Join(s.reportsDir, period, fmt.Sprintf("revision-%03d", newRev))
	bundleHash, _, err = export.WriteEvidencePackage(s.db, dir)
	if err != nil {
		return "", "", err
	}
	if err := s.writeReport(period, reportID, newRev, prevID, reason, bundleHash, dir, StatusRevised, nil, actor, now); err != nil {
		return "", "", err
	}
	return reportID, bundleHash, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
