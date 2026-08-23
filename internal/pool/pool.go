// Package pool defines a provider-agnostic interface for solo-pool statistics
// and a normalised snapshot.
//
// The dashboard and the rest of the application never contain provider-specific
// logic. Each pool has its own adapter behind the Provider interface; a Router
// groups miners by their detected (or overridden) provider and merges the
// per-provider results into one Snapshot. Fields a provider cannot supply are
// left nil and reported as unavailable through Capabilities, never faked as 0.
package pool

import (
	"context"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/model"
)

// Field names a single pool metric. A provider advertises which fields it can
// supply through Capabilities, so the UI hides what is genuinely unavailable
// instead of showing a permanent zero that looks like a bug.
type Field string

const (
	FieldHashrate       Field = "hashrate"
	FieldAcceptedShares Field = "accepted_shares"
	FieldRejectedShares Field = "rejected_shares"
	FieldBestShare      Field = "best_share"
	FieldBestEver       Field = "best_ever"
	FieldLastShare      Field = "last_share"
	FieldActiveWorkers  Field = "active_workers"
	FieldPoolDifficulty Field = "pool_difficulty"
	FieldBlocksFound    Field = "blocks_found"
)

// Capabilities is the set of fields a provider can supply.
type Capabilities map[Field]bool

// Caps builds a capability set from a list of supported fields.
func Caps(fields ...Field) Capabilities {
	c := make(Capabilities, len(fields))
	for _, f := range fields {
		c[f] = true
	}
	return c
}

// Has reports whether the field is supported.
func (c Capabilities) Has(f Field) bool { return c[f] }

// Union merges capability sets; a field is present if any set has it. Used by
// the Router when several providers contribute to one merged snapshot.
func (c Capabilities) Union(other Capabilities) Capabilities {
	out := make(Capabilities, len(c)+len(other))
	for f := range c {
		out[f] = true
	}
	for f := range other {
		out[f] = true
	}
	return out
}

// Worker is one miner as a pool sees it.
type Worker struct {
	Name           string    `json:"name"`
	MinerName      string    `json:"minerName,omitempty"`
	Provider       string    `json:"provider,omitempty"`
	HashrateTHs    *float64  `json:"hashrateThs,omitempty"`
	BestDifficulty *float64  `json:"bestDifficulty,omitempty"`
	LastSeen       time.Time `json:"lastSeen,omitempty"`
}

// Snapshot is the normalised, provider-agnostic pool view.
type Snapshot struct {
	model.Source

	// Provider is the source provider, or "mixed" when more than one
	// contributed to a merged snapshot.
	Provider string       `json:"provider"`
	Caps     Capabilities `json:"caps"`

	Workers      []Worker `json:"workers,omitempty"`
	WorkersCount int      `json:"workersCount"`
	// ActiveWorkers is the count currently hashing, when the provider reports
	// it separately from the roster size.
	ActiveWorkers *int `json:"activeWorkers,omitempty"`

	// HashrateTHs is the operator-side hashrate the pool attributes to us.
	HashrateTHs    *float64 `json:"hashrateThs,omitempty"`
	AcceptedShares *uint64  `json:"acceptedShares,omitempty"`
	RejectedShares *uint64  `json:"rejectedShares,omitempty"`

	// BestDifficulty is the best share this session; BestEver is all-time. Both
	// are the maximum across addresses, never a sum.
	BestDifficulty *float64 `json:"bestDifficulty,omitempty"`
	BestEver       *float64 `json:"bestEver,omitempty"`

	// PoolDifficulty is the per-worker assigned difficulty.
	PoolDifficulty *float64   `json:"poolDifficulty,omitempty"`
	LastShare      *time.Time `json:"lastShare,omitempty"`
	BlocksFound    *int       `json:"blocksFound,omitempty"`

	// Pool-wide figures, when available. Best effort, not per-operator.
	PoolHashrateTHs *float64 `json:"poolHashrateThs,omitempty"`
	PoolMiners      *int     `json:"poolMiners,omitempty"`
}

// Miner is one configured miner handed to a provider for a fetch: its name, the
// payout address it mines to, and the latest telemetry (for the generic adapter
// and for provider detection).
type Miner struct {
	Name         string
	Address      string
	Telemetry    miner.Snapshot
	HasTelemetry bool
}

// Input carries the miners a provider should report on for one fetch.
type Input struct {
	Miners []Miner
}

// Provider is a single pool's statistics adapter. Implementations are
// independent and never referenced by name outside their own package and the
// Router.
type Provider interface {
	Name() string
	Capabilities() Capabilities
	Fetch(ctx context.Context, in Input) (Snapshot, error)
}

// Fetcher is what the collector drives: something that produces one merged
// Snapshot from the current miner telemetry. Both Router and the demo pool
// implement it.
type Fetcher interface {
	Name() string
	Capabilities() Capabilities
	Fetch(ctx context.Context, telemetry map[string]miner.Snapshot) (Snapshot, error)
}
