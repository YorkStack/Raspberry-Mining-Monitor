// Package mempool is the public Bitcoin network-data provider. It targets the
// mempool.space REST API and any instance that mirrors it, so a self-hosted
// mempool box is a one-line base-URL change.
//
// The block subsidy and the next-halving height are computed locally from the
// tip height, never fetched: they are deterministic, and an API call for them
// would be a needless dependency.
package mempool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/bitcoin"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/model"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/subsidy"
)

const (
	defaultBaseURL = "https://mempool.space"
	maxBody        = 1 << 20 // 1 MiB
)

// Config configures the provider.
type Config struct {
	BaseURL string
	Timeout time.Duration
}

// Provider reads network data from a mempool.space-compatible API.
type Provider struct {
	baseURL string
	http    *http.Client
}

// New creates a provider.
func New(cfg Config) *Provider {
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &Provider{
		baseURL: base,
		http: &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{MaxIdleConns: 2, IdleConnTimeout: 30 * time.Second},
		},
	}
}

// SourceKind reports that this is a public API, which drives the dashboard's
// NETWORK SOURCE badge.
func (p *Provider) SourceKind() bitcoin.SourceKind { return bitcoin.SourcePublic }

type blockEntry struct {
	Height     uint32  `json:"height"`
	Timestamp  int64   `json:"timestamp"`
	Difficulty float64 `json:"difficulty"`
}

type hashrateResp struct {
	CurrentHashrate   float64 `json:"currentHashrate"`
	CurrentDifficulty float64 `json:"currentDifficulty"`
}

type retargetResp struct {
	DifficultyChange   float64 `json:"difficultyChange"`
	RemainingTimeMs    float64 `json:"remainingTime"`
	NextRetargetHeight uint32  `json:"nextRetargetHeight"`
}

type pricesResp struct {
	EUR float64 `json:"EUR"`
	USD float64 `json:"USD"`
}

// Network fetches the current chain state.
//
// /api/blocks is the one essential call: it carries height, timestamp and
// difficulty together, so a failure there fails the whole fetch and the source
// is marked stale. The hashrate and retarget calls are slow-moving extras; if
// they fail, their fields are simply absent rather than fabricated.
func (p *Provider) Network(ctx context.Context) (bitcoin.Snapshot, error) {
	var blocks []blockEntry
	if err := p.getJSON(ctx, "/api/blocks", &blocks); err != nil {
		return bitcoin.Snapshot{}, err
	}
	if len(blocks) == 0 {
		return bitcoin.Snapshot{}, errors.New("mempool: /api/blocks returned no blocks")
	}
	tip := blocks[0]

	s := bitcoin.Snapshot{
		Kind:              bitcoin.SourcePublic,
		Height:            tip.Height,
		Difficulty:        tip.Difficulty,
		LastBlockTime:     time.Unix(tip.Timestamp, 0),
		SubsidyBTC:        subsidy.BTC(tip.Height),
		NextHalvingHeight: subsidy.NextHalvingHeight(tip.Height),
	}

	// Secondary, best-effort. Network hashrate and a more precise difficulty.
	var hr hashrateResp
	if err := p.getJSON(ctx, "/api/v1/mining/hashrate/3d", &hr); err == nil {
		s.NetworkHashrateHs = hr.CurrentHashrate
		if hr.CurrentDifficulty > 0 {
			s.Difficulty = hr.CurrentDifficulty
		}
	}

	// Secondary, best-effort. Retarget estimate.
	var rt retargetResp
	if err := p.getJSON(ctx, "/api/v1/difficulty-adjustment", &rt); err == nil {
		s.NextRetargetChangePct = rt.DifficultyChange
		s.NextRetargetHeight = rt.NextRetargetHeight
		s.NextRetargetETASeconds = rt.RemainingTimeMs / 1000
	}

	// Secondary, best-effort. Fiat price.
	var pr pricesResp
	if err := p.getJSON(ctx, "/api/v1/prices", &pr); err == nil {
		s.PriceEUR = pr.EUR
	}

	s.Source = model.Source{}
	s.Succeed(time.Now())
	return s, nil
}

func (p *Provider) getJSON(ctx context.Context, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "raspberry-mining-monitor")

	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("mempool: GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	// A single fetch never retries. Rate limiting and outages are handled by the
	// collector's own backoff, which slows the whole poll rather than hammering
	// within one attempt.
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("mempool: GET %s: rate limited (429)", path)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mempool: GET %s: status %d", path, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("mempool: read %s: %w", path, err)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("mempool: parse %s: %w", path, err)
	}
	return nil
}
