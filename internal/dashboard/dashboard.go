// Package dashboard projects raw collector snapshots into the document the
// browser receives.
//
// The projection is an explicit allowlist, built field by field. Nothing is
// re-serialised from an upstream response, so a future firmware version cannot
// leak new fields into the UI just by adding them to its JSON.
package dashboard

import (
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/aggregate"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/bitcoin"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/pool"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/probability"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/subsidy"
)

// Thresholds are the per-fleet temperature warning levels in Celsius.
type Thresholds struct {
	ASICWarnC float64
	ASICCritC float64
	VRMWarnC  float64
	VRMCritC  float64
}

// Input is everything the projection needs. Intervals are used to decide
// whether a source has gone stale.
type Input struct {
	Miners        []miner.Snapshot
	MinerInterval time.Duration

	Pool         pool.Snapshot
	PoolInterval time.Duration

	Network         bitcoin.Snapshot
	NetworkInterval time.Duration

	// Thresholds is the fleet default.
	Thresholds Thresholds
	// MinerThresholds overrides the default for named miners. A six-phase
	// NerdOctaxe and a single-ASIC Gamma do not share a sensible warning band.
	MinerThresholds map[string]Thresholds

	// DisabledMiners are switched off in monitoring and hidden from the view.
	DisabledMiners map[string]bool

	// ScreensaverSeconds is the idle time before the burn-in saver. 0 disables.
	ScreensaverSeconds int
}

// thresholdsFor returns the band that applies to one miner.
func (in Input) thresholdsFor(name string) Thresholds {
	if t, ok := in.MinerThresholds[name]; ok {
		return t
	}
	return in.Thresholds
}

// View is the document served to the browser.
type View struct {
	GeneratedAt time.Time        `json:"generatedAt"`
	Miners      []MinerView      `json:"miners"`
	Totals      TotalsView       `json:"totals"`
	Pool        PoolView         `json:"pool"`
	Network     NetworkView      `json:"network"`
	Probability *ProbabilityView `json:"probability,omitempty"`

	// ScreensaverSeconds tells the UI when to show the burn-in saver.
	ScreensaverSeconds int `json:"screensaverSeconds"`
}

// MinerView is one tile on the dashboard.
type MinerView struct {
	Name     string `json:"name"`
	Model    string `json:"model,omitempty"`
	Firmware string `json:"firmware,omitempty"`
	Variant  string `json:"variant,omitempty"`

	Online     bool    `json:"online"`
	Stale      bool    `json:"stale"`
	HasData    bool    `json:"hasData"`
	AgeSeconds float64 `json:"ageSeconds"`
	Err        string  `json:"err,omitempty"`

	UptimeSeconds float64 `json:"uptimeSeconds"`

	HashrateTHs     float64  `json:"hashrateThs"`
	ExpectedHashTHs *float64 `json:"expectedHashThs,omitempty"`
	PowerW          *float64 `json:"powerW,omitempty"`
	EfficiencyJTH   *float64 `json:"efficiencyJth,omitempty"`
	FreqMHz         *float64 `json:"freqMhz,omitempty"`

	ASICTempC      *float64 `json:"asicTempC,omitempty"`
	ASICTempStatus string   `json:"asicTempStatus"`
	VRMTempC       *float64 `json:"vrmTempC,omitempty"`
	VRMTempStatus  string   `json:"vrmTempStatus"`

	FanRPM   *float64 `json:"fanRpm,omitempty"`
	FanPct   *float64 `json:"fanPct,omitempty"`
	FanCount int      `json:"fanCount"`

	SharesAccepted  *uint64  `json:"sharesAccepted,omitempty"`
	SharesRejected  *uint64  `json:"sharesRejected,omitempty"`
	AcceptanceRatio *float64 `json:"acceptanceRatio,omitempty"`

	BestDiff        *float64 `json:"bestDiff,omitempty"`
	BestSessionDiff *float64 `json:"bestSessionDiff,omitempty"`

	PoolURL string `json:"poolUrl,omitempty"`
	// PoolUserMasked is the payout address truncated for display. The full
	// address is never included in this document.
	PoolUserMasked string `json:"poolUserMasked,omitempty"`
	UsingFallback  *bool  `json:"usingFallback,omitempty"`
}

// TotalsView is the combined tile.
type TotalsView struct {
	HashrateTHs   float64  `json:"hashrateThs"`
	PowerW        float64  `json:"powerW"`
	PowerComplete bool     `json:"powerComplete"`
	EfficiencyJTH *float64 `json:"efficiencyJth,omitempty"`
	MinersOnline  int      `json:"minersOnline"`
	MinersTotal   int      `json:"minersTotal"`
}

// PoolView is the solo-mining panel.
type PoolView struct {
	Provider   string  `json:"provider"`
	Online     bool    `json:"online"`
	Stale      bool    `json:"stale"`
	HasData    bool    `json:"hasData"`
	AgeSeconds float64 `json:"ageSeconds"`
	Err        string  `json:"err,omitempty"`

	WorkersCount   int      `json:"workersCount"`
	BestDifficulty *float64 `json:"bestDifficulty,omitempty"`
	BestEver       *float64 `json:"bestEver,omitempty"`

	SharesAccepted uint64 `json:"sharesAccepted"`
	SharesRejected uint64 `json:"sharesRejected"`
	// RejectedFromMiners is true when the pool cannot report rejected shares
	// and the figures above came from the miners instead. The UI labels them
	// accordingly rather than passing them off as pool-side numbers.
	RejectedFromMiners bool `json:"rejectedFromMiners"`

	LastShareSeconds *float64 `json:"lastShareSeconds,omitempty"`
	// LastShareInferred marks a connection state derived from share age rather
	// than reported by the pool.
	LastShareInferred bool `json:"lastShareInferred"`
}

// NetworkView is the Bitcoin panel.
type NetworkView struct {
	SourceLabel string  `json:"sourceLabel"`
	Online      bool    `json:"online"`
	Stale       bool    `json:"stale"`
	HasData     bool    `json:"hasData"`
	AgeSeconds  float64 `json:"ageSeconds"`
	Err         string  `json:"err,omitempty"`

	Height            uint32  `json:"height"`
	Difficulty        float64 `json:"difficulty"`
	NetworkHashrateHs float64 `json:"networkHashrateHs"`
	SecondsSinceBlock float64 `json:"secondsSinceBlock"`

	SubsidyBTC        float64 `json:"subsidyBtc"`
	PriceEUR          float64 `json:"priceEur"`
	NextHalvingHeight uint32  `json:"nextHalvingHeight"`

	NextRetargetChangePct  float64 `json:"nextRetargetChangePct"`
	NextRetargetETASeconds float64 `json:"nextRetargetEtaSeconds,omitempty"`
}

// ProbabilityView holds solo block probabilities. Every field is a
// probability, never a prediction.
type ProbabilityView struct {
	Day   float64 `json:"day"`
	Week  float64 `json:"week"`
	Month float64 `json:"month"`
	Year  float64 `json:"year"`

	MeanYears   float64 `json:"meanYears"`
	MedianYears float64 `json:"medianYears"`

	ShareOfNetwork float64 `json:"shareOfNetwork"`
}

// MaskAddress truncates a payout address for display. The address is public
// information, but there is no reason to put it on a screen in full.
func MaskAddress(a string) string {
	if a == "" {
		return ""
	}
	const keep = 4
	if len(a) < 2*keep+2 {
		return "…"
	}
	return a[:keep] + "…" + a[len(a)-keep:]
}

// tempStatus maps a reading onto the three-colour band. An unset threshold
// disables that level rather than firing at every temperature, so a partly
// filled config cannot produce a permanent false alarm.
func tempStatus(c *float64, warn, crit float64) string {
	switch {
	case c == nil:
		return "unknown"
	case crit > 0 && *c >= crit:
		return "crit"
	case warn > 0 && *c >= warn:
		return "warn"
	default:
		return "ok"
	}
}

// Build projects the collector snapshots into the browser document.
func Build(in Input, now time.Time) View {
	v := View{
		GeneratedAt:        now,
		Miners:             make([]MinerView, 0, len(in.Miners)),
		ScreensaverSeconds: in.ScreensaverSeconds,
	}

	agg := make([]aggregate.MinerInput, 0, len(in.Miners))
	var accepted, rejected uint64

	for _, m := range in.Miners {
		if in.DisabledMiners[m.Name] {
			continue // switched off in monitoring, hidden entirely
		}
		th := in.thresholdsFor(m.Name)
		mv := MinerView{
			Name:            m.Name,
			Model:           m.Model,
			Firmware:        m.Firmware,
			Variant:         string(m.Variant),
			Online:          m.OK,
			Stale:           m.Stale(now, in.MinerInterval),
			HasData:         m.HasData(),
			AgeSeconds:      m.Age(now).Seconds(),
			Err:             m.Err,
			UptimeSeconds:   m.UptimeSeconds,
			HashrateTHs:     m.HashrateTHs,
			ExpectedHashTHs: m.ExpectedHashTHs,
			PowerW:          m.PowerW,
			FreqMHz:         m.FreqMHz,
			ASICTempC:       m.ASICTempC,
			ASICTempStatus:  tempStatus(m.ASICTempC, th.ASICWarnC, th.ASICCritC),
			VRMTempC:        m.VRMTempC,
			VRMTempStatus:   tempStatus(m.VRMTempC, th.VRMWarnC, th.VRMCritC),
			FanCount:        len(m.Fans),
			SharesAccepted:  m.SharesAccepted,
			SharesRejected:  m.SharesRejected,
			BestDiff:        m.BestDiff,
			BestSessionDiff: m.BestSessionDiff,
			PoolURL:         m.PoolURL,
			PoolUserMasked:  MaskAddress(m.PoolUser),
			UsingFallback:   m.UsingFallback,
		}

		if len(m.Fans) > 0 {
			mv.FanRPM = m.Fans[0].RPM
			mv.FanPct = m.Fans[0].Percent
		}
		if m.PowerW != nil {
			if eff, ok := aggregate.EfficiencyJTH(*m.PowerW, m.HashrateTHs); ok {
				mv.EfficiencyJTH = &eff
			}
		}
		if m.SharesAccepted != nil && m.SharesRejected != nil {
			if r, ok := aggregate.AcceptanceRatio(*m.SharesAccepted, *m.SharesRejected); ok {
				mv.AcceptanceRatio = &r
			}
			accepted += *m.SharesAccepted
			rejected += *m.SharesRejected
		}

		v.Miners = append(v.Miners, mv)
		agg = append(agg, aggregate.MinerInput{
			Name:        m.Name,
			OK:          m.OK,
			HashrateTHs: m.HashrateTHs,
			PowerW:      m.PowerW,
		})
	}

	totals := aggregate.Combine(agg)
	v.Totals = TotalsView{
		HashrateTHs:   totals.HashrateTHs,
		PowerW:        totals.PowerW,
		PowerComplete: totals.PowerComplete,
		MinersOnline:  totals.MinersOnline,
		MinersTotal:   totals.MinersTotal,
	}
	if totals.HasEfficiency {
		eff := totals.EfficiencyJTH
		v.Totals.EfficiencyJTH = &eff
	}

	v.Pool = PoolView{
		Provider:           in.Pool.Provider,
		Online:             in.Pool.OK,
		Stale:              in.Pool.Stale(now, in.PoolInterval),
		HasData:            in.Pool.HasData(),
		AgeSeconds:         in.Pool.Age(now).Seconds(),
		Err:                in.Pool.Err,
		WorkersCount:       in.Pool.WorkersCount,
		BestDifficulty:     in.Pool.BestDifficulty,
		BestEver:           in.Pool.BestEver,
		SharesAccepted:     accepted,
		SharesRejected:     rejected,
		RejectedFromMiners: !in.Pool.Caps.RejectedShares,
		LastShareInferred:  !in.Pool.Caps.ConnectionStatus,
	}
	if in.Pool.LastShare != nil {
		s := now.Sub(*in.Pool.LastShare).Seconds()
		v.Pool.LastShareSeconds = &s
	}

	n := in.Network
	v.Network = NetworkView{
		SourceLabel:            n.Kind.Label(),
		Online:                 n.OK,
		Stale:                  n.Stale(now, in.NetworkInterval),
		HasData:                n.HasData(),
		AgeSeconds:             n.Age(now).Seconds(),
		Err:                    n.Err,
		Height:                 n.Height,
		Difficulty:             n.Difficulty,
		NetworkHashrateHs:      n.NetworkHashrateHs,
		SubsidyBTC:             subsidy.BTC(n.Height),
		PriceEUR:               n.PriceEUR,
		NextHalvingHeight:      subsidy.NextHalvingHeight(n.Height),
		NextRetargetChangePct:  n.NextRetargetChangePct,
		NextRetargetETASeconds: n.NextRetargetETASeconds,
	}
	if !n.LastBlockTime.IsZero() {
		v.Network.SecondsSinceBlock = now.Sub(n.LastBlockTime).Seconds()
	}

	// Probability needs both a hashrate and a difficulty. Without either it is
	// absent rather than zero, because a zero would read as certainty.
	if n.HasData() && n.Difficulty > 0 && totals.HashrateTHs > 0 {
		hs := totals.HashrateTHs * 1e12
		p := ProbabilityView{
			Day:   probability.AtLeastOne(hs, n.Difficulty, probability.Day),
			Week:  probability.AtLeastOne(hs, n.Difficulty, probability.Week),
			Month: probability.AtLeastOne(hs, n.Difficulty, probability.Month),
			Year:  probability.AtLeastOne(hs, n.Difficulty, probability.Year),
		}
		if mean, ok := probability.MeanTimeToBlockSeconds(hs, n.Difficulty); ok {
			p.MeanYears = mean / probability.Year
		}
		if median, ok := probability.MedianTimeToBlockSeconds(hs, n.Difficulty); ok {
			p.MedianYears = median / probability.Year
		}
		if share, ok := probability.ShareOfNetwork(hs, n.NetworkHashrateHs); ok {
			p.ShareOfNetwork = share
		}
		v.Probability = &p
	}

	return v
}
