// Package telemetry persists per-miner measurements and rolls them up into
// hourly aggregates with explicit data-completeness accounting.
//
// Raw samples are kept short-term (default 30 days); hourly aggregates are kept
// for years. Missing samples are surfaced as a lower completeness percentage and
// listed gaps — never silently interpolated.
package telemetry

import (
	"database/sql"
	"encoding/json"
	"math"
	"time"
)

// Sample is one per-poll measurement. Optional fields are pointers so a value a
// miner does not report is absent, not a confident zero.
type Sample struct {
	MinerInternalID string
	TsUTC           time.Time
	Online          bool
	HashrateHs      *int64
	Hashrate1hHs    *int64
	AcceptedShares  *int64
	RejectedShares  *int64
	HWErrors        *int64
	BestDifficulty  *float64
	ASICTempC       *float64
	VRMTempC        *float64
	FanRPM          *int64
	PowerW          *float64
	UptimeS         *int64
	APIAvailable    bool
	DataQuality     string
}

// Hourly is one hour's aggregate for one miner.
type Hourly struct {
	MinerInternalID string
	HourStartUTC    time.Time
	AvgHashrateHs   *int64
	MinHashrateHs   *int64
	MaxHashrateHs   *int64
	AvgPowerW       *float64
	MinTempC        *float64
	MaxTempC        *float64
	EnergyWh        int64
	AcceptedDelta   int64
	RejectedDelta   int64
	OnlineMinutes   int64
	OfflineMinutes  int64
	ExpectedSamples int64
	ReceivedSamples int64
	CompletenessPct float64
	Gaps            string
}

// Store persists telemetry.
type Store struct {
	db           *sql.DB
	pollInterval time.Duration
}

// New creates a store. pollInterval is the expected sample cadence, used for
// completeness and energy accounting.
func New(db *sql.DB, pollInterval time.Duration) *Store {
	if pollInterval <= 0 {
		pollInterval = 60 * time.Second
	}
	return &Store{db: db, pollInterval: pollInterval}
}

func nullI(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
func nullF(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// RecordRaw inserts samples in one transaction, minimising SD-card writes.
func (s *Store) RecordRaw(samples ...Sample) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO telemetry_raw
		(miner_internal_id, ts_utc, online, hashrate_hs, hashrate_1h_hs, accepted_shares,
		 rejected_shares, hw_errors, best_difficulty, asic_temp_c, vrm_temp_c, fan_rpm,
		 power_w, uptime_s, api_available, data_quality)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, s := range samples {
		if _, err := stmt.Exec(s.MinerInternalID, s.TsUTC.UTC().Format(time.RFC3339Nano), b2i(s.Online),
			nullI(s.HashrateHs), nullI(s.Hashrate1hHs), nullI(s.AcceptedShares), nullI(s.RejectedShares),
			nullI(s.HWErrors), nullF(s.BestDifficulty), nullF(s.ASICTempC), nullF(s.VRMTempC),
			nullI(s.FanRPM), nullF(s.PowerW), nullI(s.UptimeS), b2i(s.APIAvailable), s.DataQuality); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type gap struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// AggregateHour computes and stores the aggregate for [hourStart, hourStart+1h)
// for one miner. Re-running recomputes from the raw samples (the source of
// truth). It returns the aggregate.
func (s *Store) AggregateHour(minerID string, hourStart time.Time, now time.Time) (Hourly, error) {
	hourStart = hourStart.UTC().Truncate(time.Hour)
	end := hourStart.Add(time.Hour)

	rows, err := s.db.Query(`SELECT ts_utc, online, hashrate_hs, accepted_shares, rejected_shares,
		asic_temp_c, power_w FROM telemetry_raw
		WHERE miner_internal_id = ? AND ts_utc >= ? AND ts_utc < ? ORDER BY ts_utc ASC`,
		minerID, hourStart.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano))
	if err != nil {
		return Hourly{}, err
	}
	defer rows.Close()

	h := Hourly{MinerInternalID: minerID, HourStartUTC: hourStart}
	var (
		sumHr, cntHr             int64
		minHr, maxHr             int64
		haveHr                   bool
		sumPow                   float64
		cntPow                   int
		minTemp, maxTemp         float64
		haveTemp                 bool
		firstAcc, lastAcc        sql.NullInt64
		firstRej, lastRej        sql.NullInt64
		onlineCnt, offlineCnt    int64
		prevTs                   time.Time
		havePrev                 bool
		gaps                     []gap
		intervalSec              = s.pollInterval.Seconds()
	)

	for rows.Next() {
		var (
			tsStr        string
			online       int
			hr, acc, rej sql.NullInt64
			temp, pow    sql.NullFloat64
		)
		if err := rows.Scan(&tsStr, &online, &hr, &acc, &rej, &temp, &pow); err != nil {
			return Hourly{}, err
		}
		ts, _ := time.Parse(time.RFC3339Nano, tsStr)
		h.ReceivedSamples++
		if online == 1 {
			onlineCnt++
		} else {
			offlineCnt++
		}
		if hr.Valid {
			sumHr += hr.Int64
			cntHr++
			if !haveHr || hr.Int64 < minHr {
				minHr = hr.Int64
			}
			if !haveHr || hr.Int64 > maxHr {
				maxHr = hr.Int64
			}
			haveHr = true
		}
		if pow.Valid {
			sumPow += pow.Float64
			cntPow++
		}
		if temp.Valid {
			if !haveTemp || temp.Float64 < minTemp {
				minTemp = temp.Float64
			}
			if !haveTemp || temp.Float64 > maxTemp {
				maxTemp = temp.Float64
			}
			haveTemp = true
		}
		if acc.Valid {
			if !firstAcc.Valid {
				firstAcc = acc
			}
			lastAcc = acc
		}
		if rej.Valid {
			if !firstRej.Valid {
				firstRej = rej
			}
			lastRej = rej
		}
		// A gap is a jump larger than two poll intervals between samples.
		if havePrev && ts.Sub(prevTs).Seconds() > 2*intervalSec {
			gaps = append(gaps, gap{From: prevTs.Format(time.RFC3339), To: ts.Format(time.RFC3339)})
		}
		prevTs = ts
		havePrev = true
	}
	if err := rows.Err(); err != nil {
		return Hourly{}, err
	}

	h.ExpectedSamples = int64(math.Round(3600 / intervalSec))
	if h.ExpectedSamples > 0 {
		h.CompletenessPct = math.Min(100, float64(h.ReceivedSamples)/float64(h.ExpectedSamples)*100)
	}
	h.OnlineMinutes = int64(math.Round(float64(onlineCnt) * intervalSec / 60))
	h.OfflineMinutes = int64(math.Round(float64(offlineCnt) * intervalSec / 60))
	if haveHr {
		avg := sumHr / cntHr
		h.AvgHashrateHs, h.MinHashrateHs, h.MaxHashrateHs = &avg, &minHr, &maxHr
	}
	if cntPow > 0 {
		avgPow := sumPow / float64(cntPow)
		h.AvgPowerW = &avgPow
		// Energy: each sample stands for one poll interval. Wh = W * h.
		h.EnergyWh = int64(math.Round(sumPow * intervalSec / 3600))
	}
	if haveTemp {
		h.MinTempC, h.MaxTempC = &minTemp, &maxTemp
	}
	h.AcceptedDelta = counterDelta(firstAcc, lastAcc)
	h.RejectedDelta = counterDelta(firstRej, lastRej)
	if len(gaps) > 0 {
		b, _ := json.Marshal(gaps)
		h.Gaps = string(b)
	}

	if err := s.upsertHourly(h, now); err != nil {
		return Hourly{}, err
	}
	return h, nil
}

// counterDelta returns last-first for a monotonic counter, clamped at 0 so a
// mid-hour restart never produces a negative share count.
func counterDelta(first, last sql.NullInt64) int64 {
	if !first.Valid || !last.Valid {
		return 0
	}
	d := last.Int64 - first.Int64
	if d < 0 {
		return 0
	}
	return d
}

func (s *Store) upsertHourly(h Hourly, now time.Time) error {
	_, err := s.db.Exec(`INSERT INTO telemetry_hourly
		(miner_internal_id, hour_start_utc, avg_hashrate_hs, min_hashrate_hs, max_hashrate_hs,
		 avg_power_w, min_temp_c, max_temp_c, energy_wh, accepted_delta, rejected_delta,
		 online_minutes, offline_minutes, expected_samples, received_samples, completeness_pct, gaps, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(miner_internal_id, hour_start_utc) DO UPDATE SET
		 avg_hashrate_hs=excluded.avg_hashrate_hs, min_hashrate_hs=excluded.min_hashrate_hs,
		 max_hashrate_hs=excluded.max_hashrate_hs, avg_power_w=excluded.avg_power_w,
		 min_temp_c=excluded.min_temp_c, max_temp_c=excluded.max_temp_c, energy_wh=excluded.energy_wh,
		 accepted_delta=excluded.accepted_delta, rejected_delta=excluded.rejected_delta,
		 online_minutes=excluded.online_minutes, offline_minutes=excluded.offline_minutes,
		 expected_samples=excluded.expected_samples, received_samples=excluded.received_samples,
		 completeness_pct=excluded.completeness_pct, gaps=excluded.gaps`,
		h.MinerInternalID, h.HourStartUTC.Format(time.RFC3339), nullI(h.AvgHashrateHs), nullI(h.MinHashrateHs),
		nullI(h.MaxHashrateHs), nullF(h.AvgPowerW), nullF(h.MinTempC), nullF(h.MaxTempC), h.EnergyWh,
		h.AcceptedDelta, h.RejectedDelta, h.OnlineMinutes, h.OfflineMinutes, h.ExpectedSamples,
		h.ReceivedSamples, h.CompletenessPct, h.Gaps, now.UTC().Format(time.RFC3339))
	return err
}

// PruneRaw deletes raw samples older than the cutoff and returns the count.
// Aggregates are unaffected, so history stays intact.
func (s *Store) PruneRaw(olderThan time.Time) (int64, error) {
	res, err := s.db.Exec("DELETE FROM telemetry_raw WHERE ts_utc < ?", olderThan.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RawCount returns the number of raw samples (for tests and diagnostics).
func (s *Store) RawCount() (int64, error) {
	var n int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM telemetry_raw").Scan(&n)
	return n, err
}
