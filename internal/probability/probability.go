// Package probability computes solo-mining block probabilities.
//
// Finding a block is a Poisson process. A miner running at H hashes per second
// against network difficulty D expects to need D*2^32 hashes per block, so
// blocks arrive at a rate of lambda = H / (D * 2^32) per second.
//
// Every result here is a probability, never a prediction, and the process is
// memoryless: time already spent without a block does not change the odds.
package probability

import "math"

// Time windows in seconds. Seconds are used rather than time.Duration because
// solo waiting times routinely exceed the ~292-year range of a time.Duration.
const (
	Day   = 86400.0
	Week  = 7 * Day
	Month = 30 * Day
	Year  = 365.25 * Day
)

// hashesPerBlock is the expected number of hashes needed to find a block at a
// given difficulty: D * 2^32.
const twoPow32 = 4294967296.0

func valid(hashrateHs, difficulty float64) bool {
	if math.IsNaN(hashrateHs) || math.IsNaN(difficulty) {
		return false
	}
	if math.IsInf(hashrateHs, 0) || math.IsInf(difficulty, 0) {
		return false
	}
	return hashrateHs > 0 && difficulty > 0
}

// LambdaPerSecond returns the expected number of blocks found per second.
// It returns 0 for inputs that do not describe a mining scenario.
func LambdaPerSecond(hashrateHs, difficulty float64) float64 {
	if !valid(hashrateHs, difficulty) {
		return 0
	}
	return hashrateHs / (difficulty * twoPow32)
}

// ExpectedBlocks returns the expected number of blocks found within the given
// window. It is an expectation, not a probability, and may exceed 1.
func ExpectedBlocks(hashrateHs, difficulty, seconds float64) float64 {
	if seconds <= 0 || math.IsNaN(seconds) {
		return 0
	}
	return LambdaPerSecond(hashrateHs, difficulty) * seconds
}

// AtLeastOne returns the probability of finding at least one block within the
// given window: 1 - e^(-lambda*t).
//
// Expm1 is used rather than 1-Exp because lambda*t is typically around 1e-6 or
// far smaller, where the naive subtraction cancels away every significant digit
// and can return a hard zero.
func AtLeastOne(hashrateHs, difficulty, seconds float64) float64 {
	x := ExpectedBlocks(hashrateHs, difficulty, seconds)
	if x <= 0 {
		return 0
	}
	return -math.Expm1(-x)
}

// MeanTimeToBlockSeconds returns the mean waiting time until the next block,
// in seconds. The second return value is false when the scenario is undefined.
func MeanTimeToBlockSeconds(hashrateHs, difficulty float64) (float64, bool) {
	lambda := LambdaPerSecond(hashrateHs, difficulty)
	if lambda <= 0 {
		return 0, false
	}
	return 1 / lambda, true
}

// MedianTimeToBlockSeconds returns the median waiting time until the next
// block, in seconds. For an exponential distribution this is ln(2) times the
// mean, and it is the more honest headline figure of the two.
func MedianTimeToBlockSeconds(hashrateHs, difficulty float64) (float64, bool) {
	mean, ok := MeanTimeToBlockSeconds(hashrateHs, difficulty)
	if !ok {
		return 0, false
	}
	return math.Ln2 * mean, true
}

// ShareOfNetwork returns the miner's fraction of total network hashrate.
func ShareOfNetwork(hashrateHs, networkHashrateHs float64) (float64, bool) {
	if !valid(hashrateHs, networkHashrateHs) {
		return 0, false
	}
	return hashrateHs / networkHashrateHs, true
}
