// Package minercfg is the operator-editable, persisted miner and provider
// configuration. The admin UI writes here, and a live-reload manager rebuilds
// the collectors when it changes, so miners can be prepared before the hardware
// is on the network.
//
// This holds addresses, so like the rest of the runtime config it lives in a
// 0600 file and never in git.
package minercfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultInterval / DefaultTimeout apply when a spec omits them.
const (
	DefaultInterval = 2 * time.Second
	DefaultTimeout  = 2 * time.Second
	minInterval     = time.Second
)

var knownTypes = map[string]bool{"axeos": true, "demo": true}

// Spec is one editable miner. Demo nominal fields are carried through so a
// simulated miner survives a round-trip, but the UI only edits real ones.
type Spec struct {
	Name          string        `json:"name"`
	Type          string        `json:"type"`
	Host          string        `json:"host"`
	PayoutAddress string        `json:"payoutAddress"`
	// PoolProvider overrides provider detection for this miner (empty = use the
	// global default / auto-detect).
	PoolProvider string `json:"poolProvider,omitempty"`
	// Token is an optional Bearer token for miners behind the monitoring
	// security contract. It is persisted here but never returned by the admin
	// API; see httpapi.apiMiner.
	Token        string        `json:"token,omitempty"`
	Interval     time.Duration `json:"interval"`
	Timeout      time.Duration `json:"timeout"`

	NominalTHs   float64 `json:"nominalThs,omitempty"`
	NominalW     float64 `json:"nominalW,omitempty"`
	NominalTempC float64 `json:"nominalTempC,omitempty"`
	Model        string  `json:"model,omitempty"`
	Fans         int     `json:"fans,omitempty"`
}

// Providers holds the network-data and pool base URLs.
type Providers struct {
	BitcoinBaseURL string `json:"bitcoinBaseUrl"`
	PoolBaseURL    string `json:"poolBaseUrl"`
}

type file struct {
	Miners    []Spec    `json:"miners"`
	Providers Providers `json:"providers"`
}

// Store is the persisted, thread-safe config.
type Store struct {
	mu        sync.RWMutex
	path      string
	miners    []Spec
	providers Providers
	loaded    bool
}

// New creates a store with fallback providers used until seeded or loaded.
func New(path string, fallback Providers) *Store {
	return &Store{path: path, providers: fallback}
}

// Path is the backing file.
func (s *Store) Path() string { return s.path }

func validateSpecs(in []Spec) ([]Spec, error) {
	seen := make(map[string]bool, len(in))
	out := make([]Spec, 0, len(in))
	for i, m := range in {
		if m.Name == "" {
			return nil, fmt.Errorf("miner %d has no name", i)
		}
		if seen[m.Name] {
			return nil, fmt.Errorf("duplicate miner name %q", m.Name)
		}
		seen[m.Name] = true
		if m.Type == "" {
			m.Type = "axeos"
		}
		if !knownTypes[m.Type] {
			return nil, fmt.Errorf("miner %q has unknown type %q", m.Name, m.Type)
		}
		if m.Type == "axeos" && m.Host == "" {
			return nil, fmt.Errorf("miner %q needs a host or IP", m.Name)
		}
		if m.Interval == 0 {
			m.Interval = DefaultInterval
		}
		if m.Interval < minInterval {
			return nil, fmt.Errorf("miner %q interval %v is below the %v minimum", m.Name, m.Interval, minInterval)
		}
		if m.Timeout == 0 {
			m.Timeout = DefaultTimeout
		}
		out = append(out, m)
	}
	return out, nil
}

func validateProviders(p Providers) error {
	for label, raw := range map[string]string{"bitcoin": p.BitcoinBaseURL, "pool": p.PoolBaseURL} {
		if raw == "" {
			continue // empty means "keep using the built-in default"
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%s URL %q is not a valid http(s) URL", label, raw)
		}
	}
	return nil
}

// Miners returns a copy of the configured miners.
func (s *Store) Miners() []Spec {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Spec(nil), s.miners...)
}

// Names returns the miner names in order.
func (s *Store) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.miners))
	for i, m := range s.miners {
		out[i] = m.Name
	}
	return out
}

// Providers returns the current provider URLs.
func (s *Store) Providers() Providers {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.providers
}

// Replace validates, stores and persists a new miner list.
func (s *Store) Replace(in []Spec) error {
	clean, err := validateSpecs(in)
	if err != nil {
		return fmt.Errorf("minercfg: %w", err)
	}
	s.mu.Lock()
	s.miners = clean
	s.mu.Unlock()
	return s.save()
}

// SetProviders validates, stores and persists the provider URLs.
func (s *Store) SetProviders(p Providers) error {
	if err := validateProviders(p); err != nil {
		return fmt.Errorf("minercfg: %w", err)
	}
	s.mu.Lock()
	s.providers = p
	s.mu.Unlock()
	return s.save()
}

// SeedIfEmpty populates a fresh store (no file yet, nothing loaded) with the
// given defaults. It never overwrites an existing config.
func (s *Store) SeedIfEmpty(miners []Spec, p Providers) error {
	if err := s.Load(); err == nil && s.loaded {
		return nil // already have a config on disk
	}
	clean, err := validateSpecs(miners)
	if err != nil {
		return fmt.Errorf("minercfg: seed: %w", err)
	}
	s.mu.Lock()
	s.miners = clean
	s.providers = p
	s.mu.Unlock()
	return s.save()
}

// Load reads the file. A missing file is not an error but leaves loaded=false.
func (s *Store) Load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("minercfg: read %s: %w", s.path, err)
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("minercfg: parse %s: %w", s.path, err)
	}
	clean, err := validateSpecs(f.Miners)
	if err != nil {
		return fmt.Errorf("minercfg: %s: %w", s.path, err)
	}
	s.mu.Lock()
	s.miners = clean
	if f.Providers.BitcoinBaseURL != "" || f.Providers.PoolBaseURL != "" {
		s.providers = f.Providers
	}
	s.loaded = true
	s.mu.Unlock()
	return nil
}

func (s *Store) save() error {
	s.mu.RLock()
	f := file{Miners: s.miners, Providers: s.providers}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("minercfg: encode: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("minercfg: mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".miners-*.json")
	if err != nil {
		return fmt.Errorf("minercfg: temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
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
	s.mu.Lock()
	s.loaded = true
	s.mu.Unlock()
	return os.Rename(tmpName, s.path)
}
