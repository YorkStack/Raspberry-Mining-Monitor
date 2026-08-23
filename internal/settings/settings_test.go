package settings

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func defaults() Thresholds {
	return Thresholds{ASICWarnC: 64, ASICCritC: 70, VRMWarnC: 80, VRMCritC: 90}
}

func newStore(t *testing.T) *Store {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "thresholds.json"), defaults())
}

func TestFreshStoreReturnsTheDefaults(t *testing.T) {
	s := newStore(t)
	if got := s.For("NerdOctaxe"); got != defaults() {
		t.Errorf("For = %+v, want the defaults %+v", got, defaults())
	}
	if len(s.Overrides()) != 0 {
		t.Errorf("Overrides = %v, want empty", s.Overrides())
	}
}

func TestSetOverridesOneMinerOnly(t *testing.T) {
	s := newStore(t)
	custom := Thresholds{ASICWarnC: 50, ASICCritC: 58, VRMWarnC: 75, VRMCritC: 85}

	if err := s.Set("Gamma 602", custom); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := s.For("Gamma 602"); got != custom {
		t.Errorf("For(Gamma) = %+v, want %+v", got, custom)
	}
	if got := s.For("NerdOctaxe"); got != defaults() {
		t.Errorf("For(NerdOctaxe) = %+v, want the untouched defaults", got)
	}
}

func TestResetRestoresTheDefault(t *testing.T) {
	s := newStore(t)
	_ = s.Set("Gamma 602", Thresholds{ASICWarnC: 50, ASICCritC: 58, VRMWarnC: 75, VRMCritC: 85})
	s.Reset("Gamma 602")

	if got := s.For("Gamma 602"); got != defaults() {
		t.Errorf("For = %+v, want the defaults back", got)
	}
}

func TestRejectsCritAtOrBelowWarn(t *testing.T) {
	s := newStore(t)
	bad := Thresholds{ASICWarnC: 70, ASICCritC: 65, VRMWarnC: 80, VRMCritC: 90}

	err := s.Set("NerdOctaxe", bad)
	if err == nil || !strings.Contains(err.Error(), "crit") {
		t.Fatalf("err = %v, want a complaint about crit below warn", err)
	}
	if got := s.For("NerdOctaxe"); got != defaults() {
		t.Errorf("a rejected Set changed the store to %+v", got)
	}
}

func TestRejectsOutOfRangeValues(t *testing.T) {
	s := newStore(t)
	cases := map[string]Thresholds{
		"negative":  {ASICWarnC: -5, ASICCritC: 70, VRMWarnC: 80, VRMCritC: 90},
		"too hot":   {ASICWarnC: 64, ASICCritC: 400, VRMWarnC: 80, VRMCritC: 90},
		"NaN":       {ASICWarnC: math.NaN(), ASICCritC: 70, VRMWarnC: 80, VRMCritC: 90},
		"infinite":  {ASICWarnC: 64, ASICCritC: math.Inf(1), VRMWarnC: 80, VRMCritC: 90},
		"vrm order": {ASICWarnC: 64, ASICCritC: 70, VRMWarnC: 95, VRMCritC: 90},
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			if err := s.Set("NerdOctaxe", bad); err == nil {
				t.Errorf("%+v was accepted, want an error", bad)
			}
		})
	}
}

func TestRejectsEmptyMinerName(t *testing.T) {
	s := newStore(t)
	if err := s.Set("", defaults()); err == nil {
		t.Error("an empty miner name was accepted")
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thresholds.json")
	custom := Thresholds{ASICWarnC: 50, ASICCritC: 58, VRMWarnC: 75, VRMCritC: 85}

	a := New(path, defaults())
	if err := a.Set("Gamma 602", custom); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := a.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	b := New(path, defaults())
	if err := b.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := b.For("Gamma 602"); got != custom {
		t.Errorf("after reload For = %+v, want %+v", got, custom)
	}
}

// A missing file is the normal first-run state, not an error.
func TestLoadWithNoFileIsNotAnError(t *testing.T) {
	s := newStore(t)
	if err := s.Load(); err != nil {
		t.Fatalf("Load on a fresh install returned %v", err)
	}
	if got := s.For("NerdOctaxe"); got != defaults() {
		t.Errorf("For = %+v, want the defaults", got)
	}
}

// A corrupt file must not take the dashboard down. It reports the problem and
// leaves the in-memory defaults intact.
func TestCorruptFileLeavesTheDefaultsUsable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thresholds.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := New(path, defaults())
	if err := s.Load(); err == nil {
		t.Error("Load accepted a corrupt file")
	}
	if got := s.For("NerdOctaxe"); got != defaults() {
		t.Errorf("For = %+v after a corrupt load, want the defaults", got)
	}
}

// A stored file that violates the rules must not be trusted either.
func TestLoadRejectsAnInvalidStoredBand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thresholds.json")
	body := `{"miners":{"NerdOctaxe":{"asicWarnC":70,"asicCritC":60,"vrmWarnC":80,"vrmCritC":90}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	s := New(path, defaults())
	if err := s.Load(); err == nil {
		t.Error("Load accepted a stored band with crit below warn")
	}
	if got := s.For("NerdOctaxe"); got != defaults() {
		t.Errorf("For = %+v, want the defaults", got)
	}
}

func TestSavedFileIsNotWorldReadable(t *testing.T) {
	s := newStore(t)
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("mode = %v, want no group or world access", perm)
	}
}

func TestOverridesReturnsACopy(t *testing.T) {
	s := newStore(t)
	custom := Thresholds{ASICWarnC: 50, ASICCritC: 58, VRMWarnC: 75, VRMCritC: 85}
	_ = s.Set("Gamma 602", custom)

	got := s.Overrides()
	got["Gamma 602"] = Thresholds{ASICWarnC: 1, ASICCritC: 2}
	delete(got, "Gamma 602")

	if s.For("Gamma 602") != custom {
		t.Error("mutating the returned map changed the store")
	}
}

func TestConcurrentUseIsSafe(t *testing.T) {
	s := newStore(t)
	custom := Thresholds{ASICWarnC: 50, ASICCritC: 58, VRMWarnC: 75, VRMCritC: 85}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = s.Set("Gamma 602", custom)
				_ = s.For("Gamma 602")
				_ = s.Overrides()
				_ = s.Default()
			}
		}()
	}
	wg.Wait()
}

func TestMinersEnabledByDefault(t *testing.T) {
	s := newStore(t)
	if !s.Enabled("NerdOctaxe") {
		t.Error("a miner should be enabled by default")
	}
}

func TestDisableAndReenable(t *testing.T) {
	s := newStore(t)
	s.SetEnabled("Gamma 602", false)
	if s.Enabled("Gamma 602") {
		t.Error("Gamma should be disabled after SetEnabled(false)")
	}
	if !s.Enabled("NerdOctaxe") {
		t.Error("disabling one miner must not affect another")
	}
	s.SetEnabled("Gamma 602", true)
	if !s.Enabled("Gamma 602") {
		t.Error("Gamma should be enabled again")
	}
}

func TestDisabledStatePersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thresholds.json")

	a := New(path, defaults())
	a.SetEnabled("MacBook M2", false)
	if err := a.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	b := New(path, defaults())
	if err := b.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if b.Enabled("MacBook M2") {
		t.Error("disabled state did not survive a reload")
	}
	if !b.Enabled("NerdOctaxe") {
		t.Error("an untouched miner should still be enabled after reload")
	}
}

func TestIconPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thresholds.json")
	a := New(path, defaults())
	a.SetIcon("NerdOctaxe", "i-quantum-cube")
	if err := a.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	b := New(path, defaults())
	if err := b.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if b.Icon("NerdOctaxe") != "i-quantum-cube" {
		t.Errorf("icon = %q, want i-quantum-cube", b.Icon("NerdOctaxe"))
	}
	if b.Icon("Gamma 602") != "" {
		t.Errorf("unset icon should be empty, got %q", b.Icon("Gamma 602"))
	}
}

func TestScreensaverPersistsAndValidates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thresholds.json")
	a := New(path, defaults())

	if err := a.SetScreensaver(Screensaver{Mode: "blank", Minutes: 5}); err != nil {
		t.Fatalf("SetScreensaver: %v", err)
	}
	if err := a.SetScreensaver(Screensaver{Mode: "nope", Minutes: 5}); err == nil {
		t.Error("unknown mode accepted")
	}
	if err := a.SetScreensaver(Screensaver{Mode: "off", Minutes: 9999}); err == nil {
		t.Error("out-of-range minutes accepted")
	}
	if err := a.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	b := New(path, defaults())
	if err := b.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := b.ScreensaverCfg(); got.Mode != "blank" || got.Minutes != 5 {
		t.Errorf("screensaver = %+v, want blank/5", got)
	}
}

func TestScreensaverDefaultDoesNotOverrideLoaded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thresholds.json")
	a := New(path, defaults())
	_ = a.SetScreensaver(Screensaver{Mode: "blank", Minutes: 3})
	_ = a.Save()

	b := New(path, defaults())
	_ = b.Load()
	b.SetScreensaverDefault(Screensaver{Mode: "floating", Minutes: 15})
	if got := b.ScreensaverCfg(); got.Mode != "blank" {
		t.Errorf("default overrode a loaded value: %+v", got)
	}
}
