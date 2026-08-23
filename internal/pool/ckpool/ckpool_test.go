package ckpool

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/pool"
)

const addr = "bc1qckpoolckpoolckpoolckpoolckpoolckpool00"

func input() pool.Input {
	return pool.Input{Miners: []pool.Miner{{Name: "NerdOctaxe", Address: addr}}}
}

func newAdapter(base string) *Adapter { return New(Config{BaseURL: base, Timeout: 2 * time.Second}) }

func TestParseHashrateSuffixes(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"1.23T", 1.23, true},
		{"890G", 0.89, true},
		{"2P", 2000, true},
		{"0", 0, false},
		{"", 0, false},
		{"12000000000000", 12, true}, // 1.2e13 H/s -> 12 TH/s
	}
	for _, c := range cases {
		got, ok := parseHashrate(c.in)
		if ok != c.ok || (ok && math.Abs(got-c.want) > 1e-6) {
			t.Errorf("parseHashrate(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestFetchParsesUser(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/users/"+addr, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"hashrate1hr":"12.1T","hashrate5m":"12.5T",
			"lastshare":1756900790,"workers":1,"shares":123456,
			"bestshare":124300000000,"bestever":250000000000,
			"worker":[{"workername":"bc1q.nerd","hashrate1hr":"12.1T","lastshare":1756900790,"bestshare":124300000000}]
		}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s, err := newAdapter(srv.URL).Fetch(context.Background(), input())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if s.Provider != pool.KeyCKPool {
		t.Errorf("Provider = %q", s.Provider)
	}
	if s.HashrateTHs == nil || math.Abs(*s.HashrateTHs-12.1) > 1e-6 {
		t.Errorf("HashrateTHs = %v, want 12.1", s.HashrateTHs)
	}
	if s.AcceptedShares == nil || *s.AcceptedShares != 123456 {
		t.Errorf("AcceptedShares = %v, want 123456", s.AcceptedShares)
	}
	if s.BestDifficulty == nil || *s.BestDifficulty != 124300000000 {
		t.Errorf("BestDifficulty = %v", s.BestDifficulty)
	}
	if s.BestEver == nil || *s.BestEver != 250000000000 {
		t.Errorf("BestEver = %v", s.BestEver)
	}
	if s.LastShare == nil {
		t.Error("LastShare = nil")
	}
	if s.ActiveWorkers == nil || *s.ActiveWorkers != 1 {
		t.Errorf("ActiveWorkers = %v, want 1", s.ActiveWorkers)
	}
	if len(s.Workers) != 1 || s.Workers[0].Provider != pool.KeyCKPool {
		t.Errorf("worker not labelled: %+v", s.Workers)
	}
}

func TestUnknownAddressNotAnError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/users/"+addr, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s, err := newAdapter(srv.URL).Fetch(context.Background(), input())
	if err != nil {
		t.Fatalf("Fetch should not fail on unknown address: %v", err)
	}
	if !s.OK {
		t.Error("OK = false, want true")
	}
}
