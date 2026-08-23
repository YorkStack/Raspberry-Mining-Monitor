package collect

import (
	"testing"
	"time"
)

func TestFirstIntervalIsTheBase(t *testing.T) {
	b := NewBackoff(2*time.Second, time.Minute)
	if got := b.Interval(); got != 2*time.Second {
		t.Errorf("Interval = %v, want 2s before any failure", got)
	}
}

func TestIntervalDoublesOnEachFailure(t *testing.T) {
	b := NewBackoff(2*time.Second, time.Minute)

	want := []time.Duration{4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second}
	for i, w := range want {
		b.Failure()
		if got := b.Interval(); got != w {
			t.Errorf("after %d failures Interval = %v, want %v", i+1, got, w)
		}
	}
}

func TestIntervalIsCappedAtMax(t *testing.T) {
	b := NewBackoff(2*time.Second, time.Minute)
	for i := 0; i < 50; i++ {
		b.Failure()
	}
	if got := b.Interval(); got != time.Minute {
		t.Errorf("Interval = %v, want the 1m cap", got)
	}
}

func TestSuccessResetsToTheBase(t *testing.T) {
	b := NewBackoff(2*time.Second, time.Minute)
	b.Failure()
	b.Failure()
	b.Success()

	if got := b.Interval(); got != 2*time.Second {
		t.Errorf("Interval = %v, want the base restored after a success", got)
	}
}

// A very long base must not overflow into a negative duration after repeated
// doubling.
func TestLargeBaseDoesNotOverflow(t *testing.T) {
	b := NewBackoff(time.Hour, 2*time.Hour)
	for i := 0; i < 100; i++ {
		b.Failure()
		if got := b.Interval(); got <= 0 || got > 2*time.Hour {
			t.Fatalf("after %d failures Interval = %v, which is out of range", i+1, got)
		}
	}
}

func TestMaxBelowBaseFallsBackToBase(t *testing.T) {
	b := NewBackoff(30*time.Second, time.Second)
	b.Failure()
	if got := b.Interval(); got != 30*time.Second {
		t.Errorf("Interval = %v, want the base when max is smaller", got)
	}
}

func TestFailureCountIsReported(t *testing.T) {
	b := NewBackoff(time.Second, time.Minute)
	if b.Failures() != 0 {
		t.Errorf("Failures = %d, want 0", b.Failures())
	}
	b.Failure()
	b.Failure()
	if b.Failures() != 2 {
		t.Errorf("Failures = %d, want 2", b.Failures())
	}
	b.Success()
	if b.Failures() != 0 {
		t.Errorf("Failures = %d, want 0 after a success", b.Failures())
	}
}
