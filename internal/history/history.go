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
	"sync"
	"time"
)

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

// Store holds the three rings.
type Store struct {
	mu    sync.RWMutex
	path  string
	rings map[string]*ring
}

// New creates a store. An empty path disables persistence.
func New(path string) *Store {
	return &Store{
		path: path,
		rings: map[string]*ring{
			"1h":  {Interval: 10 * time.Second, Cap: 360},
			"24h": {Interval: time.Minute, Cap: 1440},
			"7d":  {Interval: 10 * time.Minute, Cap: 1008},
		},
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

// persisted is the on-disk shape.
type persisted struct {
	Rings map[string][]Point
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
	return nil
}
