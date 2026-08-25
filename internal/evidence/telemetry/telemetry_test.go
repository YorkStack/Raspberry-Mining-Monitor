package telemetry

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

func i64(v int64) *int64     { return &v }
func f64(v float64) *float64 { return &v }

func setup(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(time.Now()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestAggregateHourComputesStatsAndCompleteness(t *testing.T) {
	db := setup(t)
	defer db.Close()
	s := New(db.SQL(), time.Minute)
	hour := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)

	var samples []Sample
	for i := 0; i < 30; i++ { // 30 of an expected 60 samples => 50% complete
		samples = append(samples, Sample{
			MinerInternalID: "M1", TsUTC: hour.Add(time.Duration(i) * time.Minute), Online: true,
			HashrateHs: i64(12_000_000_000_000), PowerW: f64(158), ASICTempC: f64(62),
			AcceptedShares: i64(int64(100 + i)), RejectedShares: i64(2),
		})
	}
	if err := s.RecordRaw(samples...); err != nil {
		t.Fatalf("RecordRaw: %v", err)
	}
	h, err := s.AggregateHour("M1", hour, time.Now())
	if err != nil {
		t.Fatalf("AggregateHour: %v", err)
	}
	if h.ReceivedSamples != 30 || h.ExpectedSamples != 60 {
		t.Errorf("samples got %d/%d, want 30/60", h.ReceivedSamples, h.ExpectedSamples)
	}
	if h.CompletenessPct != 50 {
		t.Errorf("completeness = %v, want 50", h.CompletenessPct)
	}
	if h.AvgHashrateHs == nil || *h.AvgHashrateHs != 12_000_000_000_000 {
		t.Errorf("avg hashrate = %v", h.AvgHashrateHs)
	}
	if h.EnergyWh != 79 { // 30 * 158 W * 60s / 3600 = 79 Wh
		t.Errorf("energy = %d Wh, want 79", h.EnergyWh)
	}
	if h.OnlineMinutes != 30 || h.OfflineMinutes != 0 {
		t.Errorf("minutes online/offline = %d/%d, want 30/0", h.OnlineMinutes, h.OfflineMinutes)
	}
	if h.AcceptedDelta != 29 {
		t.Errorf("accepted delta = %d, want 29", h.AcceptedDelta)
	}
	if h.Gaps != "" {
		t.Errorf("no gaps expected for consecutive samples, got %q", h.Gaps)
	}
}

func TestMissingTelemetryShownAsGap(t *testing.T) {
	db := setup(t)
	defer db.Close()
	s := New(db.SQL(), time.Minute)
	hour := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)

	var samples []Sample
	for _, m := range []int{0, 1, 2, 3, 4, 10, 11, 12, 13, 14} { // gap between minute 4 and 10
		samples = append(samples, Sample{MinerInternalID: "M1", TsUTC: hour.Add(time.Duration(m) * time.Minute),
			Online: true, HashrateHs: i64(1)})
	}
	if err := s.RecordRaw(samples...); err != nil {
		t.Fatalf("RecordRaw: %v", err)
	}
	h, _ := s.AggregateHour("M1", hour, time.Now())
	if h.CompletenessPct >= 100 {
		t.Errorf("completeness = %v, want < 100 with a gap", h.CompletenessPct)
	}
	if h.Gaps == "" {
		t.Error("a gap must be recorded, never silently interpolated")
	}
}

func TestPruneRawKeepsAggregates(t *testing.T) {
	db := setup(t)
	defer db.Close()
	s := New(db.SQL(), time.Minute)
	old := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)

	_ = s.RecordRaw(
		Sample{MinerInternalID: "M1", TsUTC: old, Online: true, HashrateHs: i64(1)},
		Sample{MinerInternalID: "M1", TsUTC: recent, Online: true, HashrateHs: i64(1)},
	)
	cutoff := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	n, err := s.PruneRaw(cutoff)
	if err != nil {
		t.Fatalf("PruneRaw: %v", err)
	}
	if n != 1 {
		t.Errorf("pruned %d, want 1 (the old sample)", n)
	}
	if c, _ := s.RawCount(); c != 1 {
		t.Errorf("remaining raw = %d, want 1", c)
	}
}
