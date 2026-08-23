package mempool

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/bitcoin"
)

// mempoolMux serves the handful of endpoints the provider uses, with responses
// shaped like the real mempool.space REST API observed on 2026-08-23.
func mempoolMux() *http.ServeMux {
	mux := http.NewServeMux()

	// /api/blocks returns the most recent blocks, newest first.
	mux.HandleFunc("/api/blocks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"height":963692,"timestamp":1787467800,"difficulty":125807076547197.5},
			{"height":963691,"timestamp":1787467100,"difficulty":125807076547197.5}
		]`))
	})

	// /api/v1/mining/hashrate/3d carries the network hashrate and current difficulty.
	mux.HandleFunc("/api/v1/mining/hashrate/3d", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"currentHashrate":907782986431433900000,
			"currentDifficulty":125807076547197.5
		}`))
	})

	// /api/v1/difficulty-adjustment carries the retarget estimate.
	mux.HandleFunc("/api/v1/difficulty-adjustment", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"difficultyChange":-2.45,
			"remainingBlocks":1972,
			"remainingTime":1215308104,
			"nextRetargetHeight":965664
		}`))
	})

	// /api/v1/prices carries fiat exchange rates.
	mux.HandleFunc("/api/v1/prices", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"time":1787481005,"USD":76747,"EUR":65515,"GBP":56223}`))
	})

	return mux
}

func newProvider(base string) *Provider {
	return New(Config{BaseURL: base, Timeout: 2 * time.Second})
}

func TestSourceKindIsPublic(t *testing.T) {
	if got := newProvider("http://x").SourceKind(); got != bitcoin.SourcePublic {
		t.Errorf("SourceKind = %v, want public", got)
	}
}

func TestNetworkParsesCoreFields(t *testing.T) {
	srv := httptest.NewServer(mempoolMux())
	defer srv.Close()

	s, err := newProvider(srv.URL).Network(context.Background())
	if err != nil {
		t.Fatalf("Network: %v", err)
	}

	if s.Kind != bitcoin.SourcePublic {
		t.Errorf("Kind = %v, want public", s.Kind)
	}
	if !s.OK {
		t.Error("OK = false, want true")
	}
	if s.Height != 963692 {
		t.Errorf("Height = %d, want 963692", s.Height)
	}
	if math.Abs(s.Difficulty-125807076547197.5) > 1 {
		t.Errorf("Difficulty = %v", s.Difficulty)
	}
	if math.Abs(s.NetworkHashrateHs-907782986431433900000) > 1e15 {
		t.Errorf("NetworkHashrateHs = %v", s.NetworkHashrateHs)
	}
	wantBlock := time.Unix(1787467800, 0)
	if !s.LastBlockTime.Equal(wantBlock) {
		t.Errorf("LastBlockTime = %v, want %v", s.LastBlockTime, wantBlock)
	}
}

// Subsidy and next-halving are computed locally from the height, never fetched.
func TestSubsidyAndHalvingComputedFromHeight(t *testing.T) {
	srv := httptest.NewServer(mempoolMux())
	defer srv.Close()

	s, _ := newProvider(srv.URL).Network(context.Background())
	if s.SubsidyBTC != 3.125 {
		t.Errorf("SubsidyBTC = %v, want 3.125", s.SubsidyBTC)
	}
	if s.NextHalvingHeight != 1_050_000 {
		t.Errorf("NextHalvingHeight = %d, want 1050000", s.NextHalvingHeight)
	}
}

func TestRetargetFieldsParsed(t *testing.T) {
	srv := httptest.NewServer(mempoolMux())
	defer srv.Close()

	s, _ := newProvider(srv.URL).Network(context.Background())
	if math.Abs(s.NextRetargetChangePct-(-2.45)) > 1e-9 {
		t.Errorf("NextRetargetChangePct = %v, want -2.45", s.NextRetargetChangePct)
	}
	if s.NextRetargetHeight != 965664 {
		t.Errorf("NextRetargetHeight = %d, want 965664", s.NextRetargetHeight)
	}
	if math.Abs(s.NextRetargetETASeconds-1215308.104) > 1 {
		t.Errorf("NextRetargetETASeconds = %v, want ~1215308", s.NextRetargetETASeconds)
	}
}

// The core panel must still populate even if the slow-moving secondary calls
// fail, because /api/blocks alone carries height, time and difficulty.
func TestPartialFailureStillYieldsCoreData(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/blocks", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"height":963692,"timestamp":1787467800,"difficulty":125807076547197.5}]`))
	})
	mux.HandleFunc("/api/v1/mining/hashrate/3d", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream down", http.StatusBadGateway)
	})
	mux.HandleFunc("/api/v1/difficulty-adjustment", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream down", http.StatusBadGateway)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s, err := newProvider(srv.URL).Network(context.Background())
	if err != nil {
		t.Fatalf("Network should succeed on core data alone: %v", err)
	}
	if s.Height != 963692 {
		t.Errorf("Height = %d, want 963692", s.Height)
	}
	// Hashrate came from the failed call, so it is absent, not fabricated.
	if s.NetworkHashrateHs != 0 {
		t.Errorf("NetworkHashrateHs = %v, want 0 when its source failed", s.NetworkHashrateHs)
	}
	// Difficulty still comes from /api/blocks.
	if s.Difficulty == 0 {
		t.Error("Difficulty should come from /api/blocks even when the hashrate call fails")
	}
}

// If the one essential call fails, the whole fetch fails so the source is
// marked stale rather than showing an empty chain.
func TestBlocksFailureIsAnError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/blocks", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if _, err := newProvider(srv.URL).Network(context.Background()); err == nil {
		t.Error("a failing /api/blocks returned no error")
	}
}

func TestEmptyBlocksIsAnError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/blocks", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if _, err := newProvider(srv.URL).Network(context.Background()); err == nil {
		t.Error("an empty blocks array returned no error")
	}
}

func TestHonoursContextDeadline(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/blocks", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := newProvider(srv.URL).Network(ctx); err == nil {
		t.Error("slow response past the deadline returned no error")
	}
}

func TestBacksOffOn429(t *testing.T) {
	var calls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/blocks", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "slow down", http.StatusTooManyRequests)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := newProvider(srv.URL).Network(context.Background())
	if err == nil {
		t.Error("429 returned no error")
	}
	// One fetch must not hammer: a 429 is a single failed attempt, not a retry storm.
	if n := atomic.LoadInt32(&calls); n > 1 {
		t.Errorf("made %d calls to /api/blocks in one fetch, want 1", n)
	}
}

func TestMalformedJsonIsAnError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/blocks", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not an array`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if _, err := newProvider(srv.URL).Network(context.Background()); err == nil {
		t.Error("malformed JSON returned no error")
	}
}

func TestPriceEURParsed(t *testing.T) {
	srv := httptest.NewServer(mempoolMux())
	defer srv.Close()

	s, err := newProvider(srv.URL).Network(context.Background())
	if err != nil {
		t.Fatalf("Network: %v", err)
	}
	if math.Abs(s.PriceEUR-65515) > 1 {
		t.Errorf("PriceEUR = %v, want 65515", s.PriceEUR)
	}
}

// The price is a secondary call: if it fails, the rest of the panel still
// populates and the price is simply absent (zero), not fabricated.
func TestPriceFailureDoesNotFailFetch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/blocks", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"height":963692,"timestamp":1787467800,"difficulty":125807076547197.5}]`))
	})
	mux.HandleFunc("/api/v1/prices", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s, err := newProvider(srv.URL).Network(context.Background())
	if err != nil {
		t.Fatalf("Network should not fail when prices fail: %v", err)
	}
	if s.PriceEUR != 0 {
		t.Errorf("PriceEUR = %v, want 0 when its source failed", s.PriceEUR)
	}
}
