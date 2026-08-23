// Package axeos is the read-only collector for AxeOS miners.
//
// It speaks two firmware dialects behind one interface: bitaxeorg/ESP-Miner
// (the Bitaxe, flat /api/system/info in mV and mA) and
// shufps/ESP-Miner-NerdQAxePlus (the NerdOctaxe, nested /api/v2/dashboard in
// V and A). The variant is detected once and cached. Everything is normalised
// to TH/s, volts and amps so the rest of the system never sees the difference.
//
// This collector only ever issues GET. It has no method that writes to a
// miner, by construction.
package axeos

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/model"
)

// reprobeAfter is how many consecutive failures force the variant to be
// re-detected, so a firmware upgrade between the two dialects is picked up.
const reprobeAfter = 5

// maxBody caps how much of a response is read, so a misbehaving device cannot
// exhaust memory on the Pi.
const maxBody = 1 << 20 // 1 MiB

// Config configures a miner client.
type Config struct {
	Name    string
	BaseURL string
	Timeout time.Duration
}

// Client is a single miner's collector.
type Client struct {
	name    string
	baseURL string
	http    *http.Client

	mu       sync.Mutex
	variant  miner.Variant // empty until detected
	failures int
}

// New creates a client. The HTTP client keeps no idle connections, so a miner
// that drops off the network does not leave a stale socket behind.
func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &Client{
		name:    cfg.Name,
		baseURL: cfg.BaseURL,
		http: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DisableKeepAlives:   true,
				MaxIdleConns:        1,
				TLSHandshakeTimeout: timeout,
			},
		},
	}
}

// Name is the configured display name.
func (c *Client) Name() string { return c.name }

// Fetch returns a normalised snapshot. On any error the returned snapshot is
// not marked OK, and the error is returned for the caller to record.
func (c *Client) Fetch(ctx context.Context) (miner.Snapshot, error) {
	variant, err := c.resolveVariant(ctx)
	if err != nil {
		c.noteFailure()
		return miner.Snapshot{}, err
	}

	var snap miner.Snapshot
	switch variant {
	case miner.VariantNerdQAxe:
		snap, err = c.fetchNerdQAxe(ctx)
	default:
		snap, err = c.fetchUpstream(ctx)
	}
	if err != nil {
		c.noteFailure()
		return miner.Snapshot{}, err
	}

	c.noteSuccess()
	snap.Name = c.name
	snap.Variant = variant
	snap.Source = model.Source{}
	snap.Succeed(time.Now())
	return snap, nil
}

func (c *Client) noteFailure() {
	c.mu.Lock()
	c.failures++
	if c.failures >= reprobeAfter {
		c.variant = "" // force re-detection on the next attempt
		c.failures = 0
	}
	c.mu.Unlock()
}

func (c *Client) noteSuccess() {
	c.mu.Lock()
	c.failures = 0
	c.mu.Unlock()
}

// resolveVariant returns the cached variant, detecting it once if needed.
// Detection probes /api/v2/dashboard: present means the NerdQAxe fork, absent
// (404) means upstream.
func (c *Client) resolveVariant(ctx context.Context) (miner.Variant, error) {
	c.mu.Lock()
	v := c.variant
	c.mu.Unlock()
	if v != "" {
		return v, nil
	}

	status, _, err := c.get(ctx, "/api/v2/dashboard")
	if err != nil {
		return "", err
	}
	switch {
	case status == http.StatusOK:
		v = miner.VariantNerdQAxe
	case status == http.StatusNotFound:
		v = miner.VariantUpstream
	default:
		return "", fmt.Errorf("axeos %s: unexpected status %d probing variant", c.name, status)
	}

	c.mu.Lock()
	c.variant = v
	c.mu.Unlock()
	return v, nil
}

// get performs a GET and returns the status and body. A non-2xx status is not
// itself an error here, because 404 is meaningful during variant detection.
func (c *Client) get(ctx context.Context, path string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "raspberry-mining-monitor")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("axeos %s: %w", c.name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("axeos %s: read body: %w", c.name, err)
	}
	return resp.StatusCode, body, nil
}

// getJSON fetches a path and decodes it, requiring a 200.
func (c *Client) getJSON(ctx context.Context, path string, into any) error {
	status, body, err := c.get(ctx, path)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("axeos %s: %s returned status %d", c.name, path, status)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("axeos %s: parse %s: %w", c.name, path, err)
	}
	return nil
}
