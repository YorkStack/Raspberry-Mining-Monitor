// Package miner defines the normalised view of an AxeOS miner.
//
// Every optional value is a pointer. A field the firmware does not report
// becomes nil and renders as an em dash, never as a confident zero. That is
// what lets a mixed fleet of firmware variants degrade gracefully.
package miner

import (
	"context"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/model"
)

// Variant identifies which AxeOS firmware family a device runs.
type Variant string

const (
	// VariantUpstream is bitaxeorg/ESP-Miner, the Bitaxe firmware.
	VariantUpstream Variant = "upstream"
	// VariantNerdQAxe is shufps/ESP-Miner-NerdQAxePlus, used by NerdOctaxe.
	VariantNerdQAxe Variant = "nerdqaxe"
	// VariantDemo is the synthetic miner used by demo mode.
	VariantDemo Variant = "demo"
)

// Fan is one cooling fan. Upstream firmware reports scalars and produces a
// single-element slice; the NerdQAxe fork reports an array directly.
type Fan struct {
	RPM     *float64 `json:"rpm,omitempty"`
	Percent *float64 `json:"percent,omitempty"`
}

// Snapshot is one miner's telemetry, normalised across firmware variants.
// Hashrate is always TH/s, voltage always volts, current always amps,
// regardless of the units the source firmware used.
type Snapshot struct {
	model.Source

	Name     string  `json:"name"`
	Model    string  `json:"model,omitempty"`
	Firmware string  `json:"firmware,omitempty"`
	Variant  Variant `json:"variant,omitempty"`

	UptimeSeconds float64 `json:"uptimeSeconds"`

	HashrateTHs      float64  `json:"hashrateThs"`
	HashrateAvg1hTHs *float64 `json:"hashrateAvg1hThs,omitempty"`
	ExpectedHashTHs  *float64 `json:"expectedHashThs,omitempty"`

	PowerW       *float64 `json:"powerW,omitempty"`
	VoltageV     *float64 `json:"voltageV,omitempty"`
	CurrentA     *float64 `json:"currentA,omitempty"`
	FreqMHz      *float64 `json:"freqMhz,omitempty"`
	CoreVoltageV *float64 `json:"coreVoltageV,omitempty"`

	ASICTempC *float64 `json:"asicTempC,omitempty"`
	VRMTempC  *float64 `json:"vrmTempC,omitempty"`
	Fans      []Fan    `json:"fans,omitempty"`

	SharesAccepted *uint64 `json:"sharesAccepted,omitempty"`
	SharesRejected *uint64 `json:"sharesRejected,omitempty"`

	BestDiff        *float64 `json:"bestDiff,omitempty"`
	BestSessionDiff *float64 `json:"bestSessionDiff,omitempty"`

	// PoolUser is the payout address the miner is configured with. It is
	// public information but is always truncated before it reaches the UI.
	PoolURL       string `json:"poolUrl,omitempty"`
	PoolUser      string `json:"-"`
	UsingFallback *bool  `json:"usingFallback,omitempty"`
}

// Collector fetches telemetry for a single miner.
type Collector interface {
	// Name is the configured display name of the miner.
	Name() string
	// Fetch returns a fresh snapshot. Implementations must respect ctx and
	// must never mutate the device.
	Fetch(ctx context.Context) (Snapshot, error)
}
