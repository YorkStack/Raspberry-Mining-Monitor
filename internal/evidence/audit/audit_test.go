package audit

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

func newLog(t *testing.T) (*Log, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(time.Now()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(db.SQL(), time.UTC), db
}

func TestAppendChainsAndVerifies(t *testing.T) {
	l, db := newLog(t)
	defer db.Close()

	base := time.Date(2026, 8, 24, 12, 0, 0, 123456789, time.UTC)
	for i := 0; i < 5; i++ {
		if _, err := l.Append(Event{
			EventUID: "evt-" + string(rune('a'+i)), TsUTC: base.Add(time.Duration(i) * time.Second),
			Actor: "tester", Type: "miner.created", Entity: "miner", EntityID: "M1",
			NewValueHash: "abc", Reason: "test",
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if n, _ := l.Count(); n != 5 {
		t.Fatalf("count = %d, want 5", n)
	}
	ok, broken, err := l.Verify()
	if err != nil || !ok {
		t.Fatalf("Verify = %v, broken=%d, err=%v; want ok", ok, broken, err)
	}
}

func TestTamperingIsDetected(t *testing.T) {
	l, db := newLog(t)
	defer db.Close()

	for i := 0; i < 3; i++ {
		_, _ = l.Append(Event{EventUID: "e" + string(rune('0'+i)), TsUTC: time.Now(), Actor: "t", Type: "x"})
	}
	// Tamper one field of the middle entry.
	if _, err := db.SQL().Exec("UPDATE audit_log SET reason = 'tampered' WHERE id = 2"); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	ok, broken, err := l.Verify()
	if err != nil {
		t.Fatalf("Verify err: %v", err)
	}
	if ok {
		t.Fatal("Verify passed on a tampered chain")
	}
	if broken != 2 {
		t.Errorf("broken id = %d, want 2 (the first broken entry)", broken)
	}
}

func TestDeletionIsDetected(t *testing.T) {
	l, db := newLog(t)
	defer db.Close()
	for i := 0; i < 3; i++ {
		_, _ = l.Append(Event{EventUID: "d" + string(rune('0'+i)), TsUTC: time.Now(), Actor: "t", Type: "x"})
	}
	// Delete the first entry: the second's prev_entry_hash no longer matches genesis.
	if _, err := db.SQL().Exec("DELETE FROM audit_log WHERE id = 1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	ok, _, _ := l.Verify()
	if ok {
		t.Fatal("Verify passed after a deletion")
	}
}
