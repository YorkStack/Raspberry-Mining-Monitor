// Package push sends the live fleet metrics to a Prometheus-compatible
// remote_write endpoint, such as Grafana Cloud.
//
// It lets a monitor on the LAN forward its metrics to a hosted dashboard
// without ever exposing the monitor or the miners to the internet: the monitor
// only makes outbound HTTPS calls. The metric names and labels match the
// /metrics endpoint exactly, so both surfaces describe the same fleet.
//
// The remote_write body is a snappy-compressed Prometheus WriteRequest. The
// protobuf is encoded by hand to keep the monitor's dependency footprint small
// on the Raspberry Pi; only snappy compression is a dependency.
package push

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/golang/snappy"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/dashboard"
)

// Config configures the remote_write push. It is off unless Enabled is set and
// a URL, user and token are present.
type Config struct {
	Enabled  bool
	URL      string
	User     string // Grafana Cloud instance / username
	Token    string // Grafana Cloud API token (secret)
	Interval time.Duration
	Timeout  time.Duration
	Version  string // software version, emitted as rmm_build_info
}

type label struct{ name, value string }

type series struct {
	labels []label
	value  float64
}

func b01(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

// buildSeries turns a dashboard view into remote_write series. Names and labels
// mirror internal/httpapi/metrics.go. Every series carries __name__ and its
// labels are sorted by name, as remote_write requires.
func buildSeries(v dashboard.View, version string) []series {
	var out []series
	add := func(name string, value float64, extra ...label) {
		ls := make([]label, 0, len(extra)+1)
		ls = append(ls, label{"__name__", name})
		ls = append(ls, extra...)
		sort.Slice(ls, func(i, j int) bool { return ls[i].name < ls[j].name })
		out = append(out, series{labels: ls, value: value})
	}

	if version != "" {
		add("rmm_build_info", 1, label{"version", version})
	}

	for _, m := range v.Miners {
		ml := label{"miner", m.Name}
		add("rmm_miner_online", b01(m.Online), ml)
		add("rmm_miner_hashrate_ths", m.HashrateTHs, ml)
		if m.PowerW != nil {
			add("rmm_miner_power_watts", *m.PowerW, ml)
		}
		if m.ASICTempC != nil {
			add("rmm_miner_asic_temp_celsius", *m.ASICTempC, ml)
		}
		if m.VRMTempC != nil {
			add("rmm_miner_vrm_temp_celsius", *m.VRMTempC, ml)
		}
		if m.SharesAccepted != nil {
			add("rmm_miner_shares_accepted_total", float64(*m.SharesAccepted), ml)
		}
		if m.SharesRejected != nil {
			add("rmm_miner_shares_rejected_total", float64(*m.SharesRejected), ml)
		}
	}

	add("rmm_fleet_hashrate_ths", v.Totals.HashrateTHs)
	add("rmm_fleet_power_watts", v.Totals.PowerW)
	add("rmm_fleet_miners_online", float64(v.Totals.MinersOnline))
	add("rmm_fleet_miners_total", float64(v.Totals.MinersTotal))

	add("rmm_pool_workers", float64(v.Pool.WorkersCount))
	if v.Pool.BestDifficulty != nil {
		add("rmm_pool_best_difficulty", *v.Pool.BestDifficulty)
	}

	add("rmm_network_difficulty", v.Network.Difficulty)
	add("rmm_network_hashrate_hs", v.Network.NetworkHashrateHs)
	add("rmm_network_height", float64(v.Network.Height))
	add("rmm_btc_price_eur", v.Network.PriceEUR)

	if v.Probability != nil {
		add("rmm_solo_probability_year", v.Probability.Year)
		add("rmm_solo_mean_interval_seconds", v.Probability.MeanSeconds)
	}
	return out
}

// --- minimal protobuf wire encoding for prometheus.WriteRequest ---

func putUvarint(b []byte, v uint64) []byte {
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tmp[:], v)
	return append(b, tmp[:n]...)
}

func fieldTag(b []byte, field int, wire byte) []byte {
	return putUvarint(b, uint64(field)<<3|uint64(wire))
}

func lenField(b []byte, field int, data []byte) []byte {
	b = fieldTag(b, field, 2)
	b = putUvarint(b, uint64(len(data)))
	return append(b, data...)
}

func strField(b []byte, field int, s string) []byte {
	return lenField(b, field, []byte(s))
}

func doubleField(b []byte, field int, f float64) []byte {
	b = fieldTag(b, field, 1)
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], math.Float64bits(f))
	return append(b, tmp[:]...)
}

func varintField(b []byte, field int, v uint64) []byte {
	b = fieldTag(b, field, 0)
	return putUvarint(b, v)
}

func encodeLabel(l label) []byte {
	var b []byte
	b = strField(b, 1, l.name)
	b = strField(b, 2, l.value)
	return b
}

func encodeSample(value float64, tsMillis int64) []byte {
	var b []byte
	b = doubleField(b, 1, value)
	b = varintField(b, 2, uint64(tsMillis))
	return b
}

func encodeTimeSeries(s series, tsMillis int64) []byte {
	var b []byte
	for _, l := range s.labels {
		b = lenField(b, 1, encodeLabel(l))
	}
	b = lenField(b, 2, encodeSample(s.value, tsMillis))
	return b
}

// marshalWriteRequest encodes the series as a Prometheus remote_write
// WriteRequest (field 1 = repeated TimeSeries).
func marshalWriteRequest(ss []series, tsMillis int64) []byte {
	var b []byte
	for _, s := range ss {
		b = lenField(b, 1, encodeTimeSeries(s, tsMillis))
	}
	return b
}

// Client posts remote_write payloads.
type Client struct {
	cfg Config
	hc  *http.Client
	log *slog.Logger
}

// New builds a client. A nil logger is tolerated.
func New(cfg Config, log *slog.Logger) *Client {
	to := cfg.Timeout
	if to == 0 {
		to = 10 * time.Second
	}
	return &Client{cfg: cfg, hc: &http.Client{Timeout: to}, log: log}
}

// Push builds and sends one remote_write frame for the given view.
func (c *Client) Push(ctx context.Context, v dashboard.View, now time.Time) error {
	raw := marshalWriteRequest(buildSeries(v, c.cfg.Version), now.UnixMilli())
	body := snappy.Encode(nil, raw)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	req.Header.Set("User-Agent", "rmm/"+c.cfg.Version)
	if c.cfg.User != "" || c.cfg.Token != "" {
		req.SetBasicAuth(c.cfg.User, c.cfg.Token)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("remote_write %s: %s", resp.Status, bytes.TrimSpace(snippet))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// Run pushes the current view on a fixed interval until ctx is cancelled. It
// logs failures and keeps going: a dropped push must never take down the
// monitor.
func Run(ctx context.Context, c *Client, view func() dashboard.View) {
	interval := c.cfg.Interval
	if interval == 0 {
		interval = 30 * time.Second
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()

	push := func() {
		pctx, cancel := context.WithTimeout(ctx, c.hc.Timeout)
		defer cancel()
		if err := c.Push(pctx, view(), time.Now()); err != nil && c.log != nil {
			c.log.Warn("grafana push failed", "err", err)
		}
	}
	push() // send an initial frame so a dashboard lights up quickly
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			push()
		}
	}
}
