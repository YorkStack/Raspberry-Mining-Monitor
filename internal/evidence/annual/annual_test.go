package annual

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/report"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/signing"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

// setup opens a fresh store with a clean, closable miner/address and some
// year-2026 rewards, costs and energy so the summary has content.
func setup(t *testing.T) (*store.DB, *report.Store, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "e.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(time.Now()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := "2026-06-15T12:00:00Z"
	db.SQL().Exec("INSERT INTO miners (internal_id, created_at) VALUES ('M1', ?)", now)
	db.SQL().Exec(`INSERT INTO miner_versions (miner_id, version, valid_from, serial_number, invoice_number, created_at)
		VALUES (1, 1, ?, 'SN1', 'INV-1', ?)`, now, now)
	db.SQL().Exec("INSERT INTO watched_addresses (address, added_at, added_by) VALUES ('bc1q', ?, 'york')", now)
	db.SQL().Exec(`INSERT INTO reward_events (address, txid, vout, amount_sat, block_time, status, created_at)
		VALUES ('bc1q','tx1',0, 500000, '2026-03-01T00:00:00Z', 'CONFIRMED', ?)`, now)
	db.SQL().Exec(`INSERT INTO reward_events (address, txid, vout, amount_sat, block_time, status, created_at)
		VALUES ('bc1q','tx2',0, 250000, '2025-12-31T00:00:00Z', 'CONFIRMED', ?)`, now) // prior year, excluded
	db.SQL().Exec(`INSERT INTO cost_records (cost_date, description, category, gross_cents, currency, created_at, created_by)
		VALUES ('2026-04-10','power','ENERGY', 12000, 'EUR', ?, 'york')`, now)
	db.SQL().Exec(`INSERT INTO energy_measurements (measurement_start, measurement_end, energy_wh, measured, created_at, created_by)
		VALUES ('2026-04-01T00:00:00Z','2026-04-30T00:00:00Z', 90000, 1, ?, 'york')`, now)

	log := audit.New(db.SQL(), time.UTC)
	rs := report.New(db.SQL(), log, filepath.Join(dir, "reports"), "0.8.0")
	return db, rs, dir
}

func TestBuildRollsUpClosedMonths(t *testing.T) {
	db, rs, dir := setup(t)
	defer db.Close()

	// Close two months of the year (with acknowledge for any warnings).
	if _, _, _, err := rs.Close("2026-03", true, "york", time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("close 03: %v", err)
	}
	if _, _, _, err := rs.Close("2026-04", true, "york", time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("close 04: %v", err)
	}

	key, err := signing.Generate()
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	out := filepath.Join(dir, "annual", "2026")
	m, bundle, err := Build(db.SQL(), out, key, Meta{Year: "2026", SoftwareVersion: "0.8.0", SchemaVersion: 7}, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(m.Periods) != 2 {
		t.Fatalf("periods = %d, want 2", len(m.Periods))
	}
	for _, p := range m.Periods {
		if !p.Verified {
			t.Errorf("period %s not verified", p.Period)
		}
	}
	if bundle == "" {
		t.Error("empty annual bundle hash")
	}
	if m.Disclaimer == "" {
		t.Error("disclaimer missing")
	}
	// Year-filtered summary: only the 2026 reward (500000), not the 2025 one.
	if m.Summary.RewardsTotalSat != 500000 {
		t.Errorf("rewards sat = %d, want 500000 (2025 excluded)", m.Summary.RewardsTotalSat)
	}
	if m.Summary.CostsTotalCents != 12000 {
		t.Errorf("costs cents = %d, want 12000", m.Summary.CostsTotalCents)
	}
	if m.Summary.EnergyMeasuredWh != 90000 {
		t.Errorf("measured Wh = %d, want 90000", m.Summary.EnergyMeasuredWh)
	}

	// The signed annual package verifies, and tampering a copied file fails it.
	ok, bad, err := Verify(out, key.Public())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatalf("fresh annual package did not verify (bad=%s)", bad)
	}

	// Find a copied CSV and corrupt it.
	victim := filepath.Join(out, "2026-03", "data", "rewards.csv")
	if err := os.WriteFile(victim, []byte("tampered\n"), 0o640); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	ok, bad, err = Verify(out, key.Public())
	if err != nil {
		t.Fatalf("Verify after tamper: %v", err)
	}
	if ok {
		t.Error("tampered annual package still verified")
	}
	if bad == "" {
		t.Error("tamper detected but no bad file named")
	}
}

func TestBuildRefusesEmptyYear(t *testing.T) {
	db, _, dir := setup(t)
	defer db.Close()
	key, _ := signing.Generate()
	_, _, err := Build(db.SQL(), filepath.Join(dir, "annual", "2099"), key, Meta{Year: "2099"}, time.Now())
	if err == nil {
		t.Error("Build with no closed months should fail")
	}
}
