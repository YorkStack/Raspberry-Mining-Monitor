// Package braiins is the read-only adapter for Braiins Pool.
//
// Unlike the solo pools, Braiins keys statistics on an account (an API token)
// rather than on a payout address, so this adapter fetches the account totals
// once per cycle and ignores per-address targets. It is registered only when a
// token is configured; without one the router falls back to the generic
// telemetry provider.
package braiins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/model"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/pool"
)

const (
	defaultBaseURL = "https://pool.braiins.com"
	statsPath      = "/stats/json/btc/"
	maxBody        = 1 << 20
)

// Config configures the adapter. Token is the account's Pool-Auth-Token.
type Config struct {
	BaseURL string
	Token   string
	Timeout time.Duration
}

// Adapter is the Braiins Pool collector.
type Adapter struct {
	baseURL string
	token   string
	http    *http.Client
}

// New creates an adapter.
func New(cfg Config) *Adapter {
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &Adapter{
		baseURL: strings.TrimRight(base, "/"),
		token:   cfg.Token,
		http: &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{MaxIdleConns: 2, IdleConnTimeout: 30 * time.Second},
		},
	}
}

// Name identifies the adapter.
func (a *Adapter) Name() string { return pool.KeyBraiins }

// Capabilities reports what the account stats endpoint supplies.
func (a *Adapter) Capabilities() pool.Capabilities {
	return pool.Caps(pool.FieldHashrate, pool.FieldActiveWorkers)
}

type statsResp struct {
	BTC struct {
		HashRateUnit string  `json:"hash_rate_unit"`
		HashRate5m   float64 `json:"hash_rate_5m"`
		HashRate24h  float64 `json:"hash_rate_24h"`
		OKWorkers    int     `json:"ok_workers"`
		LowWorkers   int     `json:"low_workers"`
	} `json:"btc"`
}

// Fetch reads the account totals. The token identifies the account, so the
// per-address targets in the input are not used.
func (a *Adapter) Fetch(ctx context.Context, _ pool.Input) (pool.Snapshot, error) {
	if a.token == "" {
		return pool.Snapshot{}, fmt.Errorf("braiins: no API token configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+statsPath, nil)
	if err != nil {
		return pool.Snapshot{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "raspberry-mining-monitor")
	req.Header.Set("Pool-Auth-Token", a.token)

	resp, err := a.http.Do(req)
	if err != nil {
		return pool.Snapshot{}, fmt.Errorf("braiins: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return pool.Snapshot{}, fmt.Errorf("braiins: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return pool.Snapshot{}, fmt.Errorf("braiins: read: %w", err)
	}
	var sr statsResp
	if err := json.Unmarshal(body, &sr); err != nil {
		return pool.Snapshot{}, fmt.Errorf("braiins: parse: %w", err)
	}

	snap := pool.Snapshot{Provider: pool.KeyBraiins, Caps: a.Capabilities()}
	if ths, ok := toTHs(sr.BTC.HashRate5m, sr.BTC.HashRateUnit); ok {
		snap.HashrateTHs = &ths
	}
	active := sr.BTC.OKWorkers
	snap.ActiveWorkers = &active
	snap.WorkersCount = sr.BTC.OKWorkers + sr.BTC.LowWorkers

	snap.Source = model.Source{}
	snap.Succeed(time.Now())
	return snap, nil
}

// toTHs converts a Braiins hash-rate value and its unit ("Gh/s", "Th/s", …) to
// TH/s.
func toTHs(v float64, unit string) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "ph/s":
		return v * 1000, true
	case "th/s":
		return v, true
	case "gh/s":
		return v / 1000, true
	case "mh/s":
		return v / 1e6, true
	case "", "h/s":
		return v / 1e12, true
	default:
		return 0, false
	}
}
