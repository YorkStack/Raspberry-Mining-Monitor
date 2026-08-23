package demo

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/bitcoin"
)

var start = time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

// clock returns a controllable time source.
func clock(t *time.Time) func() time.Time {
	return func() time.Time { return *t }
}

func nerdOctaxeConfig() MinerConfig {
	return MinerConfig{
		Name:         "NerdOctaxe",
		Model:        "BM1370",
		NominalTHs:   12.10,
		NominalW:     158,
		NominalTempC: 62,
		Fans:         1,
	}
}

func TestMinerHashrateStaysInAPlausibleBand(t *testing.T) {
	now := start
	m := NewMiner(nerdOctaxeConfig(), 1, clock(&now))

	var min, max float64 = math.Inf(1), math.Inf(-1)
	for i := 0; i < 2000; i++ {
		now = now.Add(2 * time.Second)
		s, err := m.Fetch(context.Background())
		if err != nil {
			continue
		}
		min = math.Min(min, s.HashrateTHs)
		max = math.Max(max, s.HashrateTHs)
	}

	if min < 12.10*0.75 || max > 12.10*1.25 {
		t.Errorf("hashrate ranged %v..%v, want within 25%% of 12.10", min, max)
	}
	if max-min < 0.05 {
		t.Errorf("hashrate barely moved (%v..%v); demo data should drift", min, max)
	}
}

func TestMinerTemperatureStaysInAPlausibleBand(t *testing.T) {
	now := start
	m := NewMiner(nerdOctaxeConfig(), 2, clock(&now))

	for i := 0; i < 2000; i++ {
		now = now.Add(2 * time.Second)
		s, err := m.Fetch(context.Background())
		if err != nil {
			continue
		}
		if s.ASICTempC == nil {
			t.Fatal("ASICTempC = nil, want a value")
		}
		if *s.ASICTempC < 40 || *s.ASICTempC > 90 {
			t.Fatalf("ASICTempC = %v, outside a plausible range", *s.ASICTempC)
		}
		if s.VRMTempC == nil || *s.VRMTempC <= *s.ASICTempC {
			t.Fatalf("VRM temp %v should sit above the ASIC temp %v", s.VRMTempC, *s.ASICTempC)
		}
	}
}

func TestMinerSharesNeverGoBackwards(t *testing.T) {
	now := start
	m := NewMiner(nerdOctaxeConfig(), 3, clock(&now))

	var prev uint64
	for i := 0; i < 500; i++ {
		now = now.Add(2 * time.Second)
		s, err := m.Fetch(context.Background())
		if err != nil {
			continue
		}
		if s.SharesAccepted == nil {
			t.Fatal("SharesAccepted = nil, want a value")
		}
		if *s.SharesAccepted < prev {
			t.Fatalf("accepted shares went backwards: %d then %d", prev, *s.SharesAccepted)
		}
		prev = *s.SharesAccepted
	}
	if prev == 0 {
		t.Error("no shares were accumulated over 500 polls")
	}
}

func TestMinerUptimeTracksElapsedTime(t *testing.T) {
	now := start
	m := NewMiner(nerdOctaxeConfig(), 4, clock(&now))

	now = now.Add(time.Hour)
	s, err := m.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if math.Abs(s.UptimeSeconds-3600) > 1 {
		t.Errorf("UptimeSeconds = %v, want ~3600", s.UptimeSeconds)
	}
}

func TestSameSeedProducesSameSequence(t *testing.T) {
	collect := func(seed int64) []float64 {
		now := start
		m := NewMiner(nerdOctaxeConfig(), seed, clock(&now))
		var out []float64
		for i := 0; i < 50; i++ {
			now = now.Add(2 * time.Second)
			s, err := m.Fetch(context.Background())
			if err != nil {
				out = append(out, -1)
				continue
			}
			out = append(out, s.HashrateTHs)
		}
		return out
	}

	a, b := collect(99), collect(99)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("sample %d differs between runs with the same seed: %v vs %v", i, a[i], b[i])
		}
	}
	if c := collect(100); c[10] == a[10] {
		t.Error("different seeds produced identical output")
	}
}

func TestDropoutsCanBeDisabled(t *testing.T) {
	now := start
	cfg := nerdOctaxeConfig()
	cfg.DropoutChance = 0
	m := NewMiner(cfg, 5, clock(&now))

	for i := 0; i < 1000; i++ {
		now = now.Add(2 * time.Second)
		if _, err := m.Fetch(context.Background()); err != nil {
			t.Fatalf("unexpected dropout at poll %d: %v", i, err)
		}
	}
}

func TestDropoutsHappenAndRecover(t *testing.T) {
	now := start
	cfg := nerdOctaxeConfig()
	cfg.DropoutChance = 0.05
	m := NewMiner(cfg, 6, clock(&now))

	var failures, successesAfterFailure int
	for i := 0; i < 2000; i++ {
		now = now.Add(2 * time.Second)
		_, err := m.Fetch(context.Background())
		if err != nil {
			failures++
			continue
		}
		if failures > 0 {
			successesAfterFailure++
		}
	}

	if failures == 0 {
		t.Error("no dropouts occurred over 2000 polls with a 5% chance")
	}
	if successesAfterFailure == 0 {
		t.Error("the miner never recovered after a dropout")
	}
}

func TestBitcoinProviderAdvancesHeightOverTime(t *testing.T) {
	now := start
	p := NewBitcoin(7, clock(&now))

	first, err := p.Network(context.Background())
	if err != nil {
		t.Fatalf("network: %v", err)
	}

	now = now.Add(6 * time.Hour)
	later, err := p.Network(context.Background())
	if err != nil {
		t.Fatalf("network: %v", err)
	}

	if later.Height <= first.Height {
		t.Errorf("height did not advance over 6 hours: %d then %d", first.Height, later.Height)
	}
	// Roughly one block every ten minutes, so about 36 blocks in six hours.
	if d := later.Height - first.Height; d < 10 || d > 90 {
		t.Errorf("height advanced by %d over 6 hours, which is not a plausible rate", d)
	}
}

func TestBitcoinProviderReportsDemoSource(t *testing.T) {
	now := start
	p := NewBitcoin(8, clock(&now))
	if p.SourceKind() != bitcoin.SourceDemo {
		t.Errorf("SourceKind = %v, want demo", p.SourceKind())
	}
	s, _ := p.Network(context.Background())
	if s.Kind != bitcoin.SourceDemo {
		t.Errorf("snapshot Kind = %v, want demo", s.Kind)
	}
}

func TestBitcoinProviderHasPlausibleNetworkNumbers(t *testing.T) {
	now := start
	p := NewBitcoin(9, clock(&now))
	s, err := p.Network(context.Background())
	if err != nil {
		t.Fatalf("network: %v", err)
	}
	if s.Difficulty < 1e14 || s.Difficulty > 1e15 {
		t.Errorf("Difficulty = %v, outside a plausible 2026 range", s.Difficulty)
	}
	if s.NetworkHashrateHs < 1e20 || s.NetworkHashrateHs > 1e22 {
		t.Errorf("NetworkHashrateHs = %v, outside a plausible 2026 range", s.NetworkHashrateHs)
	}
	if s.LastBlockTime.After(now) {
		t.Error("LastBlockTime is in the future")
	}
}

func TestPoolAdapterReportsPublicPoolCapabilities(t *testing.T) {
	now := start
	p := NewPool([]string{"NerdOctaxe", "Gamma 602"}, 10, clock(&now))

	caps := p.Capabilities()
	if caps.RejectedShares {
		t.Error("RejectedShares = true; Public Pool cannot report them, so demo must not either")
	}
	if caps.PoolDifficulty {
		t.Error("PoolDifficulty = true; Public Pool does not expose it")
	}

	s, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if s.WorkersCount != 2 {
		t.Errorf("WorkersCount = %d, want 2", s.WorkersCount)
	}
	if s.BestDifficulty == nil || *s.BestDifficulty <= 0 {
		t.Error("BestDifficulty missing")
	}
}

func TestPoolBestDifficultyNeverDecreases(t *testing.T) {
	now := start
	p := NewPool([]string{"NerdOctaxe"}, 11, clock(&now))

	var prev float64
	for i := 0; i < 500; i++ {
		now = now.Add(time.Minute)
		s, err := p.Fetch(context.Background())
		if err != nil {
			continue
		}
		if s.BestDifficulty == nil {
			t.Fatal("BestDifficulty = nil")
		}
		if *s.BestDifficulty < prev {
			t.Fatalf("best difficulty went backwards: %v then %v", prev, *s.BestDifficulty)
		}
		prev = *s.BestDifficulty
	}
}
