// Package state holds the latest snapshot from every collector and notifies
// subscribers when anything changes.
//
// Collectors never block here. They take a short write lock, store a snapshot
// and return. Notification is best-effort: a browser that stops reading gets
// its update dropped rather than stalling the collectors behind it.
package state

import (
	"sync"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/bitcoin"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/pool"
)

// Store is the in-memory snapshot store.
type Store struct {
	mu sync.RWMutex

	// order preserves the configured miner order so tiles never move.
	order  []string
	miners map[string]miner.Snapshot

	pool    pool.Snapshot
	network bitcoin.Snapshot

	subMu sync.Mutex
	subs  map[int]chan struct{}
	nexID int
}

// New creates a store with one placeholder per configured miner name, so every
// miner has a tile from the first frame even before it has answered.
func New(minerNames []string) *Store {
	s := &Store{
		order:  append([]string(nil), minerNames...),
		miners: make(map[string]miner.Snapshot, len(minerNames)),
		subs:   make(map[int]chan struct{}),
	}
	for _, n := range minerNames {
		s.miners[n] = miner.Snapshot{Name: n}
	}
	return s
}

// Reconcile updates the set of miners to the given names, in order. Existing
// miners keep their last snapshot; new names get a placeholder; dropped names
// are removed. Used when the operator edits the miner list at runtime.
func (s *Store) Reconcile(names []string) {
	s.mu.Lock()
	next := make(map[string]miner.Snapshot, len(names))
	for _, n := range names {
		if cur, ok := s.miners[n]; ok {
			next[n] = cur
		} else {
			next[n] = miner.Snapshot{Name: n}
		}
	}
	s.order = append([]string(nil), names...)
	s.miners = next
	s.mu.Unlock()
	s.notify()
}

// SetMiner stores a fresh snapshot for a configured miner. Unknown names are
// ignored so a misconfigured collector cannot add phantom tiles.
func (s *Store) SetMiner(name string, snap miner.Snapshot) {
	s.mu.Lock()
	if _, known := s.miners[name]; !known {
		s.mu.Unlock()
		return
	}
	snap.Name = name
	s.miners[name] = snap
	s.mu.Unlock()
	s.notify()
}

// FailMiner records a failed fetch without discarding the last good data.
func (s *Store) FailMiner(name string, now time.Time, reason string) {
	s.mu.Lock()
	cur, known := s.miners[name]
	if !known {
		s.mu.Unlock()
		return
	}
	cur.Fail(now, reason)
	s.miners[name] = cur
	s.mu.Unlock()
	s.notify()
}

// Miners returns a copy of every miner snapshot in configured order.
func (s *Store) Miners() []miner.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]miner.Snapshot, 0, len(s.order))
	for _, n := range s.order {
		out = append(out, s.miners[n])
	}
	return out
}

// SetPool stores a fresh pool snapshot.
func (s *Store) SetPool(p pool.Snapshot) {
	s.mu.Lock()
	s.pool = p
	s.mu.Unlock()
	s.notify()
}

// FailPool records a failed pool fetch.
func (s *Store) FailPool(now time.Time, reason string) {
	s.mu.Lock()
	s.pool.Fail(now, reason)
	s.mu.Unlock()
	s.notify()
}

// Pool returns the latest pool snapshot.
func (s *Store) Pool() pool.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pool
}

// SetNetwork stores a fresh Bitcoin network snapshot.
func (s *Store) SetNetwork(n bitcoin.Snapshot) {
	s.mu.Lock()
	s.network = n
	s.mu.Unlock()
	s.notify()
}

// FailNetwork records a failed network fetch.
func (s *Store) FailNetwork(now time.Time, reason string) {
	s.mu.Lock()
	s.network.Fail(now, reason)
	s.mu.Unlock()
	s.notify()
}

// Network returns the latest Bitcoin network snapshot.
func (s *Store) Network() bitcoin.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.network
}

// Subscribe returns a channel that receives a token whenever the store
// changes, plus a cancel function. The channel has a one-slot buffer and
// updates are coalesced, so a subscriber that falls behind simply gets the
// next change rather than a backlog.
//
// cancel is safe to call more than once.
func (s *Store) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)

	s.subMu.Lock()
	id := s.nexID
	s.nexID++
	s.subs[id] = ch
	s.subMu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			s.subMu.Lock()
			delete(s.subs, id)
			s.subMu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

func (s *Store) notify() {
	s.subMu.Lock()
	defer s.subMu.Unlock()

	for _, ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default:
			// Already has a pending token. Coalesce.
		}
	}
}
