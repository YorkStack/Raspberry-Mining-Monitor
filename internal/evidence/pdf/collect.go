package pdf

import (
	"database/sql"
	"fmt"
	"time"
)

// Collect assembles report Data from the evidence database.
func Collect(db *sql.DB, reportID, period string, revision int, bundleHash, softwareVersion, gitCommit string,
	schemaVersion int, generatedAt time.Time, pdfaStatus, sigStatus, backupStatus string) (Data, error) {
	d := Data{
		ReportID: reportID, Period: period, Revision: revision, EvidenceBundleHash: bundleHash,
		GeneratedAt: generatedAt.UTC().Format(time.RFC3339), SoftwareVersion: softwareVersion,
		GitCommit: gitCommit, SchemaVersion: schemaVersion, PDFAStatus: pdfaStatus,
		SignatureStatus: sigStatus, BackupStatus: backupStatus,
	}

	// Active miners.
	miners := Section{Title: "Active miner inventory"}
	rows, err := db.Query(`SELECT m.internal_id, mv.model, mv.serial_number, mv.firmware_version
		FROM miner_versions mv JOIN miners m ON m.id = mv.miner_id
		WHERE mv.superseded_at IS NULL ORDER BY m.internal_id`)
	if err != nil {
		return Data{}, err
	}
	for rows.Next() {
		var id, model, serial, fw sql.NullString
		if err := rows.Scan(&id, &model, &serial, &fw); err != nil {
			rows.Close()
			return Data{}, err
		}
		miners.Rows = append(miners.Rows, KV{Key: id.String,
			Value: fmt.Sprintf("%s  serial %s  firmware %s", nz(model.String, "—"), nz(serial.String, "—"), nz(fw.String, "—"))})
	}
	rows.Close()
	d.Sections = append(d.Sections, miners)

	// Rewards.
	rewards := Section{Title: "Actual mining rewards"}
	var rCount int
	var rSat sql.NullInt64
	db.QueryRow("SELECT COUNT(*), COALESCE(SUM(amount_sat),0) FROM reward_events WHERE status != 'REORGED'").Scan(&rCount, &rSat)
	rewards.Rows = append(rewards.Rows,
		KV{"Rewards recorded", fmt.Sprintf("%d", rCount)},
		KV{"Total amount", fmt.Sprintf("%d sat (%.8f BTC)", rSat.Int64, float64(rSat.Int64)/1e8)})
	d.Sections = append(d.Sections, rewards)

	// Valuations.
	vals := Section{Title: "EUR valuations"}
	var vCount int
	var vCents sql.NullInt64
	db.QueryRow("SELECT COUNT(*), COALESCE(SUM(amount_eur_cents),0) FROM valuation_snapshots WHERE supersedes_id IS NULL").Scan(&vCount, &vCents)
	vals.Rows = append(vals.Rows,
		KV{"Valuations", fmt.Sprintf("%d", vCount)},
		KV{"Total value", fmt.Sprintf("%.2f EUR", float64(vCents.Int64)/100)})
	d.Sections = append(d.Sections, vals)

	// Costs by category.
	costs := Section{Title: "Relevant costs (preliminary factual summary)"}
	crows, err := db.Query("SELECT category, SUM(gross_cents), COUNT(*) FROM cost_records GROUP BY category ORDER BY category")
	if err != nil {
		return Data{}, err
	}
	var total int64
	for crows.Next() {
		var cat string
		var sum, cnt int64
		crows.Scan(&cat, &sum, &cnt)
		costs.Rows = append(costs.Rows, KV{cat, fmt.Sprintf("%.2f EUR (%d)", float64(sum)/100, cnt)})
		total += sum
	}
	crows.Close()
	costs.Rows = append(costs.Rows, KV{"TOTAL", fmt.Sprintf("%.2f EUR", float64(total)/100)})
	costs.Rows = append(costs.Rows, KV{"Note", "Tax treatment requires professional review."})
	d.Sections = append(d.Sections, costs)

	// Energy (measured vs estimated).
	energy := Section{Title: "Energy consumption"}
	var measured, estimated sql.NullInt64
	db.QueryRow("SELECT COALESCE(SUM(energy_wh),0) FROM energy_measurements WHERE measured = 1").Scan(&measured)
	db.QueryRow("SELECT COALESCE(SUM(energy_wh),0) FROM energy_measurements WHERE measured = 0").Scan(&estimated)
	energy.Rows = append(energy.Rows,
		KV{"Physically measured", fmt.Sprintf("%d Wh", measured.Int64)},
		KV{"Estimated", fmt.Sprintf("%d Wh", estimated.Int64)})
	d.Sections = append(d.Sections, energy)

	// Data gaps and corrections.
	integrity := Section{Title: "Data gaps and corrections"}
	var gaps, cfgChanges, costAdj, valAdj int
	db.QueryRow("SELECT COUNT(*) FROM telemetry_hourly WHERE completeness_pct < 100").Scan(&gaps)
	db.QueryRow("SELECT COUNT(*) FROM miner_configurations").Scan(&cfgChanges)
	db.QueryRow("SELECT COUNT(*) FROM cost_adjustments").Scan(&costAdj)
	db.QueryRow("SELECT COUNT(*) FROM valuation_snapshots WHERE supersedes_id IS NOT NULL").Scan(&valAdj)
	integrity.Rows = append(integrity.Rows,
		KV{"Hours with telemetry gaps", fmt.Sprintf("%d", gaps)},
		KV{"Configuration records", fmt.Sprintf("%d", cfgChanges)},
		KV{"Cost adjustments", fmt.Sprintf("%d", costAdj)},
		KV{"Valuation corrections", fmt.Sprintf("%d", valAdj)})
	d.Sections = append(d.Sections, integrity)

	return d, nil
}

func nz(s, dflt string) string {
	if s == "" {
		return dflt
	}
	return s
}
