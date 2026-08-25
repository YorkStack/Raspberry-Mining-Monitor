package report

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

func setup(t *testing.T) (*Store, *store.DB, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "e.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(time.Now()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	log := audit.New(db.SQL(), time.UTC)
	return New(db.SQL(), log, filepath.Join(dir, "reports"), "0.5.0"), db, dir
}

// A clean setup: one miner with serial+invoice, a watched address, no gaps.
func clean(db *store.DB) {
	now := time.Now().Format(time.RFC3339)
	db.SQL().Exec("INSERT INTO miners (internal_id, created_at) VALUES ('M1', ?)", now)
	db.SQL().Exec(`INSERT INTO miner_versions (miner_id, version, valid_from, serial_number, invoice_number, created_at)
		VALUES (1, 1, ?, 'SN1', 'INV-1', ?)`, now, now)
	db.SQL().Exec("INSERT INTO watched_addresses (address, added_at, added_by) VALUES ('bc1q', ?, 'york')", now)
}

func TestCloseGeneratesPackageAndIsImmutable(t *testing.T) {
	s, db, _ := setup(t)
	defer db.Close()
	clean(db)
	now := time.Date(2026, 8, 31, 23, 0, 0, 0, time.UTC)

	id, bundle, warnings, err := s.Close("2026-08", false, "york", now)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if id != "MINING-2026-08-ORIGINAL" {
		t.Errorf("report id = %q", id)
	}
	if bundle == "" || len(warnings) != 0 {
		t.Errorf("bundle=%q warnings=%v", bundle, warnings)
	}
	// Closing again is refused: original is never overwritten.
	if _, _, _, err := s.Close("2026-08", false, "york", now.Add(time.Hour)); err == nil {
		t.Error("closing an already-closed period was allowed")
	}
}

func TestReviseKeepsOriginal(t *testing.T) {
	s, db, _ := setup(t)
	defer db.Close()
	clean(db)
	now := time.Date(2026, 8, 31, 23, 0, 0, 0, time.UTC)
	orig, _, _, err := s.Close("2026-08", false, "york", now)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	rev, _, err := s.Revise("2026-08", "corrected a cost", "york", now.Add(48*time.Hour))
	if err != nil {
		t.Fatalf("Revise: %v", err)
	}
	if rev != "MINING-2026-08-REVISION-001" {
		t.Errorf("revision id = %q", rev)
	}
	// Original report row still present and unchanged.
	var supersedes string
	err = db.SQL().QueryRow("SELECT COALESCE(supersedes_report_id,'') FROM reports WHERE report_id = ?", orig).Scan(&supersedes)
	if err != nil {
		t.Fatalf("original gone: %v", err)
	}
	if supersedes != "" {
		t.Errorf("original should not supersede anything, got %q", supersedes)
	}
	var revSupersedes string
	db.SQL().QueryRow("SELECT supersedes_report_id FROM reports WHERE report_id = ?", rev).Scan(&revSupersedes)
	if revSupersedes != orig {
		t.Errorf("revision supersedes = %q, want %q", revSupersedes, orig)
	}
}

func TestWarningsRequireAcknowledgement(t *testing.T) {
	s, db, _ := setup(t)
	defer db.Close()
	// A miner with no serial and no invoice, no watched address => warnings.
	now := time.Now().Format(time.RFC3339)
	db.SQL().Exec("INSERT INTO miners (internal_id, created_at) VALUES ('M1', ?)", now)
	db.SQL().Exec("INSERT INTO miner_versions (miner_id, version, valid_from, created_at) VALUES (1, 1, ?, ?)", now, now)

	w, err := s.Validate("2026-08")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(w) == 0 {
		t.Fatal("expected warnings")
	}
	// Close without acknowledgement is refused.
	if _, _, _, err := s.Close("2026-08", false, "york", time.Now()); err == nil {
		t.Error("closed a period with warnings without acknowledgement")
	}
	// With acknowledgement it closes, status CLOSED_WITH_WARNINGS.
	id, _, warns, err := s.Close("2026-08", true, "york", time.Now())
	if err != nil {
		t.Fatalf("Close ack: %v", err)
	}
	if len(warns) == 0 {
		t.Error("acknowledged warnings not returned")
	}
	var status string
	db.SQL().QueryRow("SELECT status FROM reports WHERE report_id = ?", id).Scan(&status)
	if status != StatusClosedWithWarnings {
		t.Errorf("status = %q, want %q", status, StatusClosedWithWarnings)
	}
}
