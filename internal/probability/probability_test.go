package probability

import (
	"math"
	"testing"
)

// Reference scenario: the two-miner setup at 13.37 TH/s against the network
// difficulty observed on 2026-08-23.
const (
	refHashrateHs = 13.37e12
	refDifficulty = 125_807_076_547_197.5
)

func closeTo(t *testing.T, got, want, relTol float64, label string) {
	t.Helper()
	if want == 0 {
		if got != 0 {
			t.Errorf("%s = %v, want 0", label, got)
		}
		return
	}
	if rel := math.Abs(got-want) / math.Abs(want); rel > relTol {
		t.Errorf("%s = %v, want %v (relative error %v > %v)", label, got, want, rel, relTol)
	}
}

func TestLambdaPerSecondMatchesHashesPerBlock(t *testing.T) {
	// lambda = H / (D * 2^32)
	want := refHashrateHs / (refDifficulty * 4294967296)
	closeTo(t, LambdaPerSecond(refHashrateHs, refDifficulty), want, 1e-12, "LambdaPerSecond")
}

func TestLambdaPerSecondReferenceValue(t *testing.T) {
	closeTo(t, LambdaPerSecond(refHashrateHs, refDifficulty), 2.47438e-11, 1e-4, "LambdaPerSecond")
}

func TestAtLeastOneReferenceWindows(t *testing.T) {
	// Hand-derived from lambda = 2.4743805e-11 and P = x - x^2/2 + x^3/6 ...
	// The second-order term only matters at the one-year scale, where it moves
	// the answer by about 0.04 percent.
	cases := []struct {
		name    string
		seconds float64
		want    float64
	}{
		{"one day", Day, 2.13786e-6},
		{"one week", Week, 1.49650e-5},
		{"thirty days", Month, 6.413388e-5},
		{"one year", Year, 7.805503e-4},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := AtLeastOne(refHashrateHs, refDifficulty, c.seconds)
			closeTo(t, got, c.want, 1e-4, "AtLeastOne")
		})
	}
}

// This is the test that forces Expm1. With a 1 H/s miner, lambda*T is around
// 1.6e-19, where math.Exp(-x) rounds to exactly 1.0 and a naive 1-Exp(-x)
// returns a hard zero.
func TestAtLeastOneStaysNonZeroForExtremelySmallProbabilities(t *testing.T) {
	const tinyHashrate = 1.0 // one hash per second

	naive := 1 - math.Exp(-LambdaPerSecond(tinyHashrate, refDifficulty)*Day)
	if naive != 0 {
		t.Fatalf("test premise broken: naive 1-Exp(-x) returned %v, expected hard zero", naive)
	}

	got := AtLeastOne(tinyHashrate, refDifficulty, Day)
	if got <= 0 {
		t.Fatalf("AtLeastOne = %v, want a positive value; the naive formula loses this entirely", got)
	}
	// For tiny x, P(>=1) is indistinguishable from the expectation lambda*T.
	closeTo(t, got, LambdaPerSecond(tinyHashrate, refDifficulty)*Day, 1e-9, "AtLeastOne")
}

func TestAtLeastOneIsMonotonicInTime(t *testing.T) {
	day := AtLeastOne(refHashrateHs, refDifficulty, Day)
	year := AtLeastOne(refHashrateHs, refDifficulty, Year)
	if !(year > day) {
		t.Errorf("P(year) = %v should exceed P(day) = %v", year, day)
	}
}

func TestAtLeastOneNeverExceedsOne(t *testing.T) {
	// A hashrate far above the whole network still cannot exceed certainty.
	got := AtLeastOne(1e30, refDifficulty, Year)
	if got > 1 {
		t.Errorf("AtLeastOne = %v, want <= 1", got)
	}
	if got < 0.999999 {
		t.Errorf("AtLeastOne = %v, want essentially 1 for an absurd hashrate", got)
	}
}

func TestAtLeastOneIsZeroWithoutHashrate(t *testing.T) {
	if got := AtLeastOne(0, refDifficulty, Year); got != 0 {
		t.Errorf("AtLeastOne with zero hashrate = %v, want 0", got)
	}
}

func TestInvalidInputsYieldZeroProbability(t *testing.T) {
	cases := []struct {
		name       string
		hashrate   float64
		difficulty float64
		seconds    float64
	}{
		{"zero difficulty", refHashrateHs, 0, Day},
		{"negative difficulty", refHashrateHs, -1, Day},
		{"negative hashrate", -1, refDifficulty, Day},
		{"negative window", refHashrateHs, refDifficulty, -1},
		{"NaN hashrate", math.NaN(), refDifficulty, Day},
		{"Inf difficulty", refHashrateHs, math.Inf(1), Day},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := AtLeastOne(c.hashrate, c.difficulty, c.seconds)
			if got != 0 {
				t.Errorf("AtLeastOne = %v, want 0", got)
			}
		})
	}
}

func TestExpectedBlocksIsLambdaTimesTime(t *testing.T) {
	want := LambdaPerSecond(refHashrateHs, refDifficulty) * Year
	closeTo(t, ExpectedBlocks(refHashrateHs, refDifficulty, Year), want, 1e-12, "ExpectedBlocks")
}

func TestMeanTimeToBlockSeconds(t *testing.T) {
	got, ok := MeanTimeToBlockSeconds(refHashrateHs, refDifficulty)
	if !ok {
		t.Fatal("MeanTimeToBlockSeconds returned ok=false for a valid scenario")
	}
	// 1/lambda = 4.04141e10 s, about 1280.5 years.
	closeTo(t, got/Year, 1280.5, 1e-3, "mean years")
}

// time.Duration tops out near 292 years, so solo waiting times must not be
// expressed as one.
func TestMeanTimeToBlockExceedsDurationRange(t *testing.T) {
	got, _ := MeanTimeToBlockSeconds(refHashrateHs, refDifficulty)
	const maxDurationSeconds = float64(math.MaxInt64) / 1e9
	if got <= maxDurationSeconds {
		t.Fatalf("test premise broken: %v s fits in a time.Duration", got)
	}
}

func TestMedianTimeIsLnTwoOfMean(t *testing.T) {
	mean, ok := MeanTimeToBlockSeconds(refHashrateHs, refDifficulty)
	if !ok {
		t.Fatal("mean not available")
	}
	median, ok := MedianTimeToBlockSeconds(refHashrateHs, refDifficulty)
	if !ok {
		t.Fatal("median not available")
	}
	closeTo(t, median, math.Ln2*mean, 1e-12, "median")
}

func TestTimeToBlockUndefinedWithoutHashrate(t *testing.T) {
	if _, ok := MeanTimeToBlockSeconds(0, refDifficulty); ok {
		t.Error("MeanTimeToBlockSeconds should report ok=false with zero hashrate")
	}
	if _, ok := MedianTimeToBlockSeconds(0, refDifficulty); ok {
		t.Error("MedianTimeToBlockSeconds should report ok=false with zero hashrate")
	}
	if _, ok := MeanTimeToBlockSeconds(refHashrateHs, 0); ok {
		t.Error("MeanTimeToBlockSeconds should report ok=false with zero difficulty")
	}
}

func TestShareOfNetwork(t *testing.T) {
	const networkHs = 907_782_986_431_433_900_000
	got, ok := ShareOfNetwork(refHashrateHs, networkHs)
	if !ok {
		t.Fatal("ShareOfNetwork returned ok=false for a valid scenario")
	}
	closeTo(t, got, refHashrateHs/networkHs, 1e-12, "ShareOfNetwork")
	// Around 1.5e-8, i.e. 0.0000015 percent.
	closeTo(t, got, 1.4728e-8, 1e-3, "ShareOfNetwork")
}

func TestShareOfNetworkUndefinedWithoutNetworkHashrate(t *testing.T) {
	if _, ok := ShareOfNetwork(refHashrateHs, 0); ok {
		t.Error("ShareOfNetwork should report ok=false when the network hashrate is zero")
	}
}
