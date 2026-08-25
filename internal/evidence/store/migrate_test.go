package store

import (
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)

func TestMigrateAppliesFoundationAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(t0); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	v, err := db.SchemaVersion()
	if err != nil || v < 1 {
		t.Fatalf("SchemaVersion = %d, %v; want >= 1", v, err)
	}
	firstVersion := v

	// Tables exist.
	for _, table := range []string{"miners", "miner_versions", "evidence_documents", "audit_log"} {
		var name string
		err := db.SQL().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q missing: %v", table, err)
		}
	}

	// Insert a row, then migrate again: must be a no-op that preserves data.
	if _, err := db.SQL().Exec("INSERT INTO miners (internal_id, created_at) VALUES ('M1', ?)", t0.Format(time.RFC3339)); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := db.Migrate(t0.Add(time.Hour)); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	var n int
	if err := db.SQL().QueryRow("SELECT COUNT(*) FROM miners").Scan(&n); err != nil || n != 1 {
		t.Errorf("miners count = %d, %v; want 1 (data preserved across re-migrate)", n, err)
	}
	if v, _ := db.SchemaVersion(); v != firstVersion {
		t.Errorf("version drifted to %d after no-op migrate (was %d)", v, firstVersion)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	db, _ := Open(filepath.Join(t.TempDir(), "e.db"))
	defer db.Close()
	if err := db.Migrate(t0); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// miner_versions.miner_id references miners(id); a dangling ref must fail.
	_, err := db.SQL().Exec("INSERT INTO miner_versions (miner_id, version, valid_from, created_at) VALUES (999, 1, ?, ?)",
		t0.Format(time.RFC3339), t0.Format(time.RFC3339))
	if err == nil {
		t.Error("foreign key violation was not rejected")
	}
}
