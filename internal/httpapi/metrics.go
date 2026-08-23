package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/dashboard"
)

// handleMetrics serves the fleet state in Prometheus text format. It is
// read-only and gated to the local network, like the other operator surfaces,
// and can be switched off entirely with dashboard.metrics: false.
func (o Options) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(renderMetrics(o.view(), o.Version)))
}

// escapeLabel escapes a Prometheus label value.
func escapeLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// renderMetrics builds the Prometheus exposition text from a dashboard view.
func renderMetrics(v dashboard.View, version string) string {
	var b strings.Builder

	line := func(name string, val float64) {
		fmt.Fprintf(&b, "%s %g\n", name, val)
	}
	labelled := func(name, miner string, val float64) {
		fmt.Fprintf(&b, "%s{miner=\"%s\"} %g\n", name, escapeLabel(miner), val)
	}
	help := func(name, typ, help string) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
	}
	b01 := func(cond bool) float64 {
		if cond {
			return 1
		}
		return 0
	}

	help("rmm_build_info", "gauge", "Build version, value is always 1.")
	fmt.Fprintf(&b, "rmm_build_info{version=\"%s\"} 1\n", escapeLabel(version))

	help("rmm_miner_online", "gauge", "1 if the miner is reporting, 0 otherwise.")
	for _, m := range v.Miners {
		labelled("rmm_miner_online", m.Name, b01(m.Online))
	}
	help("rmm_miner_hashrate_ths", "gauge", "Miner hashrate in TH/s.")
	for _, m := range v.Miners {
		labelled("rmm_miner_hashrate_ths", m.Name, m.HashrateTHs)
	}
	help("rmm_miner_power_watts", "gauge", "Miner power draw in watts.")
	for _, m := range v.Miners {
		if m.PowerW != nil {
			labelled("rmm_miner_power_watts", m.Name, *m.PowerW)
		}
	}
	help("rmm_miner_asic_temp_celsius", "gauge", "Miner ASIC temperature in Celsius.")
	for _, m := range v.Miners {
		if m.ASICTempC != nil {
			labelled("rmm_miner_asic_temp_celsius", m.Name, *m.ASICTempC)
		}
	}
	help("rmm_miner_vrm_temp_celsius", "gauge", "Miner VRM temperature in Celsius.")
	for _, m := range v.Miners {
		if m.VRMTempC != nil {
			labelled("rmm_miner_vrm_temp_celsius", m.Name, *m.VRMTempC)
		}
	}
	help("rmm_miner_shares_accepted_total", "counter", "Accepted shares reported by the miner.")
	for _, m := range v.Miners {
		if m.SharesAccepted != nil {
			labelled("rmm_miner_shares_accepted_total", m.Name, float64(*m.SharesAccepted))
		}
	}
	help("rmm_miner_shares_rejected_total", "counter", "Rejected shares reported by the miner.")
	for _, m := range v.Miners {
		if m.SharesRejected != nil {
			labelled("rmm_miner_shares_rejected_total", m.Name, float64(*m.SharesRejected))
		}
	}

	help("rmm_fleet_hashrate_ths", "gauge", "Combined hashrate of online miners in TH/s.")
	line("rmm_fleet_hashrate_ths", v.Totals.HashrateTHs)
	help("rmm_fleet_power_watts", "gauge", "Combined power of online miners in watts.")
	line("rmm_fleet_power_watts", v.Totals.PowerW)
	help("rmm_fleet_miners_online", "gauge", "Number of miners currently reporting.")
	line("rmm_fleet_miners_online", float64(v.Totals.MinersOnline))
	help("rmm_fleet_miners_total", "gauge", "Number of configured miners.")
	line("rmm_fleet_miners_total", float64(v.Totals.MinersTotal))

	help("rmm_pool_workers", "gauge", "Workers seen by the pool.")
	line("rmm_pool_workers", float64(v.Pool.WorkersCount))
	if v.Pool.BestDifficulty != nil {
		help("rmm_pool_best_difficulty", "gauge", "Best share difficulty this session.")
		line("rmm_pool_best_difficulty", *v.Pool.BestDifficulty)
	}

	help("rmm_network_difficulty", "gauge", "Bitcoin network difficulty.")
	line("rmm_network_difficulty", v.Network.Difficulty)
	help("rmm_network_hashrate_hs", "gauge", "Bitcoin network hashrate in H/s.")
	line("rmm_network_hashrate_hs", v.Network.NetworkHashrateHs)
	help("rmm_network_height", "gauge", "Bitcoin block height.")
	line("rmm_network_height", float64(v.Network.Height))
	help("rmm_btc_price_eur", "gauge", "BTC price in EUR.")
	line("rmm_btc_price_eur", v.Network.PriceEUR)

	if v.Probability != nil {
		help("rmm_solo_probability_year", "gauge", "Probability of >=1 block within a year (0..1).")
		line("rmm_solo_probability_year", v.Probability.Year)
		help("rmm_solo_mean_interval_seconds", "gauge", "Statistical mean interval between blocks, in seconds.")
		line("rmm_solo_mean_interval_seconds", v.Probability.MeanSeconds)
	}

	return b.String()
}
