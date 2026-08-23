// Package pool defines the solo-pool adapter interface and its normalised
// snapshot.
package pool

import (
	"context"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/model"
)

// Capabilities describes which metrics a given pool can actually supply.
// The UI hides what a pool cannot provide rather than showing a permanent
// placeholder that looks like a bug.
type Capabilities struct {
	// RejectedShares is false for both Public Pool and ckpool, so the
	// dashboard falls back to the miners' own reported counts.
	RejectedShares bool `json:"rejectedShares"`
	// PoolDifficulty is the per-worker assigned difficulty.
	PoolDifficulty bool `json:"poolDifficulty"`
	// ConnectionStatus is true only when the pool reports it explicitly
	// rather than leaving it to be inferred from the last share time.
	ConnectionStatus bool `json:"connectionStatus"`
	// BestEver is the all-time best share, as opposed to the session best.
	BestEver bool `json:"bestEver"`
}

// Worker is one miner as the pool sees it.
type Worker struct {
	Name           string    `json:"name"`
	MinerName      string    `json:"minerName,omitempty"`
	HashrateTHs    *float64  `json:"hashrateThs,omitempty"`
	BestDifficulty *float64  `json:"bestDifficulty,omitempty"`
	LastSeen       time.Time `json:"lastSeen,omitempty"`
}

// Snapshot is the pool-side view of the operation.
type Snapshot struct {
	model.Source

	Provider string       `json:"provider"`
	Caps     Capabilities `json:"caps"`

	Workers      []Worker `json:"workers,omitempty"`
	WorkersCount int      `json:"workersCount"`

	// BestDifficulty is the maximum across addresses, never a sum.
	BestDifficulty *float64 `json:"bestDifficulty,omitempty"`
	BestEver       *float64 `json:"bestEver,omitempty"`

	PoolHashrateTHs *float64   `json:"poolHashrateThs,omitempty"`
	PoolMiners      *int       `json:"poolMiners,omitempty"`
	BlocksFound     *int       `json:"blocksFound,omitempty"`
	LastShare       *time.Time `json:"lastShare,omitempty"`
}

// Adapter fetches pool-side statistics.
type Adapter interface {
	Name() string
	Capabilities() Capabilities
	Fetch(ctx context.Context) (Snapshot, error)
}
