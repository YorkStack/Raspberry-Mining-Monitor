// Package export writes the evidence package: one CSV per dataset, an
// evidence manifest that hashes every file, and an evidence-bundle hash used as
// the visible verification identifier (stage 1 of the two-stage integrity
// model; the final manifest and PDF come in a later phase).
//
// CSV output is UTF-8, columns are in the table's declared order, rows are in a
// deterministic order, and base units are integers (satoshi, euro cents, Wh,
// H/s). No floating point is used for money or Bitcoin amounts anywhere.
package export

import (
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Dataset is one exported table and its stable output file.
type Dataset struct {
	File    string
	Table   string
	OrderBy string
}

// Datasets is the fixed, ordered set of evidence CSVs.
var Datasets = []Dataset{
	{"miners.csv", "miner_versions", "id"},
	{"configurations.csv", "miner_configurations", "id"},
	{"telemetry-hourly.csv", "telemetry_hourly", "id"},
	{"network-snapshots.csv", "network_snapshots", "ts_utc"},
	{"expected-value-calculations.csv", "expected_value_snapshots", "id"},
	{"rewards.csv", "reward_events", "id"},
	{"valuations.csv", "valuation_snapshots", "id"},
	{"energy.csv", "energy_measurements", "id"},
	{"costs.csv", "cost_records", "id"},
	{"documents.csv", "evidence_documents", "id"},
	{"corrections.csv", "cost_adjustments", "id"},
	{"audit-log.csv", "audit_log", "id"},
}

// columns returns a table's column names in declared (cid) order.
func columns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var (
			cid, notnull, pk int
			name, ctype      string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

// dumpTable writes a table to w as CSV with a header row. NULLs are empty cells.
func dumpTable(db *sql.DB, w io.Writer, table, orderBy string) error {
	cols, err := columns(db, table)
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(cols); err != nil {
		return err
	}
	query := "SELECT " + join(cols) + " FROM " + table
	if orderBy != "" {
		query += " ORDER BY " + orderBy
	}
	rows, err := db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	raw := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	rec := make([]string, len(cols))
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		for i, v := range raw {
			if v.Valid {
				rec[i] = v.String
			} else {
				rec[i] = ""
			}
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	cw.Flush()
	return cw.Error()
}

func join(cols []string) string {
	out := ""
	for i, c := range cols {
		if i > 0 {
			out += ", "
		}
		out += c
	}
	return out
}

// ManifestEntry is one file's integrity record.
type ManifestEntry struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// EvidenceManifest lists every evidence file with its hash, sorted by name.
type EvidenceManifest struct {
	Files []ManifestEntry `json:"files"`
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// WriteEvidencePackage writes all dataset CSVs under dir/data, writes
// dir/evidence-manifest.json, and returns the evidence-bundle hash (the SHA-256
// of the canonical manifest). The manifest deliberately excludes the PDF, which
// is produced and hashed in the final manifest later.
func WriteEvidencePackage(db *sql.DB, dir string) (bundleHash string, manifest EvidenceManifest, err error) {
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return "", EvidenceManifest{}, err
	}
	for _, d := range Datasets {
		path := filepath.Join(dataDir, d.File)
		f, err := os.Create(path)
		if err != nil {
			return "", EvidenceManifest{}, err
		}
		if err := dumpTable(db, f, d.Table, d.OrderBy); err != nil {
			f.Close()
			return "", EvidenceManifest{}, err
		}
		if err := f.Close(); err != nil {
			return "", EvidenceManifest{}, err
		}
		sum, size, err := hashFile(path)
		if err != nil {
			return "", EvidenceManifest{}, err
		}
		manifest.Files = append(manifest.Files, ManifestEntry{File: "data/" + d.File, SHA256: sum, Size: size})
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].File < manifest.Files[j].File })

	canonical, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", EvidenceManifest{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "evidence-manifest.json"), canonical, 0o640); err != nil {
		return "", EvidenceManifest{}, err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), manifest, nil
}

// VerifyEvidencePackage recomputes every file's hash and checks it against the
// manifest, and re-derives the evidence-bundle hash. Any mismatch fails.
func VerifyEvidencePackage(dir string) (ok bool, badFile string, bundleHash string, err error) {
	raw, err := os.ReadFile(filepath.Join(dir, "evidence-manifest.json"))
	if err != nil {
		return false, "", "", err
	}
	var m EvidenceManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return false, "", "", err
	}
	for _, e := range m.Files {
		sum, _, err := hashFile(filepath.Join(dir, e.File))
		if err != nil {
			return false, e.File, "", nil
		}
		if sum != e.SHA256 {
			return false, e.File, "", nil
		}
	}
	// Re-derive the bundle hash from the canonical re-serialisation.
	canonical, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return false, "", "", err
	}
	sum := sha256.Sum256(canonical)
	bundleHash = hex.EncodeToString(sum[:])
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != bundleHash {
		// The on-disk manifest itself was altered.
		return false, "evidence-manifest.json", bundleHash, nil
	}
	return true, "", bundleHash, nil
}
