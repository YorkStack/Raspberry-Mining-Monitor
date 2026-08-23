package pool

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner"
)

var refTime = time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

// fakeProvider is a stand-in external provider for router tests.
type fakeProvider struct {
	name string
	caps Capabilities
	snap Snapshot
	err  error
}

func (f *fakeProvider) Name() string                { return f.name }
func (f *fakeProvider) Capabilities() Capabilities  { return f.caps }
func (f *fakeProvider) Fetch(_ context.Context, _ Input) (Snapshot, error) {
	if f.err != nil {
		return Snapshot{}, f.err
	}
	return f.snap, nil
}

func telem(name, stratum string, ths float64) miner.Snapshot {
	s := miner.Snapshot{Name: name, HashrateTHs: ths, PoolURL: stratum}
	s.Succeed(refTime)
	return s
}

func TestGenericDerivesStatsFromTelemetry(t *testing.T) {
	g := NewGeneric()
	acc, rej := uint64(10), uint64(1)
	bs, be := 2500.0, 9000.0
	tel := miner.Snapshot{
		Name: "Gamma", HashrateTHs: 1.2,
		SharesAccepted: &acc, SharesRejected: &rej,
		BestSessionDiff: &bs, BestDiff: &be,
	}
	tel.Succeed(refTime)

	snap, err := g.Fetch(context.Background(), Input{Miners: []Miner{
		{Name: "Gamma", Telemetry: tel, HasTelemetry: true},
	}})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.HashrateTHs == nil || *snap.HashrateTHs != 1.2 {
		t.Errorf("HashrateTHs = %v, want 1.2", snap.HashrateTHs)
	}
	if snap.AcceptedShares == nil || *snap.AcceptedShares != 10 {
		t.Errorf("AcceptedShares = %v, want 10", snap.AcceptedShares)
	}
	if snap.BestDifficulty == nil || *snap.BestDifficulty != 2500 {
		t.Errorf("BestDifficulty = %v, want 2500", snap.BestDifficulty)
	}
	if snap.BestEver == nil || *snap.BestEver != 9000 {
		t.Errorf("BestEver = %v, want 9000", snap.BestEver)
	}
	if snap.ActiveWorkers == nil || *snap.ActiveWorkers != 1 {
		t.Errorf("ActiveWorkers = %v, want 1", snap.ActiveWorkers)
	}
}

func TestRouterRoutesAndMerges(t *testing.T) {
	hr, best := 5.0, 12345.0
	pp := &fakeProvider{
		name: KeyPublicPool,
		caps: Caps(FieldHashrate, FieldBestShare, FieldBlocksFound),
		snap: Snapshot{
			Provider:       KeyPublicPool,
			Caps:           Caps(FieldHashrate, FieldBestShare, FieldBlocksFound),
			HashrateTHs:    &hr,
			BestDifficulty: &best,
			WorkersCount:   1,
			Workers:        []Worker{{Name: "octa", Provider: KeyPublicPool}},
		},
	}
	miners := []RouterMiner{
		{Name: "Octa", Address: "bc1qocta"}, // detects publicpool
		{Name: "Home", Address: ""},         // no address -> generic
	}
	r := NewRouter(miners, map[string]Provider{KeyPublicPool: pp})

	tel := map[string]miner.Snapshot{
		"Octa": telem("Octa", "stratum+tcp://public-pool.io:2018", 5.0),
		"Home": telem("Home", "my-node.local:3333", 1.0),
	}
	snap, err := r.Fetch(context.Background(), tel)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Provider != "mixed" {
		t.Errorf("Provider = %q, want mixed", snap.Provider)
	}
	if snap.HashrateTHs == nil || *snap.HashrateTHs != 6.0 {
		t.Errorf("HashrateTHs = %v, want 6", snap.HashrateTHs)
	}
	if snap.WorkersCount != 2 {
		t.Errorf("WorkersCount = %d, want 2", snap.WorkersCount)
	}
	if !snap.Caps.Has(FieldBlocksFound) || !snap.Caps.Has(FieldHashrate) {
		t.Error("merged caps missing a contributed field")
	}
}

func TestRouterFailsOnlyWhenEveryProviderFails(t *testing.T) {
	pp := &fakeProvider{name: KeyPublicPool, err: errors.New("down")}
	r := NewRouter([]RouterMiner{{Name: "Octa", Address: "bc1qocta", Override: KeyPublicPool}},
		map[string]Provider{KeyPublicPool: pp})

	_, err := r.Fetch(context.Background(), map[string]miner.Snapshot{
		"Octa": telem("Octa", "public-pool.io", 5.0),
	})
	if err == nil {
		t.Fatal("want error when the only provider fails")
	}
}
