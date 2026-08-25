package expected

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

func i64(v int64) *int64 { return &v }

func setup(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(time.Now()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// A network snapshot stub to satisfy the foreign key.
	_, err = db.SQL().Exec(`INSERT INTO network_snapshots (snapshot_uid, ts_utc, ts_local, created_at)
		VALUES ('NS1', ?, ?, ?)`, "2026-08-24T00:00:00Z", "2026-08-24T02:00:00+02:00", "2026-08-24T00:00:00Z")
	if err != nil {
		t.Fatalf("insert network snapshot: %v", err)
	}
	return db
}

// 12 TH/s against 900 EH/s at a 312_500_000 sat block reward yields exactly
// 600 sat/day expected value.
func refInputs() Inputs {
	return Inputs{
		MinerHashrateHs: 12e12, NetworkHashrateHs: 900e18, Difficulty: 1.26e14,
		RewardPerBlockSat: 312_500_000, BTCPriceCents: i64(6_500_000),
	}
}

func TestCalculateReferenceValues(t *testing.T) {
	r := Calculate(refInputs())
	if r.ExpectedSatDay != 600 {
		t.Errorf("ExpectedSatDay = %d, want 600", r.ExpectedSatDay)
	}
	if r.ExpectedSatYear != 600*365 {
		t.Errorf("ExpectedSatYear = %d, want %d", r.ExpectedSatYear, 600*365)
	}
	if r.ExpectedEURCentsDay == nil || *r.ExpectedEURCentsDay != 39 {
		t.Errorf("ExpectedEURCentsDay = %v, want 39", r.ExpectedEURCentsDay)
	}
	if r.ProbBlockYear <= r.ProbBlockDay {
		t.Error("year probability must exceed day probability")
	}
	if r.MeanSecondsToBlock <= 0 {
		t.Error("mean time not computed")
	}
}

func TestZeroHashrateGivesZeroExpectation(t *testing.T) {
	in := refInputs()
	in.MinerHashrateHs = 0
	r := Calculate(in)
	if r.ExpectedSatDay != 0 || r.ProbBlockYear != 0 {
		t.Errorf("zero hashrate should give zero expectation, got %+v", r)
	}
}

func TestRecordIsImmutableAfterNetworkChanges(t *testing.T) {
	db := setup(t)
	defer db.Close()
	s := New(db.SQL())
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)

	inserted, r1, err := s.Record("NS1", now, refInputs(), now)
	if err != nil || !inserted {
		t.Fatalf("first record: inserted=%v err=%v", inserted, err)
	}

	// Network hashrate later doubles (odds would halve). Re-recording the SAME
	// snapshot must NOT change the frozen historical value.
	changed := refInputs()
	changed.NetworkHashrateHs = 1800e18
	inserted2, _, err := s.Record("NS1", now, changed, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("second record: %v", err)
	}
	if inserted2 {
		t.Error("second record inserted a new row; historical value must be immutable")
	}
	got, ok, err := s.GetSatDay("NS1", FormulaVersion)
	if err != nil || !ok {
		t.Fatalf("get: %v ok=%v", err, ok)
	}
	if got != r1.ExpectedSatDay {
		t.Errorf("stored sat_day = %d, want the original %d (unchanged)", got, r1.ExpectedSatDay)
	}
}

func TestFormulaVersionSeparatesRows(t *testing.T) {
	db := setup(t)
	defer db.Close()
	now := time.Now()
	// Directly insert a v2 row for the same snapshot: both versions coexist.
	_, err := db.SQL().Exec(`INSERT INTO expected_value_snapshots
		(network_snapshot_uid, ts_utc, formula_version, miner_hashrate_hs, network_hashrate_hs,
		 difficulty, reward_per_block_sat, expected_sat_day, expected_sat_month, expected_sat_year,
		 prob_block_day, prob_block_month, prob_block_year, mean_seconds_to_block, created_at)
		VALUES ('NS1', ?, 1, 12e12, 900e18, 1.26e14, 312500000, 600, 18000, 219000, 0.1, 0.2, 0.3, 100, ?)`,
		now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("v1: %v", err)
	}
	_, err = db.SQL().Exec(`INSERT INTO expected_value_snapshots
		(network_snapshot_uid, ts_utc, formula_version, miner_hashrate_hs, network_hashrate_hs,
		 difficulty, reward_per_block_sat, expected_sat_day, expected_sat_month, expected_sat_year,
		 prob_block_day, prob_block_month, prob_block_year, mean_seconds_to_block, created_at)
		VALUES ('NS1', ?, 2, 12e12, 900e18, 1.26e14, 312500000, 610, 18300, 222650, 0.1, 0.2, 0.3, 100, ?)`,
		now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("v2 should be a new row, not a conflict: %v", err)
	}
	var n int
	db.SQL().QueryRow("SELECT COUNT(*) FROM expected_value_snapshots WHERE network_snapshot_uid='NS1'").Scan(&n)
	if n != 2 {
		t.Errorf("rows = %d, want 2 (one per formula version)", n)
	}
}
