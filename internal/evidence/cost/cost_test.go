package cost

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

func TestAddAndSummary(t *testing.T) {
	s, log, db := setup(t)
	defer db.Close()
	now := time.Now()
	id1, err := s.Add(Cost{Date: "2026-08-05", Description: "NerdOctaxe", Category: "hardware",
		GrossCents: 29900, NetCents: i64(25126), VatCents: i64(4774), ReportingPeriod: "2026-08"}, "york", now)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if g, _ := s.GetGross(id1); g != 29900 {
		t.Errorf("gross = %d, want 29900 (integer cents)", g)
	}
	_, _ = s.Add(Cost{Date: "2026-08-10", Description: "PSU", Category: "hardware", GrossCents: 3500, ReportingPeriod: "2026-08"}, "york", now)
	_, _ = s.Add(Cost{Date: "2026-08-20", Description: "Electricity", Category: "electricity", GrossCents: 1200, ReportingPeriod: "2026-08"}, "york", now)

	sum, err := s.Summary("2026-08")
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	byCat := map[string]int64{}
	for _, c := range sum {
		byCat[c.Category] = c.TotalGrossCents
	}
	if byCat["hardware"] != 29900+3500 {
		t.Errorf("hardware total = %d, want 33400", byCat["hardware"])
	}
	if byCat["electricity"] != 1200 {
		t.Errorf("electricity total = %d, want 1200", byCat["electricity"])
	}
	if ok, _, _ := log.Verify(); !ok {
		t.Error("audit chain broken")
	}
}

func TestAdjustmentPreservesOriginal(t *testing.T) {
	s, _, db := setup(t)
	defer db.Close()
	now := time.Now()
	id, _ := s.Add(Cost{Date: "2026-08-05", Description: "x", Category: "hardware", GrossCents: 29900, ReportingPeriod: "2026-08"}, "york", now)

	if _, err := s.Adjust(id, Adjustment{Reason: ""}, "adviser", now); err == nil {
		t.Error("adjustment without reason accepted")
	}
	if _, err := s.Adjust(id, Adjustment{AllocationPct: func() *float64 { v := 80.0; return &v }(), Reason: "80% mining use"}, "adviser", now); err != nil {
		t.Fatalf("Adjust: %v", err)
	}
	// Original cost is unchanged.
	if g, _ := s.GetGross(id); g != 29900 {
		t.Errorf("original changed: %d", g)
	}
	var n int
	db.SQL().QueryRow("SELECT COUNT(*) FROM cost_adjustments WHERE cost_id = ?", id).Scan(&n)
	if n != 1 {
		t.Errorf("adjustment records = %d, want 1", n)
	}
}
