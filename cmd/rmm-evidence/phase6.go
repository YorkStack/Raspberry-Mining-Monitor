package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/backup"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/finalmanifest"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/signing"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

var gitRev = "unknown"

// reportRow fetches a report's package details for a period (latest revision by
// default, or a specific revision).
func reportRow(db *store.DB, period string, revision int) (reportID, packageDir, bundle string, rev int, err error) {
	q := `SELECT report_id, package_dir, evidence_bundle_hash, revision FROM reports WHERE period = ?`
	args := []any{period}
	if revision >= 0 {
		q += " AND revision = ?"
		args = append(args, revision)
	}
	q += " ORDER BY revision DESC LIMIT 1"
	err = db.SQL().QueryRow(q, args...).Scan(&reportID, &packageDir, &bundle, &rev)
	return
}

func finalize(args []string, db *store.DB, dataDir string, now time.Time) error {
	fs := flag.NewFlagSet("finalize", flag.ContinueOnError)
	period := fs.String("period", "", "reporting period (required)")
	revision := fs.Int("revision", -1, "specific revision (default: latest)")
	backupTarget := fs.String("backup", "", "optional backup target directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *period == "" {
		return fmt.Errorf("finalize: --period is required")
	}
	reportID, packageDir, bundle, rev, err := reportRow(db, *period, *revision)
	if err != nil {
		return fmt.Errorf("finalize: no closed report for %s: %w", *period, err)
	}

	keyStore := signing.NewStore(db.SQL())
	keyPath := filepath.Join(dataDir, "keys", "evidence-signing.key")
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return err
	}
	key, err := signing.EnsureKey(keyStore, keyPath, now)
	if err != nil {
		return err
	}

	schemaVersion, _ := db.SchemaVersion()
	fm, canonical, err := finalmanifest.Build(packageDir, finalmanifest.Meta{
		ReportID: reportID, Period: *period, Revision: rev, EvidenceBundleHash: bundle,
		SoftwareVersion: version, GitCommit: gitRev, SchemaVersion: schemaVersion,
		SigningKeyID: key.KeyID(), GeneratedAt: now,
	})
	if err != nil {
		return err
	}
	if err := finalmanifest.Sign(packageDir, canonical, key); err != nil {
		return err
	}
	if err := finalmanifest.Record(db.SQL(),
		fm, filepath.Join(packageDir, "final-manifest.sig"), filepath.Join(packageDir, "final-manifest.json"), now); err != nil {
		return err
	}
	fmt.Printf("finalised %s\n  signing key: %s\n  evidence-bundle hash: %s\n  final-manifest signed: %s\n",
		reportID, key.KeyID(), bundle, filepath.Join(packageDir, "final-manifest.sig"))

	if *backupTarget != "" {
		dst := filepath.Join(*backupTarget, *period, fmt.Sprintf("revision-%03d", rev))
		r, err := backup.Copy(packageDir, dst)
		if err != nil {
			return err
		}
		if err := backup.Record(db.SQL(), reportID, dst, r, now); err != nil {
			return err
		}
		if !r.Verified {
			return fmt.Errorf("backup FAILED verification at %s", r.Bad)
		}
		fmt.Printf("  backup: %d files copied and verified to %s\n", r.FilesCopied, dst)
	}
	return nil
}

func verifyFinal(args []string, db *store.DB) error {
	fs := flag.NewFlagSet("verify-final", flag.ContinueOnError)
	dir := fs.String("dir", "", "package directory containing final-manifest.json (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("verify-final: --dir is required")
	}
	raw, err := os.ReadFile(filepath.Join(*dir, "final-manifest.json"))
	if err != nil {
		return err
	}
	var fm finalmanifest.FinalManifest
	if err := json.Unmarshal(raw, &fm); err != nil {
		return err
	}
	pub, err := signing.NewStore(db.SQL()).PublicKey(fm.SigningKeyID)
	if err != nil {
		return fmt.Errorf("verify-final: unknown signing key %q: %w", fm.SigningKeyID, err)
	}
	ok, bad, err := finalmanifest.Verify(*dir, pub)
	if err != nil {
		return err
	}
	if ok {
		fmt.Printf("final manifest OK (signed by %s)\n  evidence-bundle hash: %s\n", fm.SigningKeyID, fm.EvidenceBundleHash)
		return nil
	}
	return fmt.Errorf("final manifest FAILED verification at %s", bad)
}
