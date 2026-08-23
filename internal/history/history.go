// Package history keeps a lightweight rolling record of fleet totals over 1
// hour, 24 hours and 7 days.
//
// It is dependency-free by design: three in-RAM ring buffers, each sampled at
// its own cadence, persisted to one small gob file periodically and on
// shutdown. That keeps SD-card writes to a trickle and survives a restart,
// without pulling in a database. History failure is never fatal to the live
// dashboard.
package history

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// maxHistoryMiners bounds per-miner history so the RAM stays predictable on the
// 1 GB Pi. Each tracked miner holds three rings (~2,800 points total).
const maxHistoryMiners = 16

// Sample is one recorded moment of fleet totals plus the BTC price.
type Sample struct {
	Hashrate float64 // TH/s
	Power    float64 // W
	Price    float64 // EUR
}

// Point is a stored sample with its timestamp (unix seconds).
type Point struct {
	T        int64   `json:"t"`
	Hashrate float64 `json:"hashrate"`
	Power    float64 `json:"power"`
	Price    float64 `json:"price"`
}

type ring struct {
	Interval time.Duration
	Cap      int
	Points   []Point
	last     time.Time
}

func (r *ring) add(now time.Time, s Sample) {
	if !r.last.IsZero() && now.Sub(r.last) < r.Interval {
		return
	}
	r.last = now
	r.Points = append(r.Points, Point{T: now.Unix(), Hashrate: s.Hashrate, Power: s.Power, Price: s.Price})
	if len(r.Points) > r.Cap {
		r.Points = r.Points[len(r.Points)-r.Cap:]
	}
}

// MinerSample is one recorded moment for a single miner.
type MinerSample struct {
	Hashrate float64 // TH/s
	Power    float64 // W (0 when unknown)
	Temp     float64 // ASIC °C (0 when unknown)
}

// MinerPoint is a stored per-miner sample with its timestamp (unix seconds).
type MinerPoint struct {
	T        int64   `json:"t"`
	Hashrate float64 `json:"hashrate"`
	Power    float64 `json:"power"`
	Temp     float64 `json:"temp"`
}

type minerRing struct {
	Interval time.Duration
	Cap      int
	Points   []MinerPoint
	last     time.Time
}

func (r *minerRing) add(now time.Time, s MinerSample) {
	if !r.last.IsZero() && now.Sub(r.last) < r.Interval {
		return
	}
	r.last = now
	r.Points = append(r.Points, MinerPoint{T: now.Unix(), Hashrate: s.Hashrate, Power: s.Power, Temp: s.Temp})
	if len(r.Points) > r.Cap {
		r.Points = r.Points[len(r.Points)-r.Cap:]
	}
}

func newFleetRings() map[string]*ring {
	return map[string]*ring{
		"1h":  {Interval: 10 * time.Second, Cap: 360},
		"24h": {Interval: time.Minute, Cap: 1440},
		"7d":  {Interval: 10 * time.Minute, Cap: 1008},
	}
}

func newMinerRings() map[string]*minerRing {
	return map[string]*minerRing{
		"1h":  {Interval: 10 * time.Second, Cap: 360},
		"24h": {Interval: time.Minute, Cap: 1440},
		"7d":  {Interval: 10 * time.Minute, Cap: 1008},
	}
}

// Store holds the fleet rings plus per-miner rings.
type Store struct {
	mu         sync.RWMutex
	path       string
	rings      map[string]*ring
	minerRings map[string]map[string]*minerRing // miner name -> range key -> ring
}

// New creates a store. An empty path disables persistence.
func New(path string) *Store {
	return &Store{
		path:       path,
		rings:      newFleetRings(),
		minerRings: map[string]map[string]*minerRing{},
	}
}

// Record offers a sample to every ring; each keeps it only if its cadence is
// due.
func (s *Store) Record(now time.Time, sample Sample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rings {
		r.add(now, sample)
	}
}

// Query returns a copy of the points for a range key (1h, 24h, 7d).
func (s *Store) Query(rangeKey string) []Point {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.rings[rangeKey]
	if !ok {
		return nil
	}
	return append([]Point(nil), r.Points...)
}

// RecordMiners offers a sample per miner to that miner's rings. A miner is
// tracked lazily on first sight, up to maxHistoryMiners.
func (s *Store) RecordMiners(now time.Time, samples map[string]MinerSample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, sample := range samples {
		rings, ok := s.minerRings[name]
		if !ok {
			if len(s.minerRings) >= maxHistoryMiners {
				continue
			}
			rings = newMinerRings()
			s.minerRings[name] = rings
		}
		for _, r := range rings {
			r.add(now, sample)
		}
	}
}

// QueryMiner returns a copy of one miner's points for a range key.
func (s *Store) QueryMiner(name, rangeKey string) []MinerPoint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rings, ok := s.minerRings[name]
	if !ok {
		return nil
	}
	r, ok := rings[rangeKey]
	if !ok {
		return nil
	}
	return append([]MinerPoint(nil), r.Points...)
}

// MinerNames returns the miners that have history, sorted.
func (s *Store) MinerNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.minerRings))
	for name := range s.minerRings {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// persisted is the on-disk shape. MinerRings was added later; an older file
// without it decodes fine and simply leaves per-miner history empty.
type persisted struct {
	Rings      map[string][]Point
	MinerRings map[string]map[string][]MinerPoint
}

// Save writes all rings to the gob file atomically. A no-op without a path.
func (s *Store) Save() error {
	if s.path == "" {
		return nil
	}
	s.mu.RLock()
	p := persisted{Rings: make(map[string][]Point, len(s.rings))}
	for k, r := range s.rings {
		p.Rings[k] = append([]Point(nil), r.Points...)
	}
	p.MinerRings = make(map[string]map[string][]MinerPoint, len(s.minerRings))
	for name, rings := range s.minerRings {
		m := make(map[string][]MinerPoint, len(rings))
		for k, r := range rings {
			m[k] = append([]MinerPoint(nil), r.Points...)
		}
		p.MinerRings[name] = m
	}
	s.mu.RUnlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".history-*.gob")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := gob.NewEncoder(tmp).Encode(p); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

// Load restores rings from the gob file. A missing file is not an error.
func (s *Store) Load() error {
	if s.path == "" {
		return nil
	}
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	var p persisted
	if err := gob.NewDecoder(f).Decode(&p); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, pts := range p.Rings {
		if r, ok := s.rings[k]; ok {
			if len(pts) > r.Cap {
				pts = pts[len(pts)-r.Cap:]
			}
			r.Points = pts
		}
	}
	for name, rk := range p.MinerRings {
		if len(s.minerRings) >= maxHistoryMiners {
			break
		}
		rings := newMinerRings()
		for k, pts := range rk {
			if r, ok := rings[k]; ok {
				if len(pts) > r.Cap {
					pts = pts[len(pts)-r.Cap:]
				}
				r.Points = pts
			}
		}
		s.minerRings[name] = rings
	}
	return nil
}
