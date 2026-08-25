package valuation

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

func i64(v int64) *int64 { return &v }

func setup(t *testing.T) (*Store, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(time.Now()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// A reward event to value (FK target).
	_, err = db.SQL().Exec(`INSERT INTO reward_events (address, txid, vout, amount_sat, status, created_at)
		VALUES ('a', 'tx1', 0, 312500000, 'CONFIRMED', ?)`, time.Now().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seed reward: %v", err)
	}
	return New(db.SQL(), audit.New(db.SQL(), time.UTC)), db
}

func TestPolicyIsVersioned(t *testing.T) {
	s, db := setup(t)
	defer db.Close()
	now := time.Now()
	v1, _ := s.SetPolicy(Policy{TaxYear: 2026, Currency: "EUR", Method: MethodClose, PrimarySource: "KRAKEN"}, "york", now)
	v2, _ := s.SetPolicy(Policy{TaxYear: 2026, Currency: "EUR", Method: MethodClose, PrimarySource: "COINBASE"}, "york", now.Add(time.Hour))
	if v1 != 1 || v2 != 2 {
		t.Fatalf("versions = %d,%d; want 1,2", v1, v2)
	}
	p, ver, ok, _ := s.GetPolicy(2026)
	if !ok || ver != 2 || p.PrimarySource != "COINBASE" {
		t.Errorf("latest policy = v%d %s, want v2 COINBASE", ver, p.PrimarySource)
	}
}

func TestValueUsesIntegerCents(t *testing.T) {
	s, db := setup(t)
	defer db.Close()
	// 3.125 BTC at 65,000.00 EUR/BTC = 203,125.00 EUR = 20,312,500 cents.
	id, cents, fb, err := s.Value(Params{
		RewardEventID: 1, AmountSat: 312_500_000, PolicyYear: 2026, PolicyVersion: 1,
		Method: MethodClose, Primary: Rates{CloseCents: i64(6_500_000)}, PrimarySource: "KRAKEN",
	}, []byte(`{"close":65000}`), time.Now())
	if err != nil || fb {
		t.Fatalf("Value: id=%d fb=%v err=%v", id, fb, err)
	}
	if cents != 20_312_500 {
		t.Errorf("cents = %d, want 20312500", cents)
	}
}

func TestFallbackUsedAndRecorded(t *testing.T) {
	s, db := setup(t)
	defer db.Close()
	// Primary lacks a CLOSE rate; fallback has it.
	id, cents, fb, err := s.Value(Params{
		RewardEventID: 1, AmountSat: 100_000_000, PolicyYear: 2026, PolicyVersion: 1, Method: MethodClose,
		Primary: Rates{}, Fallback: Rates{CloseCents: i64(6_000_000)},
		PrimarySource: "KRAKEN", FallbackSource: "COINBASE",
	}, []byte(`{}`), time.Now())
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if !fb {
		t.Error("fallback should have been used")
	}
	if cents != 6_000_000 { // 1 BTC * 60,000.00 EUR
		t.Errorf("cents = %d, want 6000000", cents)
	}
	var reason string
	db.SQL().QueryRow("SELECT fallback_reason FROM valuation_snapshots WHERE id = ?", id).Scan(&reason)
	if reason == "" {
		t.Error("fallback reason not recorded")
	}
}

func TestManualAdjustmentPreservesOriginal(t *testing.T) {
	s, db := setup(t)
	defer db.Close()
	now := time.Now()
	origID, origCents, _, _ := s.Value(Params{
		RewardEventID: 1, AmountSat: 100_000_000, PolicyYear: 2026, PolicyVersion: 1, Method: MethodClose,
		Primary: Rates{CloseCents: i64(6_000_000)}, PrimarySource: "KRAKEN",
	}, []byte(`{}`), now)

	newID, err := s.ManualAdjust(origID, 6_100_000, "tax adviser approved rate", "adviser", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("ManualAdjust: %v", err)
	}
	if got, _ := s.GetCents(origID); got != origCents {
		t.Errorf("original changed: %d, want %d", got, origCents)
	}
	if got, _ := s.GetCents(newID); got != 6_100_000 {
		t.Errorf("adjusted value = %d, want 6100000", got)
	}
}
