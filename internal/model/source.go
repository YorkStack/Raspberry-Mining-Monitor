// Package model holds the primitives shared by every data source.
package model

import "time"

// staleIntervals is how many poll intervals a source may go without a
// successful fetch before the UI should treat it as stale.
const staleIntervals = 3

// Source tracks the freshness of one data source.
//
// A failed fetch never discards the last good data or its timestamp. The
// dashboard shows the last known values marked as ageing, which is more useful
// than a blank panel.
type Source struct {
	// FetchedAt is the time of the last successful fetch. Zero means nothing
	// has ever succeeded.
	FetchedAt time.Time `json:"fetchedAt"`
	// OK reports whether the most recent attempt succeeded.
	OK bool `json:"ok"`
	// Err is the reason the most recent attempt failed, empty when OK.
	Err string `json:"err,omitempty"`
}

// Succeed records a successful fetch and clears any previous error.
func (s *Source) Succeed(now time.Time) {
	s.FetchedAt = now
	s.OK = true
	s.Err = ""
}

// Fail records a failed attempt, leaving the last successful fetch time intact.
func (s *Source) Fail(_ time.Time, reason string) {
	s.OK = false
	s.Err = reason
}

// HasData reports whether the source has ever returned data.
func (s Source) HasData() bool { return !s.FetchedAt.IsZero() }

// Age is the time since the last successful fetch.
func (s Source) Age(now time.Time) time.Duration {
	if !s.HasData() {
		return 0
	}
	return now.Sub(s.FetchedAt)
}

// Stale reports whether the source has gone more than three poll intervals
// without a successful fetch. A source that has never succeeded is always
// stale.
func (s Source) Stale(now time.Time, interval time.Duration) bool {
	if !s.HasData() {
		return true
	}
	return s.Age(now) > staleIntervals*interval
}
