package model

import (
	"testing"
	"time"
)

var base = time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

func TestAgeIsTimeSinceFetch(t *testing.T) {
	s := Source{FetchedAt: base, OK: true}
	if got := s.Age(base.Add(7 * time.Second)); got != 7*time.Second {
		t.Errorf("Age = %v, want 7s", got)
	}
}

func TestNeverFetchedSourceIsStale(t *testing.T) {
	var s Source // zero value: never fetched
	if !s.Stale(base, time.Second) {
		t.Error("a source that was never fetched must be stale")
	}
}

func TestFreshWithinThreeIntervals(t *testing.T) {
	s := Source{FetchedAt: base, OK: true}
	if s.Stale(base.Add(2*time.Second), time.Second) {
		t.Error("2s old with a 1s interval should still be fresh")
	}
}

func TestStaleBeyondThreeIntervals(t *testing.T) {
	s := Source{FetchedAt: base, OK: true}
	if !s.Stale(base.Add(4*time.Second), time.Second) {
		t.Error("4s old with a 1s interval should be stale")
	}
}

// A failed fetch must not reset freshness. The last good data stays on screen
// but has to be marked as ageing.
func TestFailedSourceKeepsItsLastGoodTimestamp(t *testing.T) {
	s := Source{FetchedAt: base, OK: true}
	s.Fail(base.Add(time.Second), "dial tcp: connection refused")

	if s.OK {
		t.Error("OK = true, want false after Fail")
	}
	if s.Err != "dial tcp: connection refused" {
		t.Errorf("Err = %q, want the failure reason", s.Err)
	}
	if !s.FetchedAt.Equal(base) {
		t.Errorf("FetchedAt = %v, want the last successful fetch at %v", s.FetchedAt, base)
	}
	if got := s.Age(base.Add(10 * time.Second)); got != 10*time.Second {
		t.Errorf("Age = %v, want 10s measured from the last good fetch", got)
	}
}

func TestSucceedClearsTheError(t *testing.T) {
	var s Source
	s.Fail(base, "boom")
	s.Succeed(base.Add(time.Second))

	if !s.OK {
		t.Error("OK = false, want true after Succeed")
	}
	if s.Err != "" {
		t.Errorf("Err = %q, want empty after Succeed", s.Err)
	}
	if !s.FetchedAt.Equal(base.Add(time.Second)) {
		t.Errorf("FetchedAt = %v, want the new fetch time", s.FetchedAt)
	}
}

func TestFailBeforeAnySuccessLeavesNoFetchTime(t *testing.T) {
	var s Source
	s.Fail(base, "boom")

	if !s.FetchedAt.IsZero() {
		t.Errorf("FetchedAt = %v, want zero when nothing has ever succeeded", s.FetchedAt)
	}
	if !s.Stale(base, time.Second) {
		t.Error("a source that has never succeeded must be stale")
	}
	if s.HasData() {
		t.Error("HasData = true, want false when nothing has ever succeeded")
	}
}

func TestHasDataAfterFirstSuccess(t *testing.T) {
	var s Source
	s.Succeed(base)
	if !s.HasData() {
		t.Error("HasData = false, want true after a successful fetch")
	}
	s.Fail(base.Add(time.Second), "boom")
	if !s.HasData() {
		t.Error("HasData = false; a later failure must not discard the last good data")
	}
}
