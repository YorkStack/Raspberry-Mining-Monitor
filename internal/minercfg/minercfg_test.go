package minercfg

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func specs() []Spec {
	return []Spec{
		{Name: "NerdOctaxe", Type: "axeos", Host: "192.168.1.51", PayoutAddress: "bc1qnerd", Interval: 2 * time.Second, Timeout: 2 * time.Second},
		{Name: "Gamma 602", Type: "axeos", Host: "192.168.1.52", PayoutAddress: "bc1qgamma", Interval: 2 * time.Second, Timeout: 2 * time.Second},
	}
}

func newStore(t *testing.T) *Store {
	return New(filepath.Join(t.TempDir(), "miners.json"),
		Providers{BitcoinBaseURL: "https://mempool.space", PoolBaseURL: "https://public-pool.io:40557"})
}

func TestSeedWhenAbsent(t *testing.T) {
	s := newStore(t)
	if err := s.SeedIfEmpty(specs(), Providers{BitcoinBaseURL: "https://mempool.space", PoolBaseURL: "https://public-pool.io:40557"}); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if len(s.Miners()) != 2 {
		t.Errorf("got %d miners, want 2", len(s.Miners()))
	}
}

func TestSeedDoesNotOverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "miners.json")

	a := New(path, Providers{})
	_ = a.SeedIfEmpty(specs(), Providers{BitcoinBaseURL: "https://x", PoolBaseURL: "https://y"})

	b := New(path, Providers{})
	if err := b.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Seeding a loaded store with different data must not replace it.
	_ = b.SeedIfEmpty([]Spec{{Name: "Other", Type: "demo"}}, Providers{})
	if len(b.Miners()) != 2 {
		t.Errorf("seed overwrote existing store: got %d miners", len(b.Miners()))
	}
}

func TestReplaceValidatesAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "miners.json")
	s := New(path, Providers{})

	if err := s.Replace(specs()); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	reloaded := New(path, Providers{})
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reloaded.Miners()) != 2 || reloaded.Miners()[0].Host != "192.168.1.51" {
		t.Errorf("miners did not persist: %+v", reloaded.Miners())
	}
}

func TestReplaceRejectsDuplicateNames(t *testing.T) {
	s := newStore(t)
	bad := []Spec{{Name: "A", Type: "axeos", Host: "h1"}, {Name: "A", Type: "axeos", Host: "h2"}}
	if err := s.Replace(bad); err == nil {
		t.Error("duplicate names accepted")
	}
}

func TestReplaceRejectsAxeosWithoutHost(t *testing.T) {
	s := newStore(t)
	if err := s.Replace([]Spec{{Name: "A", Type: "axeos"}}); err == nil {
		t.Error("axeos miner without host accepted")
	}
}

func TestReplaceRejectsEmptyName(t *testing.T) {
	s := newStore(t)
	if err := s.Replace([]Spec{{Name: "", Type: "demo"}}); err == nil {
		t.Error("empty name accepted")
	}
}

func TestReplaceRejectsUnknownType(t *testing.T) {
	s := newStore(t)
	if err := s.Replace([]Spec{{Name: "A", Type: "antminer", Host: "h"}}); err == nil {
		t.Error("unknown type accepted")
	}
}

func TestReplaceAppliesIntervalDefault(t *testing.T) {
	s := newStore(t)
	if err := s.Replace([]Spec{{Name: "A", Type: "axeos", Host: "h"}}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if got := s.Miners()[0].Interval; got != DefaultInterval {
		t.Errorf("Interval = %v, want default %v", got, DefaultInterval)
	}
}

func TestSetProvidersValidatesURL(t *testing.T) {
	s := newStore(t)
	if err := s.SetProviders(Providers{BitcoinBaseURL: "not a url", PoolBaseURL: "https://y"}); err == nil {
		t.Error("invalid bitcoin URL accepted")
	}
	if err := s.SetProviders(Providers{BitcoinBaseURL: "https://mempool.space", PoolBaseURL: "https://public-pool.io:40557"}); err != nil {
		t.Errorf("valid providers rejected: %v", err)
	}
	if s.Providers().BitcoinBaseURL != "https://mempool.space" {
		t.Errorf("providers not stored")
	}
}

func TestNames(t *testing.T) {
	s := newStore(t)
	_ = s.Replace(specs())
	names := s.Names()
	if len(names) != 2 || names[0] != "NerdOctaxe" {
		t.Errorf("Names = %v", names)
	}
}

func TestSavedFileNotWorldReadable(t *testing.T) {
	s := newStore(t)
	_ = s.Replace(specs())
	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("mode = %v, want no group/world access", info.Mode().Perm())
	}
}

func TestCorruptFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "miners.json")
	_ = os.WriteFile(path, []byte("{bad"), 0o600)
	s := New(path, Providers{})
	if err := s.Load(); err == nil {
		t.Error("corrupt file accepted")
	}
}
