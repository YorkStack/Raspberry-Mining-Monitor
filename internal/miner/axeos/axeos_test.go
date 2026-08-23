package axeos

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

func approx(t *testing.T, got *float64, want float64, label string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %v", label, want)
	}
	if math.Abs(*got-want) > 0.01 {
		t.Errorf("%s = %v, want %v", label, *got, want)
	}
}

// upstreamServer serves the Bitaxe firmware shape: no /api/v2/dashboard.
func upstreamServer(t *testing.T) *httptest.Server {
	info := fixture(t, "upstream_info.json")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/dashboard", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // upstream has no v2 endpoint
	})
	mux.HandleFunc("/api/system/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(info)
	})
	return httptest.NewServer(mux)
}

// nerdqaxeServer serves the fork shape: v2 dashboard plus a legacy info.
func nerdqaxeServer(t *testing.T) *httptest.Server {
	dash := fixture(t, "nerdqaxe_dashboard.json")
	info := fixture(t, "nerdqaxe_info.json")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/dashboard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(dash)
	})
	mux.HandleFunc("/api/system/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(info)
	})
	return httptest.NewServer(mux)
}

func newClient(name, base string) *Client {
	return New(Config{Name: name, BaseURL: base, Timeout: 2 * time.Second})
}

func TestUpstreamParsedAndNormalised(t *testing.T) {
	srv := upstreamServer(t)
	defer srv.Close()

	s, err := newClient("Gamma 602", srv.URL).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if s.Variant != miner.VariantUpstream {
		t.Errorf("Variant = %q, want upstream", s.Variant)
	}
	if !s.OK {
		t.Error("OK = false, want true")
	}
	// hashRate 1265.8 GH/s -> 1.2658 TH/s
	if math.Abs(s.HashrateTHs-1.2658) > 1e-4 {
		t.Errorf("HashrateTHs = %v, want 1.2658", s.HashrateTHs)
	}
	approx(t, s.PowerW, 17.9, "PowerW")
	// voltage 5023.4 mV -> 5.0234 V
	approx(t, s.VoltageV, 5.0234, "VoltageV")
	// current 3400 mA -> 3.4 A
	approx(t, s.CurrentA, 3.4, "CurrentA")
	// coreVoltage 1150 mV -> 1.15 V
	approx(t, s.CoreVoltageV, 1.15, "CoreVoltageV")
	approx(t, s.ASICTempC, 55.2, "ASICTempC")
	approx(t, s.VRMTempC, 63, "VRMTempC")
	approx(t, s.FreqMHz, 600, "FreqMHz")
	if s.Model != "BM1370" {
		t.Errorf("Model = %q, want BM1370", s.Model)
	}
	if s.SharesAccepted == nil || *s.SharesAccepted != 1284 {
		t.Errorf("SharesAccepted = %v, want 1284", s.SharesAccepted)
	}
	if s.SharesRejected == nil || *s.SharesRejected != 2 {
		t.Errorf("SharesRejected = %v, want 2", s.SharesRejected)
	}
	approx(t, s.BestDiff, 18700000000, "BestDiff")
	if len(s.Fans) != 1 {
		t.Fatalf("Fans = %d, want a single scalar fan", len(s.Fans))
	}
	approx(t, s.Fans[0].RPM, 4680, "fan rpm")
	if s.PoolUser != "bc1qgammagammagammagammagammagammagammaxx.gamma" {
		t.Errorf("PoolUser = %q", s.PoolUser)
	}
	if s.UsingFallback == nil || *s.UsingFallback {
		t.Errorf("UsingFallback = %v, want false", s.UsingFallback)
	}
	if s.UptimeSeconds != 43201 {
		t.Errorf("UptimeSeconds = %v, want 43201", s.UptimeSeconds)
	}
}

func TestNerdQAxeParsedAndNormalised(t *testing.T) {
	srv := nerdqaxeServer(t)
	defer srv.Close()

	s, err := newClient("NerdOctaxe", srv.URL).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if s.Variant != miner.VariantNerdQAxe {
		t.Errorf("Variant = %q, want nerdqaxe", s.Variant)
	}
	// hashRate 12105.6 GH/s -> 12.1056 TH/s
	if math.Abs(s.HashrateTHs-12.1056) > 1e-4 {
		t.Errorf("HashrateTHs = %v, want 12.1056", s.HashrateTHs)
	}
	approx(t, s.PowerW, 158.4, "PowerW")
	// v2 reports volts and amps directly, no conversion
	approx(t, s.VoltageV, 5.02, "VoltageV")
	approx(t, s.CurrentA, 31.6, "CurrentA")
	approx(t, s.CoreVoltageV, 1.19, "CoreVoltageV")
	// asicTemp is the reported max across chips
	approx(t, s.ASICTempC, 62.4, "ASICTempC")
	approx(t, s.VRMTempC, 70.1, "VRMTempC")
	approx(t, s.FreqMHz, 625, "FreqMHz")
	if s.SharesAccepted == nil || *s.SharesAccepted != 5821 {
		t.Errorf("SharesAccepted = %v, want 5821", s.SharesAccepted)
	}
	approx(t, s.BestDiff, 124300000000, "BestDiff")
	// The six-phase board reports two fans as an array.
	if len(s.Fans) != 2 {
		t.Fatalf("Fans = %d, want 2", len(s.Fans))
	}
	approx(t, s.Fans[0].RPM, 4806, "fan0 rpm")
	approx(t, s.Fans[1].RPM, 4741, "fan1 rpm")
	if s.PoolUser != "bc1qnerdnerdnerdnerdnerdnerdnerdnerdnerd42.nerd" {
		t.Errorf("PoolUser = %q", s.PoolUser)
	}
	// Model and firmware come from the legacy info endpoint.
	if s.Model != "BM1370" {
		t.Errorf("Model = %q, want BM1370", s.Model)
	}
	if s.Firmware != "v1.0.34" {
		t.Errorf("Firmware = %q, want v1.0.34", s.Firmware)
	}
	if s.UptimeSeconds != 43215 {
		t.Errorf("UptimeSeconds = %v, want 43215", s.UptimeSeconds)
	}
}

// The 1000x unit trap: the same physical 5 V must not read as 5000 on one
// firmware and 5 on the other.
func TestVoltageIsConsistentAcrossVariants(t *testing.T) {
	up := upstreamServer(t)
	defer up.Close()
	nq := nerdqaxeServer(t)
	defer nq.Close()

	a, _ := newClient("g", up.URL).Fetch(context.Background())
	b, _ := newClient("n", nq.URL).Fetch(context.Background())

	if a.VoltageV == nil || b.VoltageV == nil {
		t.Fatal("voltage missing")
	}
	if math.Abs(*a.VoltageV-*b.VoltageV) > 0.2 {
		t.Errorf("voltages %.3f and %.3f differ by more than rounding; a unit conversion is wrong", *a.VoltageV, *b.VoltageV)
	}
}

func TestVariantDetectedOnceThenCached(t *testing.T) {
	var dashHits int32
	info := fixture(t, "upstream_info.json")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/dashboard", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&dashHits, 1)
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/system/info", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(info)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newClient("g", srv.URL)
	for i := 0; i < 5; i++ {
		if _, err := c.Fetch(context.Background()); err != nil {
			t.Fatalf("fetch %d: %v", i, err)
		}
	}
	if n := atomic.LoadInt32(&dashHits); n != 1 {
		t.Errorf("dashboard probed %d times, want 1 (detection must cache)", n)
	}
}

func TestUnreachableMinerReturnsError(t *testing.T) {
	// Nothing is listening here.
	c := New(Config{Name: "dead", BaseURL: "http://127.0.0.1:1", Timeout: 500 * time.Millisecond})
	if _, err := c.Fetch(context.Background()); err == nil {
		t.Error("Fetch to an unreachable miner returned no error")
	}
}

func TestMalformedJsonReturnsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/dashboard", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("/api/system/info", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if _, err := newClient("g", srv.URL).Fetch(context.Background()); err == nil {
		t.Error("malformed JSON returned no error")
	}
}

// A field the firmware omits must arrive as nil, never as a confident zero.
func TestMissingFieldsBecomeNil(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/dashboard", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("/api/system/info", func(w http.ResponseWriter, r *http.Request) {
		// Only hashRate present; everything else absent.
		_, _ = w.Write([]byte(`{"hashRate": 500, "ASICModel": "BM1370"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s, err := newClient("g", srv.URL).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if s.PowerW != nil {
		t.Errorf("PowerW = %v, want nil when absent", *s.PowerW)
	}
	if s.VRMTempC != nil {
		t.Errorf("VRMTempC = %v, want nil when absent", *s.VRMTempC)
	}
	if s.SharesAccepted != nil {
		t.Errorf("SharesAccepted = %v, want nil when absent", *s.SharesAccepted)
	}
	if s.HashrateTHs != 0.5 {
		t.Errorf("HashrateTHs = %v, want 0.5", s.HashrateTHs)
	}
}

func TestContextCancellationIsHonoured(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/dashboard", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("/api/system/info", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := newClient("g", srv.URL).Fetch(ctx); err == nil {
		t.Error("a slow miner past the context deadline returned no error")
	}
}

func TestNameIsReported(t *testing.T) {
	if got := newClient("NerdOctaxe", "http://x").Name(); got != "NerdOctaxe" {
		t.Errorf("Name = %q", got)
	}
}
