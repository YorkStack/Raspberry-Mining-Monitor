package axeos

import (
	"context"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner"
)

// upstreamInfo maps the fields we read from bitaxeorg/ESP-Miner's
// /api/system/info. Pointers distinguish an absent field from a real zero, so a
// firmware that omits something yields nil rather than a confident 0.
//
// Units as documented: hashRate in GH/s, voltage and coreVoltage in mV,
// current in mA.
type upstreamInfo struct {
	HashRate       *float64 `json:"hashRate"`
	HashRate1h     *float64 `json:"hashRate_1h"`
	ExpectedHash   *float64 `json:"expectedHashrate"`
	Power          *float64 `json:"power"`
	VoltageMv      *float64 `json:"voltage"`
	CurrentMa      *float64 `json:"current"`
	CoreVoltageMv  *float64 `json:"coreVoltage"`
	Frequency      *float64 `json:"frequency"`
	Temp           *float64 `json:"temp"`
	VrTemp         *float64 `json:"vrTemp"`
	FanRPM         *float64 `json:"fanrpm"`
	FanSpeed       *float64 `json:"fanspeed"`
	SharesAccepted *uint64  `json:"sharesAccepted"`
	SharesRejected *uint64  `json:"sharesRejected"`
	BestDiff       *float64 `json:"bestDiff"`
	BestSession    *float64 `json:"bestSessionDiff"`
	Uptime         float64  `json:"uptimeSeconds"`
	ASICModel      string   `json:"ASICModel"`
	Version        string   `json:"version"`
	StratumURL     string   `json:"stratumURL"`
	StratumUser    string   `json:"stratumUser"`
	Fallback       *int     `json:"isUsingFallbackStratum"`
}

func (c *Client) fetchUpstream(ctx context.Context) (miner.Snapshot, error) {
	var in upstreamInfo
	if err := c.getJSON(ctx, "/api/system/info", &in); err != nil {
		return miner.Snapshot{}, err
	}

	s := miner.Snapshot{
		Model:            in.ASICModel,
		Firmware:         in.Version,
		UptimeSeconds:    in.Uptime,
		HashrateAvg1hTHs: ghsToThs(in.HashRate1h),
		ExpectedHashTHs:  ghsToThs(in.ExpectedHash),
		PowerW:           in.Power,
		VoltageV:         mvToV(in.VoltageMv),
		CurrentA:         maToA(in.CurrentMa),
		CoreVoltageV:     mvToV(in.CoreVoltageMv),
		FreqMHz:          in.Frequency,
		ASICTempC:        in.Temp,
		VRMTempC:         in.VrTemp,
		SharesAccepted:   in.SharesAccepted,
		SharesRejected:   in.SharesRejected,
		BestDiff:         in.BestDiff,
		BestSessionDiff:  in.BestSession,
		PoolURL:          in.StratumURL,
		PoolUser:         in.StratumUser,
	}
	if in.HashRate != nil {
		s.HashrateTHs = *in.HashRate / 1000
	}
	if in.FanRPM != nil || in.FanSpeed != nil {
		s.Fans = []miner.Fan{{RPM: in.FanRPM, Percent: in.FanSpeed}}
	}
	if in.Fallback != nil {
		b := *in.Fallback != 0
		s.UsingFallback = &b
	}
	return s, nil
}
