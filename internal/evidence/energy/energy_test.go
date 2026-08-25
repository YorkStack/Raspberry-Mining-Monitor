package energy

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

func TestMeasuredAndEstimatedKeptSeparate(t *testing.T) {
	s, log, db := setup(t)
	defer db.Close()
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 31, 23, 59, 59, 0, time.UTC)

	if _, err := s.Record(Measurement{MinerInternalID: "M1", Start: from, End: from.Add(time.Hour),
		EnergyWh: 158, Measured: true, Source: "tasmota", EnergySource: SourceGrid, CompletenessPct: 100}, "york", from); err != nil {
		t.Fatalf("measured: %v", err)
	}
	if _, err := s.Record(Measurement{MinerInternalID: "M1", Start: from.Add(2 * time.Hour), End: from.Add(3 * time.Hour),
		EnergyWh: 160, Measured: false, EstimationMethod: "nominal power x time", EstimatedBy: "york",
		OriginalGap: "smart plug offline 12:00-13:00"}, "york", from); err != nil {
		t.Fatalf("estimated: %v", err)
	}
	measured, estimated, err := s.Totals("M1", from, to)
	if err != nil {
		t.Fatalf("Totals: %v", err)
	}
	if measured != 158 || estimated != 160 {
		t.Errorf("measured/estimated = %d/%d, want 158/160", measured, estimated)
	}
	if ok, _, _ := log.Verify(); !ok {
		t.Error("audit chain broken")
	}
}

func TestEstimateRequiresProvenance(t *testing.T) {
	s, _, db := setup(t)
	defer db.Close()
	_, err := s.Record(Measurement{MinerInternalID: "M1", Start: time.Now(), End: time.Now(),
		EnergyWh: 100, Measured: false}, "york", time.Now())
	if err == nil {
		t.Error("estimate without method/author was accepted")
	}
}
