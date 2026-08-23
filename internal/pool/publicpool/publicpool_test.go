package publicpool

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/pool"
)

const (
	addrNerd  = "bc1qnerdnerdnerdnerdnerdnerdnerdnerdnerd42"
	addrGamma = "bc1qgammagammagammagammagammagammagammaxx"
)

// A 12 TH/s worker reports hashRate in H/s: 1.2105e13.
func poolMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/client/"+addrNerd, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"bestDifficulty": 124300000000,
			"workersCount": 1,
			"workers": [
				{"sessionId":"a1","name":"nerd","bestDifficulty":"124300000000.00","hashRate":12105600000000,"startTime":"2026-08-23T09:00:00.000Z","lastSeen":"2026-08-23T11:59:48.000Z"}
			]
		}`))
	})
	mux.HandleFunc("/api/client/"+addrGamma, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"bestDifficulty": 18700000000,
			"workersCount": 1,
			"workers": [
				{"sessionId":"b1","name":"gamma","bestDifficulty":"18700000000.00","hashRate":1265800000000,"startTime":"2026-08-23T09:00:00.000Z","lastSeen":"2026-08-23T11:59:50.000Z"}
			]
		}`))
	})
	mux.HandleFunc("/api/pool", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"totalHashRate":118000000000000000,"blockHeight":963692,"totalMiners":2410,"blocksFound":3,"fee":0}`))
	})
	return mux
}

func input() pool.Input {
	return pool.Input{Miners: []pool.Miner{
		{Name: "NerdOctaxe", Address: addrNerd},
		{Name: "Gamma 602", Address: addrGamma},
	}}
}

func newAdapter(base string) *Adapter {
	return New(Config{BaseURL: base, Timeout: 2 * time.Second})
}

func TestNameAndCapabilities(t *testing.T) {
	a := newAdapter("http://x")
	if a.Name() != "publicpool" {
		t.Errorf("Name = %q", a.Name())
	}
	caps := a.Capabilities()
	if caps.Has(pool.FieldRejectedShares) {
		t.Error("RejectedShares should be unavailable: Public Pool does not report it")
	}
	if caps.Has(pool.FieldPoolDifficulty) {
		t.Error("PoolDifficulty should be unavailable")
	}
	if !caps.Has(pool.FieldHashrate) || !caps.Has(pool.FieldBestShare) || !caps.Has(pool.FieldLastShare) {
		t.Error("expected hashrate, best_share and last_share to be supported")
	}
}

func TestFetchAggregatesAcrossAddresses(t *testing.T) {
	srv := httptest.NewServer(poolMux())
	defer srv.Close()

	s, err := newAdapter(srv.URL).Fetch(context.Background(), input())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if s.Provider != "publicpool" {
		t.Errorf("Provider = %q", s.Provider)
	}
	if s.WorkersCount != 2 {
		t.Errorf("WorkersCount = %d, want 2", s.WorkersCount)
	}
	if len(s.Workers) != 2 {
		t.Fatalf("Workers = %d, want 2", len(s.Workers))
	}
	// Best difficulty is the max across addresses, never a sum.
	if s.BestDifficulty == nil || math.Abs(*s.BestDifficulty-124300000000) > 1 {
		t.Errorf("BestDifficulty = %v, want the max 124.3e9", s.BestDifficulty)
	}
}

func TestWorkerHashrateConvertedFromHsToThs(t *testing.T) {
	srv := httptest.NewServer(poolMux())
	defer srv.Close()

	s, _ := newAdapter(srv.URL).Fetch(context.Background(), input())
	var nerd *pool.Worker
	for i := range s.Workers {
		if s.Workers[i].MinerName == "NerdOctaxe" {
			nerd = &s.Workers[i]
		}
	}
	if nerd == nil {
		t.Fatal("NerdOctaxe worker missing")
	}
	// 1.21056e13 H/s -> 12.1056 TH/s
	if nerd.HashrateTHs == nil || math.Abs(*nerd.HashrateTHs-12.1056) > 1e-4 {
		t.Errorf("HashrateTHs = %v, want 12.1056", nerd.HashrateTHs)
	}
}

func TestWorkersLabelledByMiner(t *testing.T) {
	srv := httptest.NewServer(poolMux())
	defer srv.Close()

	s, _ := newAdapter(srv.URL).Fetch(context.Background(), input())
	names := map[string]bool{}
	for _, w := range s.Workers {
		names[w.MinerName] = true
	}
	if !names["NerdOctaxe"] || !names["Gamma 602"] {
		t.Errorf("workers not labelled by miner: %v", names)
	}
}

func TestLastShareIsMostRecentWorker(t *testing.T) {
	srv := httptest.NewServer(poolMux())
	defer srv.Close()

	s, _ := newAdapter(srv.URL).Fetch(context.Background(), input())
	if s.LastShare == nil {
		t.Fatal("LastShare = nil")
	}
	want := time.Date(2026, 8, 23, 11, 59, 50, 0, time.UTC)
	if !s.LastShare.Equal(want) {
		t.Errorf("LastShare = %v, want the most recent %v", s.LastShare.UTC(), want)
	}
}

// A fresh address the pool has not seen yet returns 404. That is "awaiting
// first share", not an error, and must not blank the other miner.
func TestUnknownAddressIsNotAnError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/client/"+addrNerd, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"bestDifficulty":124300000000,"workersCount":1,"workers":[{"sessionId":"a1","name":"nerd","bestDifficulty":"1.00","hashRate":12105600000000,"lastSeen":"2026-08-23T11:59:48.000Z"}]}`))
	})
	mux.HandleFunc("/api/client/"+addrGamma, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // not yet known to the pool
	})
	mux.HandleFunc("/api/pool", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s, err := newAdapter(srv.URL).Fetch(context.Background(), input())
	if err != nil {
		t.Fatalf("Fetch should not fail when one address is unknown: %v", err)
	}
	if s.WorkersCount != 1 {
		t.Errorf("WorkersCount = %d, want 1 (the known miner)", s.WorkersCount)
	}
	if !s.OK {
		t.Error("OK = false, want true: one miner is reporting fine")
	}
}

// If every address fails at the network level, the fetch fails so the source
// goes stale rather than showing an empty pool.
func TestAllAddressesFailingIsAnError(t *testing.T) {
	c := New(Config{BaseURL: "http://127.0.0.1:1", Timeout: 300 * time.Millisecond})
	if _, err := c.Fetch(context.Background(), input()); err == nil {
		t.Error("all addresses unreachable returned no error")
	}
}

func TestPoolWideStatsParsed(t *testing.T) {
	srv := httptest.NewServer(poolMux())
	defer srv.Close()

	s, _ := newAdapter(srv.URL).Fetch(context.Background(), input())
	if s.PoolMiners == nil || *s.PoolMiners != 2410 {
		t.Errorf("PoolMiners = %v, want 2410", s.PoolMiners)
	}
	if s.BlocksFound == nil || *s.BlocksFound != 3 {
		t.Errorf("BlocksFound = %v, want 3", s.BlocksFound)
	}
}

// Even without /api/pool, per-address data must still populate.
func TestPoolEndpointFailureStillYieldsWorkers(t *testing.T) {
	base := poolMux()
	mux := http.NewServeMux()
	// Reuse the address handlers, but make /api/pool fail.
	mux.Handle("/api/client/"+addrNerd, base)
	mux.Handle("/api/client/"+addrGamma, base)
	mux.HandleFunc("/api/pool", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s, err := newAdapter(srv.URL).Fetch(context.Background(), input())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if s.WorkersCount != 2 {
		t.Errorf("WorkersCount = %d, want 2", s.WorkersCount)
	}
}

func TestContextDeadlineHonoured(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/client/"+addrNerd, func(w http.ResponseWriter, r *http.Request) { time.Sleep(2 * time.Second) })
	mux.HandleFunc("/api/client/"+addrGamma, func(w http.ResponseWriter, r *http.Request) { time.Sleep(2 * time.Second) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := newAdapter(srv.URL).Fetch(ctx, input()); err == nil {
		t.Error("slow pool past deadline returned no error")
	}
}

// Each address is one request per fetch: no retry storm.
func TestOneRequestPerAddressPerFetch(t *testing.T) {
	var nerdHits int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/client/"+addrNerd, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&nerdHits, 1)
		_, _ = w.Write([]byte(`{"bestDifficulty":1,"workersCount":0,"workers":[]}`))
	})
	mux.HandleFunc("/api/client/"+addrGamma, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"bestDifficulty":1,"workersCount":0,"workers":[]}`))
	})
	mux.HandleFunc("/api/pool", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, _ = newAdapter(srv.URL).Fetch(context.Background(), input())
	if n := atomic.LoadInt32(&nerdHits); n != 1 {
		t.Errorf("address queried %d times in one fetch, want 1", n)
	}
}
