package axeos

import (
	"context"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner"
)

// nerdqaxeDashboard maps shufps/ESP-Miner-NerdQAxePlus /api/v2/dashboard.
// This document reports volts and amps directly, uses hashRate1m-style names
// without the underscore, and carries fans as an array and temps per ASIC.
type nerdqaxeDashboard struct {
	System struct {
		Uptime float64 `json:"uptime"`
	} `json:"system"`
	Performance struct {
		HashRate       *float64 `json:"hashRate"`
		HashRate1h     *float64 `json:"hashRate1h"`
		BestDiff       *float64 `json:"bestDiff"`
		BestSession    *float64 `json:"bestSessionDiff"`
		SharesAccepted *uint64  `json:"sharesAccepted"`
		SharesRejected *uint64  `json:"sharesRejected"`
		Frequency      *float64 `json:"frequency"`
		AsicCount      int      `json:"asicCount"`
		SmallCoreCount int      `json:"smallCoreCount"`
	} `json:"performance"`
	Power struct {
		Watts       *float64 `json:"watts"`
		Voltage     *float64 `json:"voltage"`  // volts
		CurrentA    *float64 `json:"currentA"` // amps
		CoreVoltage *float64 `json:"coreVoltageActual"`
	} `json:"power"`
	Thermal struct {
		AsicTemp  *float64  `json:"asicTemp"`
		VrTemp    *float64  `json:"vrTemp"`
		AsicTemps []float64 `json:"asicTemps"`
		Fans      []struct {
			Speed *float64 `json:"speed"`
			RPM   *float64 `json:"rpm"`
		} `json:"fans"`
	} `json:"thermal"`
	Stratum struct {
		Pools []struct {
			Host     string `json:"host"`
			User     string `json:"user"`
			Fallback *bool  `json:"isUsingFallback"`
		} `json:"pools"`
	} `json:"stratum"`
}

// nerdqaxeInfo is the fork's legacy /api/system/info, read only for the model
// name and firmware version, which the v2 dashboard does not carry.
type nerdqaxeInfo struct {
	ASICModel   string `json:"ASICModel"`
	DeviceModel string `json:"deviceModel"`
	Version     string `json:"version"`
}

func (c *Client) fetchNerdQAxe(ctx context.Context) (miner.Snapshot, error) {
	var d nerdqaxeDashboard
	if err := c.getJSON(ctx, "/api/v2/dashboard", &d); err != nil {
		return miner.Snapshot{}, err
	}

	s := miner.Snapshot{
		UptimeSeconds:    d.System.Uptime,
		HashrateAvg1hTHs: ghsToThs(d.Performance.HashRate1h),
		PowerW:           d.Power.Watts,
		VoltageV:         d.Power.Voltage,
		CurrentA:         d.Power.CurrentA,
		CoreVoltageV:     d.Power.CoreVoltage,
		FreqMHz:          d.Performance.Frequency,
		ASICTempC:        d.Thermal.AsicTemp,
		VRMTempC:         d.Thermal.VrTemp,
		SharesAccepted:   d.Performance.SharesAccepted,
		SharesRejected:   d.Performance.SharesRejected,
		BestDiff:         d.Performance.BestDiff,
		BestSessionDiff:  d.Performance.BestSession,
	}
	if d.Performance.HashRate != nil {
		s.HashrateTHs = *d.Performance.HashRate / 1000
	}
	for _, f := range d.Thermal.Fans {
		s.Fans = append(s.Fans, miner.Fan{RPM: f.RPM, Percent: f.Speed})
	}
	if len(d.Stratum.Pools) > 0 {
		p := d.Stratum.Pools[0]
		s.PoolURL = p.Host
		s.PoolUser = p.User
		s.UsingFallback = p.Fallback
	}

	// The model name and firmware live on the legacy endpoint. It is best
	// effort: if it fails, the dashboard shows one fewer label rather than
	// dropping the whole snapshot.
	var info nerdqaxeInfo
	if err := c.getJSON(ctx, "/api/system/info", &info); err == nil {
		s.Model = info.ASICModel
		s.Firmware = info.Version
	}
	return s, nil
}
