package history

import (
	"path/filepath"
	"testing"
	"time"
)

var base = time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

func TestRecordsIntoOneHourRing(t *testing.T) {
	s := New("")
	// Append every 10s for 5 minutes.
	now := base
	for i := 0; i < 30; i++ {
		s.Record(now, Sample{Hashrate: float64(i), Power: 100, Price: 65000})
		now = now.Add(10 * time.Second)
	}
	pts := s.Query("1h")
	if len(pts) < 25 {
		t.Fatalf("1h ring has %d points, want ~30", len(pts))
	}
	if pts[len(pts)-1].Hashrate == 0 {
		t.Error("last point should carry the latest hashrate")
	}
}

// Each ring samples at its own cadence: within one minute the 24h ring (1 min)
// gets far fewer points than the 1h ring (10 s).
func TestRingsSampleAtDifferentCadence(t *testing.T) {
	s := New("")
	now := base
	for i := 0; i < 60; i++ { // 10 minutes at 10s
		s.Record(now, Sample{Hashrate: 10, Power: 100, Price: 65000})
		now = now.Add(10 * time.Second)
	}
	oneH := len(s.Query("1h"))
	dayH := len(s.Query("24h"))
	if oneH <= dayH {
		t.Errorf("1h points (%d) should exceed 24h points (%d) over 10 minutes", oneH, dayH)
	}
	if dayH < 8 || dayH > 12 {
		t.Errorf("24h ring got %d points over 10 min, want ~10", dayH)
	}
}

func TestRingCapsAtCapacity(t *testing.T) {
	s := New("")
	now := base
	// 2 hours at 10s = 720 samples; the 1h ring caps at 360.
	for i := 0; i < 720; i++ {
		s.Record(now, Sample{Hashrate: float64(i), Power: 100, Price: 65000})
		now = now.Add(10 * time.Second)
	}
	pts := s.Query("1h")
	if len(pts) > 360 {
		t.Errorf("1h ring has %d points, want <= 360", len(pts))
	}
	// Oldest should have been evicted: first point's hashrate is not 0.
	if pts[0].Hashrate == 0 {
		t.Error("oldest sample should have been evicted")
	}
	// Points must be in chronological order.
	for i := 1; i < len(pts); i++ {
		if pts[i].T < pts[i-1].T {
			t.Fatal("points out of order")
		}
	}
}

func TestUnknownRangeReturnsEmpty(t *testing.T) {
	s := New("")
	s.Record(base, Sample{Hashrate: 1})
	if len(s.Query("99y")) != 0 {
		t.Error("unknown range should return no points")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.gob")
	a := New(path)
	now := base
	for i := 0; i < 20; i++ {
		a.Record(now, Sample{Hashrate: float64(i), Power: 100, Price: 65000})
		now = now.Add(10 * time.Second)
	}
	if err := a.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	b := New(path)
	if err := b.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(b.Query("1h")) != len(a.Query("1h")) {
		t.Errorf("reloaded 1h points = %d, want %d", len(b.Query("1h")), len(a.Query("1h")))
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	b := New(filepath.Join(t.TempDir(), "nope.gob"))
	if err := b.Load(); err != nil {
		t.Errorf("missing history file should not error: %v", err)
	}
}
