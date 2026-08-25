package reward

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

func setup(t *testing.T) (*Store, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(time.Now()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(db.SQL(), audit.New(db.SQL(), time.UTC)), db
}
func i64(v int64) *int64        { return &v }
func tp(t time.Time) *time.Time { return &t }

func TestWatchedAddressLifecycle(t *testing.T) {
	s, db := setup(t)
	defer db.Close()
	now := time.Now()
	if err := s.AddAddress("bc1qtest", "NerdOctaxe", "york", now); err != nil {
		t.Fatalf("AddAddress: %v", err)
	}
	act, _ := s.ActiveAddresses()
	if len(act) != 1 || act[0] != "bc1qtest" {
		t.Fatalf("active = %v", act)
	}
	if err := s.RemoveAddress("bc1qtest", "york", now.Add(time.Hour)); err != nil {
		t.Fatalf("RemoveAddress: %v", err)
	}
	if act, _ := s.ActiveAddresses(); len(act) != 0 {
		t.Errorf("address still active after removal: %v", act)
	}
}

func TestRecordRewardIsEvidenceAndIdempotent(t *testing.T) {
	s, db := setup(t)
	defer db.Close()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	raw := []byte(`{"txid":"abc","vout":0,"value":312500000}`)

	e := Event{
		Address: "bc1qtest", TxID: "abc", Vout: 0, AmountSat: 312_500_000,
		BlockHeight: i64(963692), BlockTime: tp(now), FirstSeen: tp(now),
		MaturityTime: tp(now.Add(100 * 10 * time.Minute)), // coinbase maturity
		SourceClassification: SourceDirectCoinbase, Status: StatusConfirmed, EvidenceSource: "node",
	}
	inserted, err := s.RecordReward(e, raw, now)
	if err != nil || !inserted {
		t.Fatalf("record: inserted=%v err=%v", inserted, err)
	}
	id, amt, status, hash, _, err := s.Get("abc", 0)
	if err != nil || id == 0 || amt != 312_500_000 || status != StatusConfirmed || hash == "" {
		t.Fatalf("get: id=%d amt=%d status=%s hash=%q err=%v", id, amt, status, hash, err)
	}
	// Idempotent: second record with different raw must not overwrite.
	inserted2, _ := s.RecordReward(e, []byte(`{"tampered":true}`), now.Add(time.Hour))
	if inserted2 {
		t.Error("duplicate reward inserted")
	}
	_, _, _, hash2, _, _ := s.Get("abc", 0)
	if hash2 != hash {
		t.Error("raw evidence changed on duplicate record")
	}
}

func TestConfirmationsTrackedAndReorgPreservesOriginal(t *testing.T) {
	s, db := setup(t)
	defer db.Close()
	now := time.Now()
	_, _ = s.RecordReward(Event{Address: "a", TxID: "tx1", Vout: 0, AmountSat: 100, Confirmations: i64(1), Status: StatusSeen}, []byte("{}"), now)

	if err := s.UpdateConfirmations("tx1", 0, 100, tp(now), nil, StatusMature); err != nil {
		t.Fatalf("UpdateConfirmations: %v", err)
	}
	_, _, status, _, conf, _ := s.Get("tx1", 0)
	if conf == nil || *conf != 100 || status != StatusMature {
		t.Errorf("confirmations not tracked: conf=%v status=%s", conf, status)
	}

	if err := s.MarkReorg("tx1", 0, "reorged at height X", now.Add(time.Hour)); err != nil {
		t.Fatalf("MarkReorg: %v", err)
	}
	_, amt, status2, _, _, _ := s.Get("tx1", 0)
	if status2 != StatusReorged || amt != 100 {
		t.Errorf("reorg should preserve the original amount and set REORGED: amt=%d status=%s", amt, status2)
	}
	var n int
	db.SQL().QueryRow("SELECT COUNT(*) FROM reward_status_events WHERE txid='tx1'").Scan(&n)
	if n != 1 {
		t.Errorf("reorg status event count = %d, want 1", n)
	}
}
