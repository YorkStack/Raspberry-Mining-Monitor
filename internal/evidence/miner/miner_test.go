package miner

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

func i64(v int64) *int64 { return &v }

func setup(t *testing.T) (*Store, *audit.Log, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(time.Now()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	log := audit.New(db.SQL(), time.UTC)
	return New(db.SQL(), log), log, db
}

func TestCreateMakesVersionOne(t *testing.T) {
	s, log, db := setup(t)
	defer db.Close()
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)

	_, err := s.Create("NERD-01", Master{
		Manufacturer: "BitAxe", Model: "NerdOctaxe", SerialNumber: "SN123",
		PurchasePriceCents: i64(29900), NominalHashrateHs: i64(12_000_000_000_000), NominalPowerW: i64(158),
	}, "york", now)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	cur, err := s.Current("NERD-01")
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if cur.Version != 1 || cur.Model != "NerdOctaxe" || cur.SupersededAt != nil {
		t.Errorf("current = %+v", cur)
	}
	if cur.PurchasePriceCents == nil || *cur.PurchasePriceCents != 29900 {
		t.Errorf("price = %v, want 29900", cur.PurchasePriceCents)
	}
	if n, _ := log.Count(); n != 1 {
		t.Errorf("audit events = %d, want 1", n)
	}
}

func TestUpdatePreservesPreviousVersion(t *testing.T) {
	s, log, db := setup(t)
	defer db.Close()
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)

	if _, err := s.Create("NERD-01", Master{Model: "NerdOctaxe", FirmwareVersion: "1.0"}, "york", now); err != nil {
		t.Fatalf("Create: %v", err)
	}
	later := now.Add(24 * time.Hour)
	if err := s.Update("NERD-01", Master{Model: "NerdOctaxe", FirmwareVersion: "1.1"}, "york", "firmware upgrade", later); err != nil {
		t.Fatalf("Update: %v", err)
	}

	cur, _ := s.Current("NERD-01")
	if cur.Version != 2 || cur.FirmwareVersion != "1.1" {
		t.Errorf("current = v%d fw=%q, want v2 fw=1.1", cur.Version, cur.FirmwareVersion)
	}
	hist, err := s.History("NERD-01")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("history len = %d, want 2", len(hist))
	}
	if hist[0].Version != 1 || hist[0].FirmwareVersion != "1.0" || hist[0].SupersededAt == nil {
		t.Errorf("v1 not preserved/superseded: %+v", hist[0])
	}
	// Two events: create + update. Chain must verify.
	if n, _ := log.Count(); n != 2 {
		t.Errorf("audit events = %d, want 2", n)
	}
	if ok, _, _ := log.Verify(); !ok {
		t.Error("audit chain broken after miner operations")
	}
}

func TestDuplicateInternalIDRejected(t *testing.T) {
	s, _, db := setup(t)
	defer db.Close()
	now := time.Now()
	if _, err := s.Create("DUP", Master{Model: "x"}, "york", now); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := s.Create("DUP", Master{Model: "y"}, "york", now); err == nil {
		t.Error("duplicate internal_id was accepted")
	}
}
