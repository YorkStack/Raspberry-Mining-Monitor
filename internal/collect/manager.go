package collect

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/bitcoin"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/minercfg"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/pool"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/state"
)

// Factories build concrete collectors from config. Injecting them keeps this
// package free of the concrete adapter imports and makes the manager testable.
type Factories struct {
	Miner   func(minercfg.Spec) miner.Collector
	Pool    func(miners []minercfg.Spec, p minercfg.Providers) pool.Fetcher // nil if no pool
	Bitcoin func(p minercfg.Providers) bitcoin.Provider                     // nil if none
}

// Manager owns the running collectors and rebuilds them on Reload, so the miner
// list and provider URLs can change at runtime without a process restart.
type Manager struct {
	store     *state.Store
	log       *slog.Logger
	factories Factories

	poolInterval time.Duration
	poolTimeout  time.Duration
	netInterval  time.Duration
	netTimeout   time.Duration

	base context.Context

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// ManagerOptions configures a Manager.
type ManagerOptions struct {
	Store        *state.Store
	Log          *slog.Logger
	Factories    Factories
	PoolInterval time.Duration
	PoolTimeout  time.Duration
	NetInterval  time.Duration
	NetTimeout   time.Duration
}

// NewManager creates a manager bound to a base context. Cancelling base stops
// everything.
func NewManager(base context.Context, o ManagerOptions) *Manager {
	return &Manager{
		store:        o.Store,
		log:          o.Log,
		factories:    o.Factories,
		poolInterval: o.PoolInterval,
		poolTimeout:  o.PoolTimeout,
		netInterval:  o.NetInterval,
		netTimeout:   o.NetTimeout,
		base:         base,
	}
}

// Reload stops the current collectors and starts a fresh set from the given
// config. Safe to call repeatedly.
func (m *Manager) Reload(specs []minercfg.Spec, providers minercfg.Providers) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop the previous generation and wait for its goroutines to exit.
	if m.cancel != nil {
		m.cancel()
		m.wg.Wait()
	}

	// Bring the snapshot store in line with the new miner set.
	names := make([]string, len(specs))
	for i, s := range specs {
		names[i] = s.Name
	}
	m.store.Reconcile(names)

	if m.base.Err() != nil {
		return // shutting down
	}

	ctx, cancel := context.WithCancel(m.base)
	m.cancel = cancel

	for _, spec := range specs {
		c := m.factories.Miner(spec)
		if c == nil {
			continue
		}
		m.wg.Add(1)
		go func(c miner.Collector, interval, timeout time.Duration) {
			defer m.wg.Done()
			RunMiner(ctx, c, m.store, interval, timeout, m.log)
		}(c, spec.Interval, spec.Timeout)
	}

	if m.factories.Pool != nil {
		if f := m.factories.Pool(specs, providers); f != nil {
			m.wg.Add(1)
			go func() {
				defer m.wg.Done()
				RunPool(ctx, f, m.store, m.poolInterval, m.poolTimeout, m.log)
			}()
		}
	}

	if m.factories.Bitcoin != nil {
		if p := m.factories.Bitcoin(providers); p != nil {
			m.wg.Add(1)
			go func() {
				defer m.wg.Done()
				RunNetwork(ctx, p, m.store, m.netInterval, m.netTimeout, m.log)
			}()
		}
	}

	m.log.Info("collectors (re)loaded", "miners", len(specs))
}

// Wait blocks until all collectors from the current generation have stopped.
// The base context must be cancelled first.
func (m *Manager) Wait() {
	m.mu.Lock()
	c := m.cancel
	m.mu.Unlock()
	if c != nil {
		c()
	}
	m.wg.Wait()
}
