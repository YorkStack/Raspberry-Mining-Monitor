package aggregate

import (
	"math"
	"testing"
)

func f(v float64) *float64 { return &v }

func closeTo(t *testing.T, got, want, tol float64, label string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want %v (tolerance %v)", label, got, want, tol)
	}
}

func referenceFleet() []MinerInput {
	return []MinerInput{
		{Name: "NerdOctaxe", OK: true, HashrateTHs: 12.10, PowerW: f(158)},
		{Name: "Gamma 602", OK: true, HashrateTHs: 1.27, PowerW: f(18)},
	}
}

func TestCombineSumsHashrateAndPower(t *testing.T) {
	got := Combine(referenceFleet())
	closeTo(t, got.HashrateTHs, 13.37, 1e-9, "HashrateTHs")
	closeTo(t, got.PowerW, 176, 1e-9, "PowerW")
}

func TestCombineUsesTotalPowerOverTotalHashrate(t *testing.T) {
	got := Combine(referenceFleet())
	if !got.HasEfficiency {
		t.Fatal("HasEfficiency = false, want true")
	}
	closeTo(t, got.EfficiencyJTH, 176.0/13.37, 1e-9, "EfficiencyJTH")
	closeTo(t, got.EfficiencyJTH, 13.164, 1e-3, "EfficiencyJTH")
}

// The weighted figure must not be the arithmetic mean of the per-miner values.
// For this fleet the mean is about 13.62 and the correct answer about 13.16.
func TestCombineEfficiencyIsNotTheMeanOfPerMinerEfficiencies(t *testing.T) {
	got := Combine(referenceFleet())
	mean := (158.0/12.10 + 18.0/1.27) / 2
	if math.Abs(got.EfficiencyJTH-mean) < 0.1 {
		t.Errorf("EfficiencyJTH = %v is suspiciously close to the per-miner mean %v", got.EfficiencyJTH, mean)
	}
	closeTo(t, mean, 13.615, 1e-3, "per-miner mean (test premise)")
}

func TestCombineExcludesOfflineMinersFromTotals(t *testing.T) {
	fleet := referenceFleet()
	fleet[0].OK = false

	got := Combine(fleet)
	closeTo(t, got.HashrateTHs, 1.27, 1e-9, "HashrateTHs")
	closeTo(t, got.PowerW, 18, 1e-9, "PowerW")
	if got.MinersOnline != 1 {
		t.Errorf("MinersOnline = %d, want 1", got.MinersOnline)
	}
	if got.MinersTotal != 2 {
		t.Errorf("MinersTotal = %d, want 2", got.MinersTotal)
	}
}

func TestCombineFlagsIncompletePowerWhenAMinerDoesNotReportIt(t *testing.T) {
	fleet := referenceFleet()
	fleet[1].PowerW = nil

	got := Combine(fleet)
	if got.PowerComplete {
		t.Error("PowerComplete = true, want false when an online miner reports no power")
	}
	closeTo(t, got.PowerW, 158, 1e-9, "PowerW")
	if got.HasEfficiency {
		t.Error("HasEfficiency = true, want false when the power total is incomplete")
	}
}

func TestCombineReportsCompletePowerForTheReferenceFleet(t *testing.T) {
	if got := Combine(referenceFleet()); !got.PowerComplete {
		t.Error("PowerComplete = false, want true")
	}
}

func TestCombineWithNoMinersOnline(t *testing.T) {
	fleet := referenceFleet()
	fleet[0].OK = false
	fleet[1].OK = false

	got := Combine(fleet)
	closeTo(t, got.HashrateTHs, 0, 1e-9, "HashrateTHs")
	if got.HasEfficiency {
		t.Error("HasEfficiency = true, want false with no hashrate")
	}
	if got.MinersOnline != 0 {
		t.Errorf("MinersOnline = %d, want 0", got.MinersOnline)
	}
}

func TestCombineWithEmptyFleet(t *testing.T) {
	got := Combine(nil)
	if got.MinersTotal != 0 || got.MinersOnline != 0 {
		t.Errorf("empty fleet gave %+v", got)
	}
	if got.HasEfficiency {
		t.Error("HasEfficiency = true, want false for an empty fleet")
	}
}

func TestEfficiencyJTH(t *testing.T) {
	got, ok := EfficiencyJTH(158, 12.10)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	closeTo(t, got, 13.058, 1e-3, "EfficiencyJTH")
}

func TestEfficiencyUndefinedAtZeroHashrate(t *testing.T) {
	if _, ok := EfficiencyJTH(158, 0); ok {
		t.Error("ok = true, want false at zero hashrate")
	}
	if _, ok := EfficiencyJTH(158, -1); ok {
		t.Error("ok = true, want false at negative hashrate")
	}
}

func TestAcceptanceRatio(t *testing.T) {
	got, ok := AcceptanceRatio(1284, 2)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	closeTo(t, got, 1284.0/1286.0, 1e-12, "AcceptanceRatio")
}

func TestAcceptanceRatioIsOneWithNoRejections(t *testing.T) {
	got, ok := AcceptanceRatio(500, 0)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	closeTo(t, got, 1, 1e-12, "AcceptanceRatio")
}

func TestAcceptanceRatioUndefinedBeforeAnyShares(t *testing.T) {
	if _, ok := AcceptanceRatio(0, 0); ok {
		t.Error("ok = true, want false before any shares have been submitted")
	}
}
