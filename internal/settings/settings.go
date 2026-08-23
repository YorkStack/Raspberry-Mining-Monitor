// Package settings holds the operator-adjustable display thresholds.
//
// These are presentation thresholds only. Nothing here is ever sent to a
// miner: changing a warning band changes the colour of a badge and nothing
// else. The monitor still has no write path to the hardware.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
)

// Temperature values outside this range are a typo rather than a setting.
const (
	minTempC = 0.0
	maxTempC = 150.0
)

// Thresholds is one miner's warning band, in Celsius.
type Thresholds struct {
	ASICWarnC float64 `json:"asicWarnC"`
	ASICCritC float64 `json:"asicCritC"`
	VRMWarnC  float64 `json:"vrmWarnC"`
	VRMCritC  float64 `json:"vrmCritC"`
}

// Validate reports whether the band is usable.
//
// Both AxeOS variants trigger their own thermal protection at 70 C, so a
// critical threshold above that would colour the tile red only after the
// firmware had already intervened.
func (t Thresholds) Validate() error {
	pairs := []struct {
		name       string
		warn, crit float64
	}{
		{"ASIC", t.ASICWarnC, t.ASICCritC},
		{"VRM", t.VRMWarnC, t.VRMCritC},
	}
	for _, p := range pairs {
		for _, v := range []float64{p.warn, p.crit} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("%s threshold is not a number", p.name)
			}
			if v < minTempC || v > maxTempC {
				return fmt.Errorf("%s threshold %.1f is outside %.0f to %.0f C", p.name, v, minTempC, maxTempC)
			}
		}
		if p.crit <= p.warn {
			return fmt.Errorf("%s crit %.1f must be above warn %.1f", p.name, p.crit, p.warn)
		}
	}
	return nil
}

// file is the on-disk shape.
type file struct {
	Miners map[string]Thresholds `json:"miners"`
	// Disabled lists miners the operator has switched off in monitoring.
	// Absence means enabled, so a fresh install monitors everything.
	Disabled []string `json:"disabled,omitempty"`
	// Icons maps a miner name to its chosen animated mark. Absence means the
	// dashboard auto-picks one from the miner name.
	Icons map[string]string `json:"icons,omitempty"`
	// Screensaver is the burn-in saver mode and idle timeout.
	Screensaver *Screensaver `json:"screensaver,omitempty"`
}

// Screensaver is the burn-in protection setting.
type Screensaver struct {
	// Mode is "off", "floating" (a drifting stats panel) or "blank".
	Mode    string `json:"mode"`
	Minutes int    `json:"minutes"`
}

// ScreensaverModes are the accepted modes.
var ScreensaverModes = map[string]bool{"off": true, "floating": true, "blank": true}

// Store holds the fleet default plus per-miner overrides.
type Store struct {
	mu            sync.RWMutex
	path          string
	def           Thresholds
	perMiner      map[string]Thresholds
	disabled      map[string]bool
	icons         map[string]string
	saver         Screensaver
	saverFromFile bool
}

// New creates a store backed by path, falling back to def for any miner
// without an override.
func New(path string, def Thresholds) *Store {
	return &Store{
		path:     path,
		def:      def,
		perMiner: make(map[string]Thresholds),
		disabled: make(map[string]bool),
		icons:    make(map[string]string),
		saver:    Screensaver{Mode: "floating", Minutes: 15},
	}
}

// Path is where overrides are persisted.
func (s *Store) Path() string { return s.path }

// Default is the fleet-wide band.
func (s *Store) Default() Thresholds {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.def
}

// For returns the band that applies to one miner.
func (s *Store) For(miner string) Thresholds {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t, ok := s.perMiner[miner]; ok {
		return t
	}
	return s.def
}

// Overrides returns a copy of the per-miner overrides.
func (s *Store) Overrides() map[string]Thresholds {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]Thresholds, len(s.perMiner))
	for k, v := range s.perMiner {
		out[k] = v
	}
	return out
}

// Set validates and stores an override. An invalid band leaves the store
// untouched.
func (s *Store) Set(miner string, t Thresholds) error {
	if miner == "" {
		return errors.New("settings: miner name must not be empty")
	}
	if err := t.Validate(); err != nil {
		return fmt.Errorf("settings: %s: %w", miner, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.perMiner[miner] = t
	return nil
}

// Reset drops a miner's override so it falls back to the default.
func (s *Store) Reset(miner string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.perMiner, miner)
}

// Enabled reports whether a miner is switched on in monitoring. Unknown or
// unlisted miners are enabled by default.
func (s *Store) Enabled(miner string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.disabled[miner]
}

// SetEnabled switches a miner on or off in monitoring.
func (s *Store) SetEnabled(miner string, enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if enabled {
		delete(s.disabled, miner)
	} else {
		s.disabled[miner] = true
	}
}

// Disabled returns the set of switched-off miners as a copy.
func (s *Store) Disabled() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]bool, len(s.disabled))
	for k := range s.disabled {
		out[k] = true
	}
	return out
}

// Icon returns a miner's chosen mark, or "" to let the UI auto-pick.
func (s *Store) Icon(miner string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.icons[miner]
}

// SetIcon records a miner's chosen mark; an empty id clears the override.
func (s *Store) SetIcon(miner, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		delete(s.icons, miner)
	} else {
		s.icons[miner] = id
	}
}

// Icons returns a copy of the per-miner mark choices.
func (s *Store) Icons() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.icons))
	for k, v := range s.icons {
		out[k] = v
	}
	return out
}

// ScreensaverCfg returns the current burn-in saver setting.
func (s *Store) ScreensaverCfg() Screensaver {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saver
}

// SetScreensaver validates and stores the burn-in saver setting.
func (s *Store) SetScreensaver(cfg Screensaver) error {
	if !ScreensaverModes[cfg.Mode] {
		return fmt.Errorf("settings: unknown screensaver mode %q", cfg.Mode)
	}
	if cfg.Minutes < 0 || cfg.Minutes > 240 {
		return fmt.Errorf("settings: screensaver minutes %d out of range 0..240", cfg.Minutes)
	}
	s.mu.Lock()
	s.saver = cfg
	s.mu.Unlock()
	return nil
}

// SetScreensaverDefault sets the initial saver config without persisting, used
// to seed from the static config on startup.
func (s *Store) SetScreensaverDefault(cfg Screensaver) {
	if !ScreensaverModes[cfg.Mode] {
		return
	}
	s.mu.Lock()
	if _, loaded := s.saverLoaded(); !loaded {
		s.saver = cfg
	}
	s.mu.Unlock()
}

func (s *Store) saverLoaded() (Screensaver, bool) { return s.saver, s.saverFromFile }

// Load reads the overrides file. A missing file is the normal first-run state
// and is not an error. Anything unreadable or invalid is reported, and the
// in-memory defaults are left usable so a bad file cannot blank the dashboard.
func (s *Store) Load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("settings: read %s: %w", s.path, err)
	}

	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("settings: parse %s: %w", s.path, err)
	}
	for name, t := range f.Miners {
		if name == "" {
			return fmt.Errorf("settings: %s contains an entry with no miner name", s.path)
		}
		if err := t.Validate(); err != nil {
			return fmt.Errorf("settings: %s: %s: %w", s.path, name, err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.perMiner = make(map[string]Thresholds, len(f.Miners))
	for name, t := range f.Miners {
		s.perMiner[name] = t
	}
	s.disabled = make(map[string]bool, len(f.Disabled))
	for _, name := range f.Disabled {
		s.disabled[name] = true
	}
	s.icons = make(map[string]string, len(f.Icons))
	for k, v := range f.Icons {
		s.icons[k] = v
	}
	if f.Screensaver != nil && ScreensaverModes[f.Screensaver.Mode] {
		s.saver = *f.Screensaver
		s.saverFromFile = true
	}
	return nil
}

// Save writes the overrides atomically: a temporary file in the same directory
// followed by a rename, so a power cut mid-write cannot truncate the original.
func (s *Store) Save() error {
	s.mu.RLock()
	f := file{Miners: make(map[string]Thresholds, len(s.perMiner))}
	for k, v := range s.perMiner {
		f.Miners[k] = v
	}
	for k := range s.disabled {
		f.Disabled = append(f.Disabled, k)
	}
	if len(s.icons) > 0 {
		f.Icons = make(map[string]string, len(s.icons))
		for k, v := range s.icons {
			f.Icons[k] = v
		}
	}
	sv := s.saver
	f.Screensaver = &sv
	s.mu.RUnlock()

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("settings: encode: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("settings: create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".thresholds-*.json")
	if err != nil {
		return fmt.Errorf("settings: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("settings: chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("settings: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("settings: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("settings: close: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("settings: rename: %w", err)
	}
	return nil
}
