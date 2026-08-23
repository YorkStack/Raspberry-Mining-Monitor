package collect

import "time"

// Backoff turns consecutive failures into a longer poll interval, so an
// unplugged miner or a rate-limited API is retried gently rather than hammered.
type Backoff struct {
	base     time.Duration
	max      time.Duration
	failures int
}

// NewBackoff creates a backoff that starts at base and doubles up to max.
// A max below base is treated as base.
func NewBackoff(base, max time.Duration) *Backoff {
	if max < base {
		max = base
	}
	return &Backoff{base: base, max: max}
}

// Failure records one consecutive failure.
func (b *Backoff) Failure() { b.failures++ }

// Success clears the failure streak.
func (b *Backoff) Success() { b.failures = 0 }

// Failures is the current consecutive-failure count.
func (b *Backoff) Failures() int { return b.failures }

// Interval is how long to wait before the next attempt.
func (b *Backoff) Interval() time.Duration {
	d := b.base
	for i := 0; i < b.failures; i++ {
		// Stop before doubling could overflow or exceed the cap.
		if d >= b.max/2 {
			return b.max
		}
		d *= 2
	}
	if d > b.max {
		return b.max
	}
	return d
}
