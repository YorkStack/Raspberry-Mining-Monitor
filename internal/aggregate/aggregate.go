// Package aggregate combines per-miner telemetry into fleet totals.
package aggregate

// MinerInput is the subset of a miner snapshot that the totals depend on.
// PowerW is a pointer because a miner that does not report power must not be
// treated as drawing zero watts.
type MinerInput struct {
	Name        string
	OK          bool
	HashrateTHs float64
	PowerW      *float64
}

// Totals describes the combined state of the fleet.
type Totals struct {
	HashrateTHs float64
	PowerW      float64

	// EfficiencyJTH is total power over total hashrate, not the mean of the
	// per-miner figures. HasEfficiency is false when it cannot be computed.
	EfficiencyJTH float64
	HasEfficiency bool

	// PowerComplete is false when at least one online miner reported no power,
	// which makes PowerW a partial sum.
	PowerComplete bool

	MinersOnline int
	MinersTotal  int
}

// Combine sums the online miners into fleet totals. Offline miners are counted
// in MinersTotal but contribute nothing to the sums.
func Combine(miners []MinerInput) Totals {
	t := Totals{MinersTotal: len(miners), PowerComplete: true}

	for _, m := range miners {
		if !m.OK {
			continue
		}
		t.MinersOnline++
		t.HashrateTHs += m.HashrateTHs
		if m.PowerW == nil {
			t.PowerComplete = false
			continue
		}
		t.PowerW += *m.PowerW
	}

	if t.PowerComplete {
		if eff, ok := EfficiencyJTH(t.PowerW, t.HashrateTHs); ok {
			t.EfficiencyJTH = eff
			t.HasEfficiency = true
		}
	}

	return t
}

// EfficiencyJTH returns joules per terahash. The second return value is false
// when there is no hashrate to divide by.
func EfficiencyJTH(powerW, hashrateTHs float64) (float64, bool) {
	if hashrateTHs <= 0 {
		return 0, false
	}
	return powerW / hashrateTHs, true
}

// AcceptanceRatio returns accepted shares as a fraction of all submitted
// shares. The second return value is false before any share has been submitted.
func AcceptanceRatio(accepted, rejected uint64) (float64, bool) {
	total := accepted + rejected
	if total == 0 {
		return 0, false
	}
	return float64(accepted) / float64(total), true
}
