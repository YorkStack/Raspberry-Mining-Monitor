// Package collect runs one independent goroutine per data source.
//
// Each collector owns its own ticker, timeout and backoff. A dead miner cannot
// stall the pool collector, and an unreachable API cannot stall anything.
// Collectors only ever read: nothing here can mutate a miner.
package collect

import (
	"context"
	"log/slog"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/bitcoin"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/pool"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/state"
)

// maxBackoff caps how far any collector may slow down after repeated failures.
const maxBackoff = 60 * time.Second

// loop runs fetch on an interval that stretches while fetch keeps failing.
// It returns when ctx is done.
func loop(ctx context.Context, interval time.Duration, fetch func(context.Context) error) {
	b := NewBackoff(interval, maxBackoff)

	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		if err := fetch(ctx); err != nil {
			b.Failure()
		} else {
			b.Success()
		}

		timer.Reset(b.Interval())
	}
}

// RunMiner polls one miner until ctx is done.
func RunMiner(ctx context.Context, c miner.Collector, store *state.Store, interval, timeout time.Duration, log *slog.Logger) {
	name := c.Name()

	loop(ctx, interval, func(ctx context.Context) error {
		fetchCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		snap, err := c.Fetch(fetchCtx)
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			store.FailMiner(name, time.Now(), err.Error())
			log.Debug("miner fetch failed", "miner", name, "err", err)
			return err
		}
		store.SetMiner(name, snap)
		return nil
	})
}

// RunPool polls the solo pool until ctx is done. It hands the fetcher the
// current miner telemetry each cycle, which the router uses for provider
// detection and the generic adapter uses as its data source.
func RunPool(ctx context.Context, f pool.Fetcher, store *state.Store, interval, timeout time.Duration, log *slog.Logger) {
	loop(ctx, interval, func(ctx context.Context) error {
		fetchCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		miners := store.Miners()
		tel := make(map[string]miner.Snapshot, len(miners))
		for _, m := range miners {
			tel[m.Name] = m
		}

		snap, err := f.Fetch(fetchCtx, tel)
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			store.FailPool(time.Now(), err.Error())
			log.Debug("pool fetch failed", "pool", f.Name(), "err", err)
			return err
		}
		store.SetPool(snap)
		return nil
	})
}

// RunNetwork polls the Bitcoin data provider until ctx is done.
func RunNetwork(ctx context.Context, p bitcoin.Provider, store *state.Store, interval, timeout time.Duration, log *slog.Logger) {
	loop(ctx, interval, func(ctx context.Context) error {
		fetchCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		snap, err := p.Network(fetchCtx)
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			store.FailNetwork(time.Now(), err.Error())
			log.Debug("network fetch failed", "err", err)
			return err
		}
		store.SetNetwork(snap)
		return nil
	})
}
