package network

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

func TestRecordStoresRawHashAndIsAppendOnly(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(time.Now()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := New(db.SQL(), time.UTC)
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)

	raw := []byte(`{"height":963692,"difficulty":1.26e14}`)
	snap := Snapshot{UID: "daily-2026-08-24", TsUTC: now, BlockHeight: 963692,
		Difficulty: 1.26e14, NetworkHashrateHs: 9e20, SubsidySat: 312_500_000,
		RewardPerBlockSat: 312_500_000, Source: "mempool", APIRetrievedAt: now}

	inserted, err := s.Record(snap, raw, now)
	if err != nil || !inserted {
		t.Fatalf("first record inserted=%v err=%v", inserted, err)
	}
	sum := sha256.Sum256(raw)
	got, ok, _ := s.RawHash("daily-2026-08-24")
	if !ok || got != hex.EncodeToString(sum[:]) {
		t.Errorf("raw hash mismatch: %q", got)
	}

	// Re-record the same UID with different raw data: append-only, no change.
	inserted2, _ := s.Record(snap, []byte(`{"tampered":true}`), now.Add(time.Hour))
	if inserted2 {
		t.Error("re-recording the same snapshot UID modified history")
	}
	after, _, _ := s.RawHash("daily-2026-08-24")
	if after != hex.EncodeToString(sum[:]) {
		t.Error("historical raw hash changed on re-record")
	}
}
