package pool

import (
	"context"
	"time"
)

// Generic is the fallback provider for miners on a pool with no dedicated
// statistics API. It derives everything from the miner's own telemetry, which
// the miner collector already gathers, so it makes no network request of its
// own.
type Generic struct{}

// NewGeneric returns the telemetry-only provider.
func NewGeneric() *Generic { return &Generic{} }

// Name identifies the provider.
func (g *Generic) Name() string { return KeyGeneric }

// Capabilities are what a miner reports about its own pool activity.
func (g *Generic) Capabilities() Capabilities {
	return Caps(
		FieldHashrate,
		FieldAcceptedShares,
		FieldRejectedShares,
		FieldBestShare,
		FieldBestEver,
		FieldActiveWorkers,
	)
}

// Fetch folds the miners' own reported figures into a pool snapshot. It never
// fails: the data is already in hand.
func (g *Generic) Fetch(_ context.Context, in Input) (Snapshot, error) {
	snap := Snapshot{Provider: KeyGeneric, Caps: g.Capabilities()}

	var (
		hashrate      float64
		accepted      uint64
		rejected      uint64
		bestSession   float64
		bestEver      float64
		haveHashrate  bool
		haveAccepted  bool
		haveRejected  bool
		haveSession   bool
		haveEver      bool
		activeWorkers int
	)

	for _, m := range in.Miners {
		if !m.HasTelemetry || !m.Telemetry.HasData() {
			continue
		}
		t := m.Telemetry
		activeWorkers++

		hashrate += t.HashrateTHs
		haveHashrate = true

		if t.SharesAccepted != nil {
			accepted += *t.SharesAccepted
			haveAccepted = true
		}
		if t.SharesRejected != nil {
			rejected += *t.SharesRejected
			haveRejected = true
		}
		if t.BestSessionDiff != nil && (!haveSession || *t.BestSessionDiff > bestSession) {
			bestSession = *t.BestSessionDiff
			haveSession = true
		}
		if t.BestDiff != nil && (!haveEver || *t.BestDiff > bestEver) {
			bestEver = *t.BestDiff
			haveEver = true
		}

		w := Worker{Name: m.Name, MinerName: m.Name, Provider: KeyGeneric}
		ths := t.HashrateTHs
		w.HashrateTHs = &ths
		if t.BestSessionDiff != nil {
			bd := *t.BestSessionDiff
			w.BestDifficulty = &bd
		}
		snap.Workers = append(snap.Workers, w)
	}

	snap.WorkersCount = activeWorkers
	snap.ActiveWorkers = &activeWorkers
	if haveHashrate {
		snap.HashrateTHs = &hashrate
	}
	if haveAccepted {
		snap.AcceptedShares = &accepted
	}
	if haveRejected {
		snap.RejectedShares = &rejected
	}
	if haveSession {
		snap.BestDifficulty = &bestSession
	}
	if haveEver {
		snap.BestEver = &bestEver
	}

	snap.Succeed(time.Now())
	return snap, nil
}
