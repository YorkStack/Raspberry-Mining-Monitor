package configlog

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

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

func TestConfigChangeClosesPreviousAndPreservesHistory(t *testing.T) {
	s, log, db := setup(t)
	defer db.Close()
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)

	a := Config{MinerInternalID: "M1", OperatingMode: "SOLO", PoolEndpoint: "public-pool.io:2018", PayoutScheme: "SOLO"}
	changed, err := s.Record(a, "initial", "york", now)
	if err != nil || !changed {
		t.Fatalf("first record: changed=%v err=%v", changed, err)
	}
	// Same config again: no new record.
	if changed, _ := s.Record(a, "noop", "york", now.Add(time.Minute)); changed {
		t.Error("unchanged config created a new record")
	}
	// Change pool: new record, previous closed.
	b := a
	b.OperatingMode = "POOL"
	b.PoolEndpoint = "solo.ckpool.org:3333"
	if changed, _ := s.Record(b, "switched to ckpool", "york", now.Add(24*time.Hour)); !changed {
		t.Fatal("changed config not recorded")
	}
	hist, err := s.History("M1")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("history len = %d, want 2", len(hist))
	}
	if hist[0].ValidTo == nil {
		t.Error("previous config not closed (valid_to nil)")
	}
	if hist[1].ValidTo != nil || hist[1].OperatingMode != "POOL" {
		t.Errorf("current config wrong: %+v", hist[1])
	}
	if ok, _, _ := log.Verify(); !ok {
		t.Error("audit chain broken")
	}
}
