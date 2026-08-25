// Package expected computes and permanently stores the contemporaneous mining
// expectation for a network snapshot.
//
// The result is a statistical expected VALUE, never a prediction of actual
// rewards. It is stored once per (network snapshot, formula version) and never
// recalculated or overwritten when difficulty, hashrate, price or subsidy later
// change — that is the whole point of a contemporaneous record. Changing the
// formula creates a new FormulaVersion instead of editing history.
//
// Bitcoin amounts are satoshi and money is euro cents, both int64. Physical
// inputs (hashrate, difficulty) are float64; no monetary value is a float.
package expected

import (
	"database/sql"
	"math"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/probability"
)

// FormulaVersion is the version of the calculation below. Bump it when the
// formula changes; historical rows keep the version they were computed with.
const FormulaVersion = 1

const (
	blocksPerDay   = 144.0
	daysPerMonth   = 30.0
	daysPerYear    = 365.0
	satPerBTC      = 100_000_000.0
	ppmDenominator = 1_000_000.0
)

// Inputs are the values the expectation is computed from. Optional fields are
// pointers so an unknown input never becomes a confident zero.
type Inputs struct {
	MinerHashrateHs          float64
	NetworkHashrateHs        float64
	Difficulty               float64
	RewardPerBlockSat        int64
	BTCPriceCents            *int64 // EUR cents per whole BTC
	ElectricityPriceCentsKWh *int64
	PoolFeePPM               int64 // 0 for solo
	MinerPowerW              *int64
}

// Result is the computed expectation. Monetary and Bitcoin outputs are integers.
type Result struct {
	FormulaVersion int

	ExpectedSatDay   int64
	ExpectedSatMonth int64
	ExpectedSatYear  int64

	ExpectedEURCentsDay   *int64
	ExpectedEURCentsMonth *int64
	ExpectedEURCentsYear  *int64

	ProbBlockDay   float64
	ProbBlockMonth float64
	ProbBlockYear  float64
	MeanSecondsToBlock float64

	ExpectedEnergyWhDay        *int64
	ExpectedElectricityCentsDay *int64
	ExpectedPoolFeeCentsDay    *int64
	ExpectedNetCentsDay        *int64
}

func satToCents(sat int64, priceCents int64) int64 {
	// round(sat / 1e8 * priceCents) without floating money: compute in float for
	// the ratio, round to integer cents.
	return int64(math.Round(float64(sat) / satPerBTC * float64(priceCents)))
}

// Calculate returns the contemporaneous expectation for the given inputs.
func Calculate(in Inputs) Result {
	r := Result{FormulaVersion: FormulaVersion}
	if in.NetworkHashrateHs <= 0 || in.MinerHashrateHs <= 0 || in.RewardPerBlockSat <= 0 {
		return r
	}
	share := in.MinerHashrateHs / in.NetworkHashrateHs
	satDay := share * blocksPerDay * float64(in.RewardPerBlockSat)

	r.ExpectedSatDay = int64(math.Round(satDay))
	r.ExpectedSatMonth = int64(math.Round(satDay * daysPerMonth))
	r.ExpectedSatYear = int64(math.Round(satDay * daysPerYear))

	r.ProbBlockDay = probability.AtLeastOne(in.MinerHashrateHs, in.Difficulty, probability.Day)
	r.ProbBlockMonth = probability.AtLeastOne(in.MinerHashrateHs, in.Difficulty, probability.Month)
	r.ProbBlockYear = probability.AtLeastOne(in.MinerHashrateHs, in.Difficulty, probability.Year)
	if mean, ok := probability.MeanTimeToBlockSeconds(in.MinerHashrateHs, in.Difficulty); ok {
		r.MeanSecondsToBlock = mean
	}

	if in.BTCPriceCents != nil {
		d := satToCents(r.ExpectedSatDay, *in.BTCPriceCents)
		m := satToCents(r.ExpectedSatMonth, *in.BTCPriceCents)
		y := satToCents(r.ExpectedSatYear, *in.BTCPriceCents)
		r.ExpectedEURCentsDay, r.ExpectedEURCentsMonth, r.ExpectedEURCentsYear = &d, &m, &y
	}

	if in.MinerPowerW != nil {
		energyWhDay := *in.MinerPowerW * 24
		r.ExpectedEnergyWhDay = &energyWhDay
		if in.ElectricityPriceCentsKWh != nil {
			cents := int64(math.Round(float64(energyWhDay) / 1000.0 * float64(*in.ElectricityPriceCentsKWh)))
			r.ExpectedElectricityCentsDay = &cents
		}
	}

	if in.PoolFeePPM > 0 && r.ExpectedEURCentsDay != nil {
		fee := int64(math.Round(float64(*r.ExpectedEURCentsDay) * float64(in.PoolFeePPM) / ppmDenominator))
		r.ExpectedPoolFeeCentsDay = &fee
	}

	// Net (before hardware depreciation) only when revenue is known.
	if r.ExpectedEURCentsDay != nil {
		net := *r.ExpectedEURCentsDay
		if r.ExpectedElectricityCentsDay != nil {
			net -= *r.ExpectedElectricityCentsDay
		}
		if r.ExpectedPoolFeeCentsDay != nil {
			net -= *r.ExpectedPoolFeeCentsDay
		}
		r.ExpectedNetCentsDay = &net
	}

	return r
}

// Store persists expected-value snapshots immutably.
type Store struct{ db *sql.DB }

// New creates a store.
func New(db *sql.DB) *Store { return &Store{db: db} }

func nInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// Record computes and stores the expectation for a network snapshot. If a row
// already exists for this (snapshot, formula version), it is left untouched and
// inserted=false is returned: historical results are never recalculated.
func (s *Store) Record(networkSnapshotUID string, tsUTC time.Time, in Inputs, now time.Time) (inserted bool, r Result, err error) {
	r = Calculate(in)
	res, err := s.db.Exec(`INSERT OR IGNORE INTO expected_value_snapshots
		(network_snapshot_uid, ts_utc, formula_version, miner_hashrate_hs, network_hashrate_hs,
		 difficulty, reward_per_block_sat, btc_price_cents, electricity_price_cents_kwh, pool_fee_ppm,
		 expected_sat_day, expected_sat_month, expected_sat_year,
		 expected_eur_cents_day, expected_eur_cents_month, expected_eur_cents_year,
		 prob_block_day, prob_block_month, prob_block_year, mean_seconds_to_block,
		 expected_energy_wh_day, expected_electricity_cents_day, expected_pool_fee_cents_day, expected_net_cents_day,
		 created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		networkSnapshotUID, tsUTC.UTC().Format(time.RFC3339), r.FormulaVersion,
		in.MinerHashrateHs, in.NetworkHashrateHs, in.Difficulty, in.RewardPerBlockSat,
		nInt(in.BTCPriceCents), nInt(in.ElectricityPriceCentsKWh), in.PoolFeePPM,
		r.ExpectedSatDay, r.ExpectedSatMonth, r.ExpectedSatYear,
		nInt(r.ExpectedEURCentsDay), nInt(r.ExpectedEURCentsMonth), nInt(r.ExpectedEURCentsYear),
		r.ProbBlockDay, r.ProbBlockMonth, r.ProbBlockYear, r.MeanSecondsToBlock,
		nInt(r.ExpectedEnergyWhDay), nInt(r.ExpectedElectricityCentsDay),
		nInt(r.ExpectedPoolFeeCentsDay), nInt(r.ExpectedNetCentsDay),
		now.UTC().Format(time.RFC3339))
	if err != nil {
		return false, r, err
	}
	n, _ := res.RowsAffected()
	return n > 0, r, nil
}

// GetSatDay returns the stored expected_sat_day for a snapshot and formula
// version, proving what was frozen. Used by tests and reports.
func (s *Store) GetSatDay(networkSnapshotUID string, formulaVersion int) (int64, bool, error) {
	var v int64
	err := s.db.QueryRow(`SELECT expected_sat_day FROM expected_value_snapshots
		WHERE network_snapshot_uid = ? AND formula_version = ?`, networkSnapshotUID, formulaVersion).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	return v, err == nil, err
}
