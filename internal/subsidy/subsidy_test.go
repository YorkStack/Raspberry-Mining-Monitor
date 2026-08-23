package subsidy

import "testing"

func TestSatsAtHalvingBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		height uint32
		want   uint64
	}{
		{"genesis", 0, 5_000_000_000},
		{"last block of first era", 209_999, 5_000_000_000},
		{"first halving", 210_000, 2_500_000_000},
		{"second halving", 420_000, 1_250_000_000},
		{"third halving", 630_000, 625_000_000},
		{"fourth halving", 840_000, 312_500_000},
		{"current tip era", 963_692, 312_500_000},
		{"fifth halving", 1_050_000, 156_250_000},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Sats(c.height); got != c.want {
				t.Errorf("Sats(%d) = %d, want %d", c.height, got, c.want)
			}
		})
	}
}

func TestSatsReachesZeroAtThirtyThirdHalving(t *testing.T) {
	// 50 BTC >> 33 rounds down to zero satoshis.
	if got := Sats(33 * 210_000); got != 0 {
		t.Errorf("Sats at 33rd halving = %d, want 0", got)
	}
}

func TestSatsDoesNotOverflowPastSixtyFourHalvings(t *testing.T) {
	// A naive shift by >= 64 is undefined-ish and can yield a nonzero result.
	if got := Sats(64 * 210_000); got != 0 {
		t.Errorf("Sats at 64th halving = %d, want 0", got)
	}
	if got := Sats(^uint32(0)); got != 0 {
		t.Errorf("Sats at max height = %d, want 0", got)
	}
}

func TestBTCConvertsFromSats(t *testing.T) {
	if got := BTC(963_692); got != 3.125 {
		t.Errorf("BTC(963692) = %v, want 3.125", got)
	}
	if got := BTC(0); got != 50 {
		t.Errorf("BTC(0) = %v, want 50", got)
	}
}

func TestNextHalvingHeight(t *testing.T) {
	cases := []struct {
		height uint32
		want   uint32
	}{
		{0, 210_000},
		{209_999, 210_000},
		{210_000, 420_000},
		{963_692, 1_050_000},
	}

	for _, c := range cases {
		if got := NextHalvingHeight(c.height); got != c.want {
			t.Errorf("NextHalvingHeight(%d) = %d, want %d", c.height, got, c.want)
		}
	}
}
