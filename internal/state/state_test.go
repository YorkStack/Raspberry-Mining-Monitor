package state

import (
	"sync"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/bitcoin"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/model"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/pool"
)

var now = time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

func snap(name string, ths float64) miner.Snapshot {
	s := miner.Snapshot{Name: name, HashrateTHs: ths}
	s.Succeed(now)
	return s
}

func TestMinersAreReturnedInRegistrationOrder(t *testing.T) {
	s := New([]string{"NerdOctaxe", "Gamma 602"})
	s.SetMiner("Gamma 602", snap("Gamma 602", 1.27))
	s.SetMiner("NerdOctaxe", snap("NerdOctaxe", 12.10))

	got := s.Miners()
	if len(got) != 2 {
		t.Fatalf("got %d miners, want 2", len(got))
	}
	if got[0].Name != "NerdOctaxe" || got[1].Name != "Gamma 602" {
		t.Errorf("order = %q, %q; want registration order", got[0].Name, got[1].Name)
	}
}

// A miner that has never answered still needs a tile, otherwise the layout
// changes shape when a device comes back.
func TestUnfetchedMinerStillAppears(t *testing.T) {
	s := New([]string{"NerdOctaxe", "Gamma 602"})
	s.SetMiner("NerdOctaxe", snap("NerdOctaxe", 12.10))

	got := s.Miners()
	if len(got) != 2 {
		t.Fatalf("got %d miners, want 2", len(got))
	}
	if got[1].Name != "Gamma 602" {
		t.Errorf("second miner = %q, want the placeholder for Gamma 602", got[1].Name)
	}
	if got[1].HasData() {
		t.Error("HasData = true for a miner that never answered")
	}
}

func TestFailMinerKeepsLastGoodHashrate(t *testing.T) {
	s := New([]string{"NerdOctaxe"})
	s.SetMiner("NerdOctaxe", snap("NerdOctaxe", 12.10))
	s.FailMiner("NerdOctaxe", now.Add(time.Second), "connection refused")

	got := s.Miners()[0]
	if got.OK {
		t.Error("OK = true, want false")
	}
	if got.HashrateTHs != 12.10 {
		t.Errorf("HashrateTHs = %v, want the last known 12.10", got.HashrateTHs)
	}
	if got.Err != "connection refused" {
		t.Errorf("Err = %q", got.Err)
	}
	if !got.FetchedAt.Equal(now) {
		t.Errorf("FetchedAt = %v, want the last successful fetch", got.FetchedAt)
	}
}

func TestSetPoolAndNetworkRoundTrip(t *testing.T) {
	s := New(nil)

	p := pool.Snapshot{Provider: "publicpool", WorkersCount: 2}
	p.Succeed(now)
	s.SetPool(p)

	n := bitcoin.Snapshot{Kind: bitcoin.SourcePublic, Height: 963692}
	n.Succeed(now)
	s.SetNetwork(n)

	if got := s.Pool(); got.WorkersCount != 2 {
		t.Errorf("WorkersCount = %d, want 2", got.WorkersCount)
	}
	if got := s.Network(); got.Height != 963692 {
		t.Errorf("Height = %d, want 963692", got.Height)
	}
}

func TestSubscriberIsNotifiedOnChange(t *testing.T) {
	s := New([]string{"NerdOctaxe"})
	ch, cancel := s.Subscribe()
	defer cancel()

	s.SetMiner("NerdOctaxe", snap("NerdOctaxe", 12.10))

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("subscriber was not notified within a second")
	}
}

func TestCancelStopsNotifications(t *testing.T) {
	s := New([]string{"NerdOctaxe"})
	ch, cancel := s.Subscribe()
	cancel()

	s.SetMiner("NerdOctaxe", snap("NerdOctaxe", 12.10))

	select {
	case _, open := <-ch:
		if open {
			t.Error("received a notification after cancel")
		}
	case <-time.After(100 * time.Millisecond):
		// Nothing arrived, which is also acceptable.
	}
}

func TestCancelIsIdempotent(t *testing.T) {
	s := New(nil)
	_, cancel := s.Subscribe()
	cancel()
	cancel() // must not panic or double-close
}

// A browser that stops reading must not be able to stall the collectors.
func TestSlowSubscriberDoesNotBlockPublisher(t *testing.T) {
	s := New([]string{"NerdOctaxe"})
	_, cancel := s.Subscribe() // never drained
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			s.SetMiner("NerdOctaxe", snap("NerdOctaxe", float64(i)))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publishing blocked on a subscriber that never reads")
	}
}

func TestConcurrentReadsAndWritesAreSafe(t *testing.T) {
	s := New([]string{"NerdOctaxe", "Gamma 602"})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				s.SetMiner("NerdOctaxe", snap("NerdOctaxe", float64(j)))
				_ = s.Miners()
				_ = s.Pool()
				_ = s.Network()
			}
		}(i)
	}
	wg.Wait()
}

// Callers must not be able to mutate stored state through a returned slice.
func TestMinersReturnsACopy(t *testing.T) {
	s := New([]string{"NerdOctaxe"})
	s.SetMiner("NerdOctaxe", snap("NerdOctaxe", 12.10))

	got := s.Miners()
	got[0].HashrateTHs = 999

	if s.Miners()[0].HashrateTHs != 12.10 {
		t.Error("mutating the returned slice changed the stored snapshot")
	}
}

func TestSetMinerIgnoresUnknownName(t *testing.T) {
	s := New([]string{"NerdOctaxe"})
	s.SetMiner("Ghost", snap("Ghost", 1))

	if len(s.Miners()) != 1 {
		t.Errorf("got %d miners, want 1; unknown names must be ignored", len(s.Miners()))
	}
}

func TestPlaceholderCarriesTheConfiguredName(t *testing.T) {
	s := New([]string{"Gamma 602"})
	got := s.Miners()[0]
	if got.Name != "Gamma 602" {
		t.Errorf("Name = %q, want the configured name", got.Name)
	}
	var zero model.Source
	if got.Source != zero {
		t.Errorf("Source = %+v, want the zero value", got.Source)
	}
}
