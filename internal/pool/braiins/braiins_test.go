package braiins

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/pool"
)

func TestFetchRequiresToken(t *testing.T) {
	a := New(Config{BaseURL: "http://x", Timeout: time.Second})
	if _, err := a.Fetch(context.Background(), pool.Input{}); err == nil {
		t.Error("want error without a token")
	}
}

func TestFetchParsesStatsAndSendsToken(t *testing.T) {
	var gotToken string
	mux := http.NewServeMux()
	mux.HandleFunc(statsPath, func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("Pool-Auth-Token")
		_, _ = w.Write([]byte(`{"btc":{"hash_rate_unit":"Gh/s","hash_rate_5m":12100.0,"hash_rate_24h":12000.0,"ok_workers":2,"low_workers":1}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL, Token: "secret-token", Timeout: 2 * time.Second})
	s, err := a.Fetch(context.Background(), pool.Input{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotToken != "secret-token" {
		t.Errorf("Pool-Auth-Token = %q, want secret-token", gotToken)
	}
	if s.HashrateTHs == nil || math.Abs(*s.HashrateTHs-12.1) > 1e-6 {
		t.Errorf("HashrateTHs = %v, want 12.1 (12100 Gh/s)", s.HashrateTHs)
	}
	if s.ActiveWorkers == nil || *s.ActiveWorkers != 2 {
		t.Errorf("ActiveWorkers = %v, want 2", s.ActiveWorkers)
	}
}
