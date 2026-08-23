// Package ckpool is the read-only adapter for solo.ckpool.org and its regional
// mirrors (eusolo, etc.). It queries /users/{address} once per address and
// aggregates. Hashrate strings carry an SI suffix (for example "1.23T"), which
// this adapter parses to TH/s.
package ckpool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/model"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/pool"
)

const (
	defaultBaseURL = "https://solo.ckpool.org"
	maxBody        = 1 << 20
)

// Config configures the adapter.
type Config struct {
	BaseURL string
	Timeout time.Duration
}

// Adapter is the ckpool collector.
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
		baseURL: strings.TrimRight(base, "/"),
		http: &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second},
		},
	}
}

// Name identifies the adapter.
func (a *Adapter) Name() string { return pool.KeyCKPool }

// Capabilities reports what ckpool supplies per user.
func (a *Adapter) Capabilities() pool.Capabilities {
	return pool.Caps(
		pool.FieldHashrate,
		pool.FieldAcceptedShares,
		pool.FieldBestShare,
		pool.FieldBestEver,
		pool.FieldLastShare,
		pool.FieldActiveWorkers,
	)
}

type userResp struct {
	Hashrate1hr string  `json:"hashrate1hr"`
	Hashrate5m  string  `json:"hashrate5m"`
	LastShare   int64   `json:"lastshare"`
	Workers     int     `json:"workers"`
	Shares      uint64  `json:"shares"`
	BestShare   float64 `json:"bestshare"`
	BestEver    float64 `json:"bestever"`
	Worker      []struct {
		Name        string `json:"workername"`
		Hashrate1hr string `json:"hashrate1hr"`
		Hashrate5m  string `json:"hashrate5m"`
		LastShare   int64  `json:"lastshare"`
		BestShare   float64 `json:"bestshare"`
	} `json:"worker"`
}

type addrResult struct {
	miner    pool.Miner
	resp     *userResp
	err      error
	notFound bool
}

// Fetch queries each address concurrently and merges into one snapshot.
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

	snap := pool.Snapshot{Provider: pool.KeyCKPool, Caps: a.Capabilities()}
	var (
		anyOK        bool
		lastErr      error
		hashrate     float64
		haveHashrate bool
		accepted     uint64
		haveAccepted bool
		bestShare    float64
		haveBest     bool
		bestEver     float64
		haveEver     bool
		latest       time.Time
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
		u := r.resp

		if ths, ok := parseHashrate(pick(u.Hashrate1hr, u.Hashrate5m)); ok {
			hashrate += ths
			haveHashrate = true
		}
		accepted += u.Shares
		haveAccepted = true
		if !haveBest || u.BestShare > bestShare {
			bestShare, haveBest = u.BestShare, true
		}
		if !haveEver || u.BestEver > bestEver {
			bestEver, haveEver = u.BestEver, true
		}
		if u.LastShare > 0 {
			ts := time.Unix(u.LastShare, 0)
			if ts.After(latest) {
				latest = ts
			}
		}
		active += u.Workers

		for _, w := range u.Worker {
			worker := pool.Worker{Name: w.Name, MinerName: r.miner.Name, Provider: pool.KeyCKPool}
			if ths, ok := parseHashrate(pick(w.Hashrate1hr, w.Hashrate5m)); ok {
				worker.HashrateTHs = &ths
			}
			if w.BestShare > 0 {
				bs := w.BestShare
				worker.BestDifficulty = &bs
			}
			if w.LastShare > 0 {
				worker.LastSeen = time.Unix(w.LastShare, 0)
			}
			snap.Workers = append(snap.Workers, worker)
			snap.WorkersCount++
		}
	}

	if !anyOK {
		if lastErr == nil {
			lastErr = fmt.Errorf("ckpool: no addresses configured")
		}
		return pool.Snapshot{}, lastErr
	}

	if haveHashrate {
		snap.HashrateTHs = &hashrate
	}
	if haveAccepted {
		snap.AcceptedShares = &accepted
	}
	if haveBest {
		snap.BestDifficulty = &bestShare
	}
	if haveEver {
		snap.BestEver = &bestEver
	}
	if !latest.IsZero() {
		snap.LastShare = &latest
	}
	snap.ActiveWorkers = &active

	snap.Source = model.Source{}
	snap.Succeed(time.Now())
	return snap, nil
}

func (a *Adapter) fetchAddress(ctx context.Context, m pool.Miner) addrResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/users/"+m.Address, nil)
	if err != nil {
		return addrResult{miner: m, err: err}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "raspberry-mining-monitor")

	resp, err := a.http.Do(req)
	if err != nil {
		return addrResult{miner: m, err: fmt.Errorf("ckpool %s: %w", m.Name, err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return addrResult{miner: m, notFound: true}
	}
	if resp.StatusCode != http.StatusOK {
		return addrResult{miner: m, err: fmt.Errorf("ckpool %s: status %d", m.Name, resp.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return addrResult{miner: m, err: fmt.Errorf("ckpool %s: read: %w", m.Name, err)}
	}
	var u userResp
	if err := json.Unmarshal(body, &u); err != nil {
		return addrResult{miner: m, err: fmt.Errorf("ckpool %s: parse: %w", m.Name, err)}
	}
	return addrResult{miner: m, resp: &u}
}

func pick(a, b string) string {
	if strings.TrimSpace(a) != "" && a != "0" {
		return a
	}
	return b
}

// parseHashrate turns a ckpool hashrate string ("1.23T", "890G", "0") into
// TH/s. The suffix is the SI magnitude in hashes per second.
func parseHashrate(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, false
	}
	mult := map[byte]float64{
		'E': 1e6, 'P': 1e3, 'T': 1, 'G': 1e-3, 'M': 1e-6, 'K': 1e-9, 'k': 1e-9,
	}
	last := s[len(s)-1]
	if m, ok := mult[last]; ok {
		v, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-1]), 64)
		if err != nil {
			return 0, false
		}
		return v * m, true
	}
	// No suffix: hashes per second.
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v / 1e12, true
}
