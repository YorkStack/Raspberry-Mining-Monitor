// Package bitcoin defines the network-data provider interface and its
// normalised snapshot.
package bitcoin

import (
	"context"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/model"
)

// SourceKind drives the NETWORK SOURCE badge on the dashboard. It comes from
// the provider itself rather than from config, so the badge cannot disagree
// with what is actually being queried.
type SourceKind string

const (
	// SourcePublic is a public REST API such as mempool.space.
	SourcePublic SourceKind = "public"
	// SourceLocalNode is a local Bitcoin Core RPC. Not wired up in the MVP.
	SourceLocalNode SourceKind = "local-node"
	// SourceDemo is the synthetic provider used by demo mode.
	SourceDemo SourceKind = "demo"
)

// Label is the short uppercase form shown on the dashboard badge.
func (k SourceKind) Label() string {
	switch k {
	case SourceLocalNode:
		return "LOCAL NODE"
	case SourceDemo:
		return "DEMO"
	default:
		return "PUBLIC"
	}
}

// Snapshot is the Bitcoin network state the dashboard needs.
type Snapshot struct {
	model.Source

	Kind SourceKind `json:"kind"`

	Height            uint32    `json:"height"`
	Difficulty        float64   `json:"difficulty"`
	NetworkHashrateHs float64   `json:"networkHashrateHs"`
	LastBlockTime     time.Time `json:"lastBlockTime"`

	// SubsidyBTC is computed locally from Height, not fetched.
	SubsidyBTC float64 `json:"subsidyBtc"`

	// PriceEUR is the current BTC price in euro, 0 when unavailable.
	PriceEUR float64 `json:"priceEur"`

	NextHalvingHeight uint32 `json:"nextHalvingHeight"`

	NextRetargetHeight     uint32  `json:"nextRetargetHeight,omitempty"`
	NextRetargetChangePct  float64 `json:"nextRetargetChangePct"`
	NextRetargetETASeconds float64 `json:"nextRetargetEtaSeconds,omitempty"`
}

// Provider supplies Bitcoin network data.
type Provider interface {
	SourceKind() SourceKind
	Network(ctx context.Context) (Snapshot, error)
}
