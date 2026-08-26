// Package annual rolls up a tax year's monthly evidence packages into one
// signed, self-contained annual package for handing to the tax adviser.
//
// It documents facts and integrity only. It copies each closed month's
// latest-revision evidence package, re-verifies every copy against its own
// manifest, records the per-month evidence-bundle hashes and a factual
// year-filtered summary, then signs the annual manifest with the dedicated
// evidence key. It never computes or determines a tax result; the standard
// disclaimer is part of the manifest.
package annual

import (
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/backup"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/export"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/signing"
)

// Disclaimer is carried in every annual manifest.
const Disclaimer = "Technical factual documentation only. This annual package " +
	"does not determine the legal or tax classification of the mining activity."

// PeriodEntry records one included month and its integrity result.
type PeriodEntry struct {
	Period             string `json:"period"`
	ReportID           string `json:"reportId"`
	Revision           int    `json:"revision"`
	EvidenceBundleHash string `json:"evidenceBundleHash"`
	FilesCopied        int    `json:"filesCopied"`
	Verified           bool   `json:"verified"`
}

// Summary is a factual, year-filtered roll-up. It states amounts, never a tax
// treatment. Money and Bitcoin amounts are integer base units.
type Summary struct {
	RewardsCount            int   `json:"rewardsCount"`
	RewardsTotalSat         int64 `json:"rewardsTotalSat"`
	ValuationsTotalEURCents int64 `json:"valuationsTotalEurCents"`
	CostsTotalCents         int64 `json:"costsTotalCents"`
	EnergyMeasuredWh        int64 `json:"energyMeasuredWh"`
	EnergyEstimatedWh       int64 `json:"energyEstimatedWh"`
}

// Manifest is the signed annual manifest. The annual-bundle hash is the SHA-256
// of this document's canonical serialisation.
type Manifest struct {
	Year            string        `json:"year"`
	GeneratedAt     string        `json:"generatedAt"`
	SoftwareVersion string        `json:"softwareVersion"`
	GitCommit       string        `json:"gitCommit"`
	SchemaVersion   int           `json:"schemaVersion"`
	SigningKeyID    string        `json:"signingKeyId"`
	Disclaimer      string        `json:"disclaimer"`
	Periods         []PeriodEntry `json:"periods"`
	Summary         Summary       `json:"summary"`
}

// Meta carries build-time metadata.
type Meta struct {
	Year            string
	SoftwareVersion string
	GitCommit       string
	SchemaVersion   int
}

// periodReport is the latest-revision report for one month.
type periodReport struct {
	period     string
	reportID   string
	revision   int
	bundleHash string
	packageDir string
}

// latestReports returns the latest revision per period for the year, ordered by
// period.
func latestReports(db *sql.DB, year string) ([]periodReport, error) {
	rows, err := db.Query(`SELECT period, report_id, revision, evidence_bundle_hash, package_dir
		FROM reports r
		WHERE period LIKE ? || '-%'
		  AND revision = (SELECT MAX(revision) FROM reports r2 WHERE r2.period = r.period)
		ORDER BY period`, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []periodReport
	for rows.Next() {
		var p periodReport
		if err := rows.Scan(&p.period, &p.reportID, &p.revision, &p.bundleHash, &p.packageDir); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func summary(db *sql.DB, year string) Summary {
	var s Summary
	like := year + "-%"
	db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(amount_sat),0) FROM reward_events
		WHERE status != 'REORGED' AND block_time LIKE ?`, like).Scan(&s.RewardsCount, &s.RewardsTotalSat)
	db.QueryRow(`SELECT COALESCE(SUM(v.amount_eur_cents),0) FROM valuation_snapshots v
		JOIN reward_events e ON e.id = v.reward_event_id
		WHERE v.supersedes_id IS NULL AND e.status != 'REORGED' AND e.block_time LIKE ?`, like).Scan(&s.ValuationsTotalEURCents)
	db.QueryRow(`SELECT COALESCE(SUM(gross_cents),0) FROM cost_records WHERE cost_date LIKE ?`, like).Scan(&s.CostsTotalCents)
	db.QueryRow(`SELECT COALESCE(SUM(energy_wh),0) FROM energy_measurements WHERE measured = 1 AND measurement_start LIKE ?`, like).Scan(&s.EnergyMeasuredWh)
	db.QueryRow(`SELECT COALESCE(SUM(energy_wh),0) FROM energy_measurements WHERE measured = 0 AND measurement_start LIKE ?`, like).Scan(&s.EnergyEstimatedWh)
	return s
}

// Build assembles the annual package under dir: it copies each closed month's
// package, re-verifies each copy, writes and signs annual-manifest.json, and
// returns the manifest and the annual-bundle hash. It fails if no month of the
// year has been closed.
func Build(db *sql.DB, dir string, key *signing.Key, m Meta, now time.Time) (Manifest, string, error) {
	reports, err := latestReports(db, m.Year)
	if err != nil {
		return Manifest{}, "", err
	}
	if len(reports) == 0 {
		return Manifest{}, "", fmt.Errorf("annual: no closed reports for %s", m.Year)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return Manifest{}, "", err
	}

	man := Manifest{
		Year: m.Year, GeneratedAt: now.UTC().Format(time.RFC3339),
		SoftwareVersion: m.SoftwareVersion, GitCommit: m.GitCommit, SchemaVersion: m.SchemaVersion,
		SigningKeyID: key.KeyID(), Disclaimer: Disclaimer, Summary: summary(db, m.Year),
	}

	for _, r := range reports {
		dst := filepath.Join(dir, r.period)
		res, err := backup.Copy(r.packageDir, dst)
		if err != nil {
			return Manifest{}, "", err
		}
		// Re-verify the copy against its own evidence manifest and confirm the
		// re-derived bundle hash matches what the report recorded.
		verOK, _, rehash, err := export.VerifyEvidencePackage(dst)
		if err != nil {
			return Manifest{}, "", err
		}
		man.Periods = append(man.Periods, PeriodEntry{
			Period: r.period, ReportID: r.reportID, Revision: r.revision,
			EvidenceBundleHash: r.bundleHash, FilesCopied: res.FilesCopied,
			Verified: res.Verified && verOK && rehash == r.bundleHash,
		})
	}
	sort.Slice(man.Periods, func(i, j int) bool { return man.Periods[i].Period < man.Periods[j].Period })

	canonical, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return Manifest{}, "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "annual-manifest.json"), canonical, 0o640); err != nil {
		return Manifest{}, "", err
	}
	sig := key.Sign(canonical)
	if err := os.WriteFile(filepath.Join(dir, "annual-manifest.sig"),
		[]byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o640); err != nil {
		return Manifest{}, "", err
	}
	sum := sha256.Sum256(canonical)
	return man, hex.EncodeToString(sum[:]), nil
}

// ReadManifest reads annual-manifest.json from dir and returns the manifest
// together with the annual-bundle hash (the SHA-256 of the file as stored).
func ReadManifest(dir string) (Manifest, string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "annual-manifest.json"))
	if err != nil {
		return Manifest{}, "", err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, "", err
	}
	sum := sha256.Sum256(raw)
	return m, hex.EncodeToString(sum[:]), nil
}

// Verify checks the annual manifest's signature and re-verifies every included
// month's copied package against its recorded evidence-bundle hash. Any
// mismatch fails and bad names the offending period or file.
func Verify(dir string, pub ed25519.PublicKey) (ok bool, bad string, err error) {
	raw, err := os.ReadFile(filepath.Join(dir, "annual-manifest.json"))
	if err != nil {
		return false, "", err
	}
	sigB64, err := os.ReadFile(filepath.Join(dir, "annual-manifest.sig"))
	if err != nil {
		return false, "", err
	}
	sig, err := base64.StdEncoding.DecodeString(trimNL(string(sigB64)))
	if err != nil {
		return false, "annual-manifest.sig", nil
	}
	if !signing.Verify(pub, raw, sig) {
		return false, "annual-manifest.json", nil
	}
	var man Manifest
	if err := json.Unmarshal(raw, &man); err != nil {
		return false, "", err
	}
	for _, p := range man.Periods {
		verOK, badFile, rehash, err := export.VerifyEvidencePackage(filepath.Join(dir, p.Period))
		if err != nil {
			return false, p.Period, nil
		}
		if !verOK {
			return false, filepath.Join(p.Period, badFile), nil
		}
		if rehash != p.EvidenceBundleHash {
			return false, p.Period, nil
		}
	}
	return true, "", nil
}

// Record persists the annual package in the database.
func Record(db *sql.DB, m Manifest, dir, bundleHash string, key *signing.Key, now time.Time) error {
	sigB64, _ := os.ReadFile(filepath.Join(dir, "annual-manifest.sig"))
	allVerified := 1
	for _, p := range m.Periods {
		if !p.Verified {
			allVerified = 0
		}
	}
	_, err := db.Exec(`INSERT INTO annual_packages
		(year, package_dir, annual_bundle_hash, periods_included, all_verified,
		 signing_key_id, signature, manifest_path, software_version, schema_version, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Year, dir, bundleHash, len(m.Periods), allVerified,
		key.KeyID(), trimNL(string(sigB64)), filepath.Join(dir, "annual-manifest.json"),
		m.SoftwareVersion, m.SchemaVersion, now.UTC().Format(time.RFC3339))
	return err
}

func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
