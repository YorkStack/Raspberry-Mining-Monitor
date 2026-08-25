// Package cost records mining-related expenses.
//
// Amounts are euro cents (integers). The module may compute preliminary
// summaries but never decides immediate deduction, depreciation, business vs
// private classification, or VAT deduction — that is the tax adviser's call.
// Adviser adjustments are separate versioned records; the original cost is
// preserved.
package cost

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
)

// Cost is one expense record.
type Cost struct {
	Date              string // ISO date
	Description       string
	Category          string
	GrossCents        int64
	NetCents          *int64
	VatCents          *int64
	Currency          string
	PaymentMethod     string
	MinerInternalID   string
	ReportingPeriod   string // e.g. 2026-08
	InvoiceDocumentID *int64
	InvoiceSHA256     string
	AllocationPct     *float64
	Note              string
}

// Adjustment is a tax-adviser correction to a cost.
type Adjustment struct {
	GrossCents    *int64
	NetCents      *int64
	VatCents      *int64
	AllocationPct *float64
	Reason        string
}

// CategorySum is a preliminary per-category total.
type CategorySum struct {
	Category        string
	TotalGrossCents int64
	Count           int
}

// Store persists costs.
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

// Add stores a cost record. If Currency is empty it defaults to EUR.
func (s *Store) Add(c Cost, actor string, now time.Time) (int64, error) {
	if c.Currency == "" {
		c.Currency = "EUR"
	}
	if c.Category == "" {
		return 0, fmt.Errorf("cost: category is required")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO cost_records
		(cost_date, description, category, gross_cents, net_cents, vat_cents, currency, payment_method,
		 miner_internal_id, reporting_period, invoice_document_id, invoice_sha256, allocation_pct, note,
		 created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Date, c.Description, c.Category, c.GrossCents, ni(c.NetCents), ni(c.VatCents), c.Currency,
		c.PaymentMethod, c.MinerInternalID, c.ReportingPeriod, ni(c.InvoiceDocumentID), c.InvoiceSHA256,
		nf(c.AllocationPct), c.Note, now.UTC().Format(time.RFC3339), actor)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if _, err := s.log.AppendTx(tx, audit.Event{
		EventUID: fmt.Sprintf("cost-%d-%s", id, now.UTC().Format(time.RFC3339Nano)),
		TsUTC:    now, Actor: actor, Type: "cost.added", Entity: "cost", EntityID: fmt.Sprint(id),
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// Adjust records a tax-adviser adjustment as a separate versioned row, leaving
// the original cost record untouched.
func (s *Store) Adjust(costID int64, adj Adjustment, actor string, now time.Time) (int64, error) {
	if adj.Reason == "" {
		return 0, fmt.Errorf("cost: an adjustment must state a reason")
	}
	var exists int
	if err := s.db.QueryRow("SELECT 1 FROM cost_records WHERE id = ?", costID).Scan(&exists); err != nil {
		return 0, fmt.Errorf("cost: original %d: %w", costID, err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO cost_adjustments
		(cost_id, adjusted_gross_cents, adjusted_net_cents, adjusted_vat_cents, adjusted_allocation_pct,
		 reason, adjusted_by, adjusted_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		costID, ni(adj.GrossCents), ni(adj.NetCents), ni(adj.VatCents), nf(adj.AllocationPct),
		adj.Reason, actor, now.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	adjID, _ := res.LastInsertId()
	if _, err := s.log.AppendTx(tx, audit.Event{
		EventUID: fmt.Sprintf("cost-adj-%d-%s", costID, now.UTC().Format(time.RFC3339Nano)),
		TsUTC:    now, Actor: actor, Type: "cost.adjusted", Entity: "cost", EntityID: fmt.Sprint(costID),
		Reason:   adj.Reason,
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return adjID, nil
}

// Summary returns a preliminary per-category total for a reporting period. It is
// a factual sum only, not a tax computation.
func (s *Store) Summary(reportingPeriod string) ([]CategorySum, error) {
	rows, err := s.db.Query(`SELECT category, SUM(gross_cents), COUNT(*) FROM cost_records
		WHERE reporting_period = ? GROUP BY category ORDER BY category`, reportingPeriod)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CategorySum
	for rows.Next() {
		var cs CategorySum
		if err := rows.Scan(&cs.Category, &cs.TotalGrossCents, &cs.Count); err != nil {
			return nil, err
		}
		out = append(out, cs)
	}
	return out, rows.Err()
}

// GetGross returns the gross_cents of a cost record.
func (s *Store) GetGross(costID int64) (int64, error) {
	var v int64
	err := s.db.QueryRow("SELECT gross_cents FROM cost_records WHERE id = ?", costID).Scan(&v)
	return v, err
}
