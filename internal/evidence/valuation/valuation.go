// Package valuation applies a versioned EUR valuation policy to rewards.
//
// Rates are EUR cents per whole BTC and reward values are euro cents — integers,
// never floats. A policy change is versioned and audited. A failed primary
// source falls back to the configured source and records the reason. A manual
// correction inserts a new valuation that references and preserves the original.
package valuation

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
)

// Valuation methods.
const (
	MethodSpot    = "SPOT"
	MethodOpen    = "OPEN"
	MethodClose   = "CLOSE"
	MethodAverage = "AVERAGE"
	MethodManual  = "MANUAL"
)

// Policy is a tax-year valuation policy.
type Policy struct {
	TaxYear          int
	Currency         string
	PrimarySource    string
	FallbackSource   string
	Method           string
	Timezone         string
	Rounding         string
	DecimalPrecision int
}

// Rates carries the candidate EUR-cents-per-BTC rates from one source.
type Rates struct {
	SpotCents    *int64
	OpenCents    *int64
	CloseCents   *int64
	AverageCents *int64
}

func (r Rates) forMethod(method string) *int64 {
	switch method {
	case MethodSpot:
		return r.SpotCents
	case MethodOpen:
		return r.OpenCents
	case MethodClose:
		return r.CloseCents
	case MethodAverage:
		return r.AverageCents
	}
	return nil
}

// Store persists policies and valuations.
type Store struct {
	db  *sql.DB
	log *audit.Log
}

// New creates a store.
func New(db *sql.DB, log *audit.Log) *Store { return &Store{db: db, log: log} }

// SetPolicy stores a new version of the policy for its tax year.
func (s *Store) SetPolicy(p Policy, actor string, now time.Time) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var maxV sql.NullInt64
	if err := tx.QueryRow("SELECT MAX(version) FROM valuation_policies WHERE tax_year = ?", p.TaxYear).Scan(&maxV); err != nil {
		return 0, err
	}
	version := 1
	if maxV.Valid {
		version = int(maxV.Int64) + 1
	}
	if _, err := tx.Exec(`INSERT INTO valuation_policies
		(tax_year, version, currency, primary_source, fallback_source, method, timezone, rounding,
		 decimal_precision, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.TaxYear, version, p.Currency, p.PrimarySource, p.FallbackSource, p.Method, p.Timezone,
		p.Rounding, p.DecimalPrecision, now.UTC().Format(time.RFC3339), actor); err != nil {
		return 0, err
	}
	if _, err := s.log.AppendTx(tx, audit.Event{
		EventUID: fmt.Sprintf("valpolicy-%d-v%d-%s", p.TaxYear, version, now.UTC().Format(time.RFC3339Nano)),
		TsUTC:    now, Actor: actor, Type: "valuation_policy.changed", Entity: "valuation_policy",
		EntityID: fmt.Sprint(p.TaxYear),
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return version, nil
}

// GetPolicy returns the latest policy version for a tax year.
func (s *Store) GetPolicy(taxYear int) (Policy, int, bool, error) {
	var (
		p       Policy
		version int
	)
	err := s.db.QueryRow(`SELECT version, currency, primary_source, fallback_source, method, timezone,
		rounding, decimal_precision FROM valuation_policies WHERE tax_year = ? ORDER BY version DESC LIMIT 1`,
		taxYear).Scan(&version, &p.Currency, &p.PrimarySource, &p.FallbackSource, &p.Method, &p.Timezone,
		&p.Rounding, &p.DecimalPrecision)
	if err == sql.ErrNoRows {
		return Policy{}, 0, false, nil
	}
	if err != nil {
		return Policy{}, 0, false, err
	}
	p.TaxYear = taxYear
	return p, version, true, nil
}

// satToCents converts a satoshi amount to euro cents at a rate of cents-per-BTC.
func satToCents(amountSat, rateCents int64) int64 {
	return int64(math.Round(float64(amountSat) / 1e8 * float64(rateCents)))
}

// Params are the inputs to a valuation.
type Params struct {
	RewardEventID  int64
	AmountSat      int64
	PolicyYear     int
	PolicyVersion  int
	Method         string
	Primary        Rates
	Fallback       Rates
	PrimarySource  string
	FallbackSource string
	APIEndpoint    string
	APIRetrievedAt time.Time
	Rounding       string
}

// Value computes and stores the EUR valuation of a reward. If the primary source
// lacks a rate for the method, it uses the fallback and records the reason.
func (s *Store) Value(p Params, rawResponse []byte, now time.Time) (id, eurCents int64, usedFallback bool, err error) {
	rate := p.Primary.forMethod(p.Method)
	fallbackReason := ""
	if rate == nil {
		rate = p.Fallback.forMethod(p.Method)
		usedFallback = true
		fallbackReason = "primary source provided no rate for method " + p.Method
	}
	if rate == nil {
		return 0, 0, false, fmt.Errorf("valuation: no %s rate from primary or fallback source", p.Method)
	}
	eurCents = satToCents(p.AmountSat, *rate)

	sum := sha256.Sum256(rawResponse)
	hash := hex.EncodeToString(sum[:])

	res, err := s.db.Exec(`INSERT INTO valuation_snapshots
		(reward_event_id, policy_year, policy_version, method, spot_rate_cents, open_rate_cents,
		 close_rate_cents, average_rate_cents, selected_rate_cents, primary_source, fallback_source,
		 fallback_reason, api_endpoint, api_retrieved_at, raw_response, raw_sha256, amount_sat,
		 amount_eur_cents, rounding, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.RewardEventID, p.PolicyYear, p.PolicyVersion, p.Method,
		ni(p.Primary.SpotCents), ni(p.Primary.OpenCents), ni(p.Primary.CloseCents), ni(p.Primary.AverageCents),
		*rate, p.PrimarySource, fallbackSourceIf(usedFallback, p.FallbackSource), nsIf(usedFallback, fallbackReason),
		p.APIEndpoint, p.APIRetrievedAt.UTC().Format(time.RFC3339), string(rawResponse), hash,
		p.AmountSat, eurCents, p.Rounding, now.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, 0, false, err
	}
	id, _ = res.LastInsertId()
	return id, eurCents, usedFallback, nil
}

// ManualAdjust records a tax-adviser-approved value as a new valuation that
// supersedes the original. The original row is preserved.
func (s *Store) ManualAdjust(originalID, newEurCents int64, reason, actor string, now time.Time) (int64, error) {
	var (
		rewardID, amountSat, selectedRate, origCents int64
		method, rounding                             string
		policyYear, policyVersion                    sql.NullInt64
	)
	err := s.db.QueryRow(`SELECT reward_event_id, amount_sat, selected_rate_cents, amount_eur_cents,
		method, rounding, policy_year, policy_version FROM valuation_snapshots WHERE id = ?`, originalID).
		Scan(&rewardID, &amountSat, &selectedRate, &origCents, &method, &rounding, &policyYear, &policyVersion)
	if err != nil {
		return 0, fmt.Errorf("valuation: original %d: %w", originalID, err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO valuation_snapshots
		(reward_event_id, policy_year, policy_version, method, selected_rate_cents, primary_source,
		 amount_sat, amount_eur_cents, rounding, manual_adjustment_cents, adjustment_reason, adjusted_by,
		 adjusted_at, supersedes_id, created_at)
		VALUES (?, ?, ?, ?, ?, 'MANUAL', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rewardID, policyYear, policyVersion, MethodManual, selectedRate, amountSat, newEurCents, rounding,
		newEurCents-origCents, reason, actor, now.UTC().Format(time.RFC3339), originalID,
		now.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	newID, _ := res.LastInsertId()
	if _, err := s.log.AppendTx(tx, audit.Event{
		EventUID: fmt.Sprintf("valadjust-%d-%s", originalID, now.UTC().Format(time.RFC3339Nano)),
		TsUTC:    now, Actor: actor, Type: "valuation.corrected", Entity: "valuation",
		EntityID: fmt.Sprint(originalID), Reason: reason,
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return newID, nil
}

// GetCents returns the amount_eur_cents of a valuation row.
func (s *Store) GetCents(id int64) (int64, error) {
	var c int64
	err := s.db.QueryRow("SELECT amount_eur_cents FROM valuation_snapshots WHERE id = ?", id).Scan(&c)
	return c, err
}

func ni(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
func fallbackSourceIf(used bool, src string) any {
	if !used {
		return nil
	}
	return src
}
func nsIf(used bool, s string) any {
	if !used {
		return nil
	}
	return s
}
