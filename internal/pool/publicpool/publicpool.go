// Package publicpool is the read-only adapter for Public Pool
// (github.com/benjamin-wilson/public-pool), the solo pool selected for the MVP.
//
// Each miner has its own payout address, so the adapter queries
// /api/client/{address} once per address and aggregates the results into a
// single snapshot. The combined best difficulty is the maximum across
// addresses, never a sum.
//
// Public Pool does not expose per-worker rejected shares or an assigned
// difficulty, so Capabilities reports those as unavailable and the dashboard
// falls back to the miners' own figures.
package publicpool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/model"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/pool"
)

const (
	defaultBaseURL = "https://public-pool.io:40557"
	maxBody        = 1 << 20
	// hsPerThs converts Public Pool's hashRate, which is H/s
	// (shares * 2^32 / seconds), to TH/s.
	hsPerThs = 1e12
)

// Target ties a payout address to the miner it belongs to.
type Target struct {
	MinerName string
	Address   string
}

// Config configures the adapter.
type Config struct {
	BaseURL string
	Timeout time.Duration
	Targets []Target
}

// Adapter is the Public Pool collector.
type Adapter struct {
	baseURL string
	targets []Target
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
		baseURL: base,
		targets: cfg.Targets,
		http: &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second},
		},
	}
}

// Name identifies the adapter.
func (a *Adapter) Name() string { return "publicpool" }

// Capabilities reports what Public Pool can actually supply.
func (a *Adapter) Capabilities() pool.Capabilities {
	return pool.Capabilities{
		RejectedShares:   false,
		PoolDifficulty:   false,
		ConnectionStatus: false,
		BestEver:         false,
	}
}

// flexFloat accepts a JSON number or a numeric string, because Public Pool
// returns the top-level bestDifficulty as a number but each worker's
// bestDifficulty as a string (toFixed(2)).
type flexFloat struct {
	val float64
	set bool
}

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			return nil
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		f.val, f.set = v, true
		return nil
	}
	var v float64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	f.val, f.set = v, true
	return nil
}

type clientResp struct {
	BestDifficulty flexFloat `json:"bestDifficulty"`
	WorkersCount   int       `json:"workersCount"`
	Workers        []struct {
		SessionID      string    `json:"sessionId"`
		Name           string    `json:"name"`
		BestDifficulty flexFloat `json:"bestDifficulty"`
		HashRate       *float64  `json:"hashRate"`
		LastSeen       time.Time `json:"lastSeen"`
	} `json:"workers"`
}

type poolResp struct {
	TotalHashRate *float64 `json:"totalHashRate"`
	TotalMiners   *int     `json:"totalMiners"`
	BlocksFound   *int     `json:"blocksFound"`
}

type addrResult struct {
	target Target
	resp   *clientResp
	err    error
	// notFound marks an address the pool has not seen yet: not a failure.
	notFound bool
}

// Fetch queries every configured address concurrently, plus the pool-wide
// endpoint, and folds the results into one snapshot.
func (a *Adapter) Fetch(ctx context.Context) (pool.Snapshot, error) {
	results := make([]addrResult, len(a.targets))
	var wg sync.WaitGroup
	for i, tgt := range a.targets {
		wg.Add(1)
		go func(i int, tgt Target) {
			defer wg.Done()
			results[i] = a.fetchAddress(ctx, tgt)
		}(i, tgt)
	}
	wg.Wait()

	snap := pool.Snapshot{Provider: "publicpool", Caps: a.Capabilities()}

	var (
		anyOK        bool
		lastErr      error
		bestDiff     float64
		haveBestDiff bool
		latestShare  time.Time
	)

	for _, r := range results {
		if r.err != nil {
			lastErr = r.err
			continue
		}
		if r.notFound {
			// Address awaiting its first share. Known, empty, not a failure.
			anyOK = true
			continue
		}
		anyOK = true

		if r.resp.BestDifficulty.set && (!haveBestDiff || r.resp.BestDifficulty.val > bestDiff) {
			bestDiff = r.resp.BestDifficulty.val
			haveBestDiff = true
		}

		for _, w := range r.resp.Workers {
			worker := pool.Worker{
				Name:      w.Name,
				MinerName: r.target.MinerName,
				LastSeen:  w.LastSeen,
			}
			if w.BestDifficulty.set {
				bd := w.BestDifficulty.val
				worker.BestDifficulty = &bd
			}
			if w.HashRate != nil {
				ths := *w.HashRate / hsPerThs
				worker.HashrateTHs = &ths
			}
			snap.Workers = append(snap.Workers, worker)

			if !w.LastSeen.IsZero() && w.LastSeen.After(latestShare) {
				latestShare = w.LastSeen
			}
		}
		snap.WorkersCount += len(r.resp.Workers)
	}

	// Every address failed at the network level: fail so the source goes stale.
	if !anyOK {
		if lastErr == nil {
			lastErr = fmt.Errorf("publicpool: no addresses configured")
		}
		return pool.Snapshot{}, lastErr
	}

	if haveBestDiff {
		snap.BestDifficulty = &bestDiff
	}
	if !latestShare.IsZero() {
		snap.LastShare = &latestShare
	}

	// Pool-wide stats are best effort.
	if pr, err := a.fetchPool(ctx); err == nil {
		snap.PoolMiners = pr.TotalMiners
		snap.BlocksFound = pr.BlocksFound
		if pr.TotalHashRate != nil {
			ths := *pr.TotalHashRate / hsPerThs
			snap.PoolHashrateTHs = &ths
		}
	}

	snap.Source = model.Source{}
	snap.Succeed(time.Now())
	return snap, nil
}

func (a *Adapter) fetchAddress(ctx context.Context, tgt Target) addrResult {
	status, body, err := a.get(ctx, "/api/client/"+tgt.Address)
	if err != nil {
		return addrResult{target: tgt, err: err}
	}
	if status == http.StatusNotFound {
		return addrResult{target: tgt, notFound: true}
	}
	if status != http.StatusOK {
		return addrResult{target: tgt, err: fmt.Errorf("publicpool %s: status %d", tgt.MinerName, status)}
	}
	var cr clientResp
	if err := json.Unmarshal(body, &cr); err != nil {
		return addrResult{target: tgt, err: fmt.Errorf("publicpool %s: parse: %w", tgt.MinerName, err)}
	}
	return addrResult{target: tgt, resp: &cr}
}

func (a *Adapter) fetchPool(ctx context.Context) (poolResp, error) {
	var pr poolResp
	status, body, err := a.get(ctx, "/api/pool")
	if err != nil {
		return pr, err
	}
	if status != http.StatusOK {
		return pr, fmt.Errorf("publicpool: /api/pool status %d", status)
	}
	if err := json.Unmarshal(body, &pr); err != nil {
		return pr, err
	}
	return pr, nil
}

func (a *Adapter) get(ctx context.Context, path string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+path, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "raspberry-mining-monitor")

	resp, err := a.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("publicpool: GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return resp.StatusCode, nil, fmt.Errorf("publicpool: GET %s: rate limited (429)", path)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("publicpool: read %s: %w", path, err)
	}
	return resp.StatusCode, body, nil
}
