// Package energy records electricity measurements for miners.
//
// It keeps physically measured and estimated consumption strictly separate, and
// an estimate must state its method and author. Measurement gaps are recorded
// through the completeness percentage and never silently interpolated. The
// module documents facts only; it does not decide whether energy is a
// deductible cost.
package energy

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
)

// Energy-source classifications.
const (
	SourceGrid    = "GRID"
	SourceSolar   = "SOLAR"
	SourceMixed   = "MIXED"
	SourceUnknown = "UNKNOWN"
)

// Measurement is one energy record for a period.
type Measurement struct {
	MinerInternalID   string
	Start             time.Time
	End               time.Time
	StartReadingWh    *int64
	EndReadingWh      *int64
	EnergyWh          int64
	AvgPowerW         *float64
	MinPowerW         *float64
	MaxPowerW         *float64
	CompletenessPct   float64
	Measured          bool // true = physically measured, false = estimated
	Source            string
	MeterDeviceID     string
	MeterSerial       string
	EnergySource      string // GRID / SOLAR / MIXED / UNKNOWN
	SolarProductionWh *int64
	GridImportWh      *int64
	GridExportWh      *int64
	// Provenance for estimates (required when Measured is false).
	EstimationMethod string
	EstimatedBy      string
	OriginalGap      string
	Note             string
}

// Store persists energy measurements.
type Store struct {
	db  *sql.DB
	log *audit.Log
}

// New creates a store.
func New(db *sql.DB, log *audit.Log) *Store { return &Store{db: db, log: log} }

func ni(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
func nf(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// Record stores a measurement. An estimate (Measured=false) must state its
// method and author, so estimated data is never mistaken for a meter reading.
func (s *Store) Record(m Measurement, actor string, now time.Time) (int64, error) {
	if !m.Measured {
		if m.EstimationMethod == "" || m.EstimatedBy == "" {
			return 0, fmt.Errorf("energy: an estimate must state estimation_method and estimated_by")
		}
	}
	if m.EnergySource == "" {
		m.EnergySource = SourceUnknown
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO energy_measurements
		(miner_internal_id, measurement_start, measurement_end, start_reading_wh, end_reading_wh,
		 energy_wh, avg_power_w, min_power_w, max_power_w, completeness_pct, measured, source,
		 meter_device_id, meter_serial, energy_source, solar_production_wh, grid_import_wh, grid_export_wh,
		 estimation_method, estimated_by, original_gap, note, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.MinerInternalID, m.Start.UTC().Format(time.RFC3339), m.End.UTC().Format(time.RFC3339),
		ni(m.StartReadingWh), ni(m.EndReadingWh), m.EnergyWh, nf(m.AvgPowerW), nf(m.MinPowerW), nf(m.MaxPowerW),
		m.CompletenessPct, boolToInt(m.Measured), m.Source, m.MeterDeviceID, m.MeterSerial, m.EnergySource,
		ni(m.SolarProductionWh), ni(m.GridImportWh), ni(m.GridExportWh), m.EstimationMethod, m.EstimatedBy,
		m.OriginalGap, m.Note, now.UTC().Format(time.RFC3339), actor)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	kind := "energy.measured"
	if !m.Measured {
		kind = "energy.estimated"
	}
	if _, err := s.log.AppendTx(tx, audit.Event{
		EventUID: fmt.Sprintf("energy-%d-%s", id, now.UTC().Format(time.RFC3339Nano)),
		TsUTC:    now, Actor: actor, Type: kind, Entity: "energy", EntityID: fmt.Sprint(id),
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// Totals returns measured and estimated energy (Wh) separately for a miner over
// a period, so the two are never conflated.
func (s *Store) Totals(minerID string, from, to time.Time) (measuredWh, estimatedWh int64, err error) {
	rows, err := s.db.Query(`SELECT measured, COALESCE(SUM(energy_wh), 0) FROM energy_measurements
		WHERE miner_internal_id = ? AND measurement_start >= ? AND measurement_end <= ?
		GROUP BY measured`,
		minerID, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var measured int
		var sum int64
		if err := rows.Scan(&measured, &sum); err != nil {
			return 0, 0, err
		}
		if measured == 1 {
			measuredWh = sum
		} else {
			estimatedWh = sum
		}
	}
	return measuredWh, estimatedWh, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
