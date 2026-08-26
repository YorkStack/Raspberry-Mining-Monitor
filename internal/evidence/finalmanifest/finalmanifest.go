// Package finalmanifest builds and signs the stage-2 final manifest.
//
// After the export files (and later the PDF) are finalised, it hashes them all,
// records the evidence-manifest hash, the evidence-bundle hash and — when
// present — the final PDF hash, plus build metadata, into final-manifest.json,
// then signs that file with a dedicated key (detached signature). The final PDF
// hash is never embedded inside the PDF itself, which would be circular.
package finalmanifest

import (
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/export"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/signing"
)

// FileHash is one file's integrity record.
type FileHash struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// FinalManifest is the stage-2 manifest.
type FinalManifest struct {
	ReportID               string     `json:"reportId"`
	Period                 string     `json:"period"`
	Revision               int        `json:"revision"`
	EvidenceManifestSHA256 string     `json:"evidenceManifestSha256"`
	EvidenceBundleHash     string     `json:"evidenceBundleHash"`
	FinalPDFSHA256         string     `json:"finalPdfSha256,omitempty"`
	Files                  []FileHash `json:"files"`
	GeneratedAt            string     `json:"generatedAt"`
	SoftwareVersion        string     `json:"softwareVersion"`
	GitCommit              string     `json:"gitCommit"`
	SchemaVersion          int        `json:"schemaVersion"`
	SigningKeyID           string     `json:"signingKeyId"`
}

// Meta carries the build-time metadata.
type Meta struct {
	ReportID           string
	Period             string
	Revision           int
	EvidenceBundleHash string
	PDFRelPath         string // e.g. "summary/report.pdf"; empty if no PDF yet
	SoftwareVersion    string
	GitCommit          string
	SchemaVersion      int
	SigningKeyID       string
	GeneratedAt        time.Time
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

// filesToHash lists the export files under dir that the final manifest covers:
// the dataset CSVs, the evidence manifest, and the PDF when present.
func filesToHash(dir string, pdfRel string) []string {
	var out []string
	for _, d := range export.Datasets {
		out = append(out, "data/"+d.File)
	}
	out = append(out, "evidence-manifest.json")
	if pdfRel != "" {
		out = append(out, pdfRel)
	}
	sort.Strings(out)
	return out
}

// Build writes final-manifest.json and returns the manifest plus its canonical
// bytes (which are what gets signed).
func Build(dir string, m Meta) (FinalManifest, []byte, error) {
	fm := FinalManifest{
		ReportID: m.ReportID, Period: m.Period, Revision: m.Revision,
		EvidenceBundleHash: m.EvidenceBundleHash, GeneratedAt: m.GeneratedAt.UTC().Format(time.RFC3339),
		SoftwareVersion: m.SoftwareVersion, GitCommit: m.GitCommit, SchemaVersion: m.SchemaVersion,
		SigningKeyID: m.SigningKeyID,
	}
	if h, _, err := hashFile(filepath.Join(dir, "evidence-manifest.json")); err == nil {
		fm.EvidenceManifestSHA256 = h
	}
	for _, rel := range filesToHash(dir, m.PDFRelPath) {
		h, size, err := hashFile(filepath.Join(dir, rel))
		if err != nil {
			return FinalManifest{}, nil, err
		}
		fm.Files = append(fm.Files, FileHash{File: rel, SHA256: h, Size: size})
		if rel == m.PDFRelPath {
			fm.FinalPDFSHA256 = h
		}
	}
	canonical, err := json.MarshalIndent(fm, "", "  ")
	if err != nil {
		return FinalManifest{}, nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "final-manifest.json"), canonical, 0o640); err != nil {
		return FinalManifest{}, nil, err
	}
	return fm, canonical, nil
}

// Sign writes a detached base64 signature of the manifest bytes.
func Sign(dir string, canonical []byte, key *signing.Key) error {
	sig := key.Sign(canonical)
	return os.WriteFile(filepath.Join(dir, "final-manifest.sig"),
		[]byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o640)
}

// Verify checks the signature over final-manifest.json and recomputes every
// listed file's hash. Any mismatch fails.
func Verify(dir string, pub ed25519.PublicKey) (ok bool, badFile string, err error) {
	raw, err := os.ReadFile(filepath.Join(dir, "final-manifest.json"))
	if err != nil {
		return false, "", err
	}
	sigB64, err := os.ReadFile(filepath.Join(dir, "final-manifest.sig"))
	if err != nil {
		return false, "", err
	}
	sig, err := base64.StdEncoding.DecodeString(trim(string(sigB64)))
	if err != nil {
		return false, "", err
	}
	if !signing.Verify(pub, raw, sig) {
		return false, "final-manifest.json", nil
	}
	var fm FinalManifest
	if err := json.Unmarshal(raw, &fm); err != nil {
		return false, "", err
	}
	for _, f := range fm.Files {
		h, _, err := hashFile(filepath.Join(dir, f.File))
		if err != nil {
			return false, f.File, nil
		}
		if h != f.SHA256 {
			return false, f.File, nil
		}
	}
	return true, "", nil
}

// Record stores the final manifest in the database.
func Record(db *sql.DB, fm FinalManifest, sigPath, manifestPath string, now time.Time) error {
	sig := ""
	if b, err := os.ReadFile(sigPath); err == nil {
		sig = trim(string(b))
	}
	manifestSHA := ""
	if h, _, err := hashFile(manifestPath); err == nil {
		manifestSHA = h
	}
	_, err := db.Exec(`INSERT INTO final_manifests
		(report_id, evidence_bundle_hash, final_pdf_sha256, manifest_sha256, signing_key_id, signature,
		 manifest_path, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		fm.ReportID, fm.EvidenceBundleHash, nullStr(fm.FinalPDFSHA256), manifestSHA, fm.SigningKeyID, sig,
		manifestPath, now.UTC().Format(time.RFC3339))
	return err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func trim(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
