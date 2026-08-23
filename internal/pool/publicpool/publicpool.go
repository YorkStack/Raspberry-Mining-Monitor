// Package publicpool is the read-only adapter for Public Pool
// (github.com/benjamin-wilson/public-pool).
//
// Each miner has its own payout address, so the adapter queries
// /api/client/{address} once per address and aggregates the results. The best
// difficulty is the maximum across addresses, never a sum. The set of addresses
// comes from the pool.Input on every fetch, so the adapter is stateless and the
// router can hand it a changing subset of miners.
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
	// hsPerThs converts Public Pool's hashRate (H/s) to TH/s.
	hsPerThs = 1e12
)

// Config configures the adapter.
type Config struct {
	BaseURL string
	Timeout time.Duration
}

// Adapter is the Public Pool collector.
type Adapter struct {
	baseURL string
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
		http: &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second},
		},
	}
}

// Name identifies the adapter.
func (a *Adapter) Name() string { return pool.KeyPublicPool }

// Capabilities reports what Public Pool can actually supply.
func (a *Adapter) Capabilities() pool.Capabilities {
	return pool.Caps(
		pool.FieldHashrate,
		pool.FieldBestShare,
		pool.FieldLastShare,
		pool.FieldActiveWorkers,
		pool.FieldBlocksFound,
	)
}

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
	miner    pool.Miner
	resp     *clientResp
	err      error
	notFound bool
}

// Fetch queries every miner's address concurrently, plus the pool-wide
// endpoint, and folds the results into one snapshot.
func (a *Adapter) Fetch(ctx context.Context, in pool.Input) (pool.Snapshot, error) {
	results := make([]addrResult, len(in.Miners))
	var wg sync.WaitGroup
	for i, m := range in.Miners {
		wg.Add(1)
		go func(i int, m pool.Miner) {
			defer wg.Done()
			results[i] = a.fetchAddress(ctx, m)
		}(i, m)
	}
	wg.Wait()

	snap := pool.Snapshot{Provider: pool.KeyPublicPool, Caps: a.Capabilities()}

	var (
		anyOK        bool
		lastErr      error
		bestDiff     float64
		haveBestDiff bool
		latestShare  time.Time
		hashrate     float64
		haveHashrate bool
		active       int
	)

	for _, r := range results {
		if r.err != nil {
			lastErr = r.err
			continue
		}
		if r.notFound {
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
				MinerName: r.miner.Name,
				Provider:  pool.KeyPublicPool,
				LastSeen:  w.LastSeen,
			}
			if w.BestDifficulty.set {
				bd := w.BestDifficulty.val
				worker.BestDifficulty = &bd
			}
			if w.HashRate != nil {
				ths := *w.HashRate / hsPerThs
				worker.HashrateTHs = &ths
				hashrate += ths
				haveHashrate = true
			}
			snap.Workers = append(snap.Workers, worker)
			active++

			if !w.LastSeen.IsZero() && w.LastSeen.After(latestShare) {
				latestShare = w.LastSeen
			}
		}
		snap.WorkersCount += len(r.resp.Workers)
	}

	if !anyOK {
		if lastErr == nil {
			lastErr = fmt.Errorf("publicpool: no addresses configured")
		}
		return pool.Snapshot{}, lastErr
	}

	if haveBestDiff {
		snap.BestDifficulty = &bestDiff
	}
	if haveHashrate {
		snap.HashrateTHs = &hashrate
	}
	snap.ActiveWorkers = &active
	if !latestShare.IsZero() {
		snap.LastShare = &latestShare
	}

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

func (a *Adapter) fetchAddress(ctx context.Context, m pool.Miner) addrResult {
	status, body, err := a.get(ctx, "/api/client/"+m.Address)
	if err != nil {
		return addrResult{miner: m, err: err}
	}
	if status == http.StatusNotFound {
		return addrResult{miner: m, notFound: true}
	}
	if status != http.StatusOK {
		return addrResult{miner: m, err: fmt.Errorf("publicpool %s: status %d", m.Name, status)}
	}
	var cr clientResp
	if err := json.Unmarshal(body, &cr); err != nil {
		return addrResult{miner: m, err: fmt.Errorf("publicpool %s: parse: %w", m.Name, err)}
	}
	return addrResult{miner: m, resp: &cr}
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
