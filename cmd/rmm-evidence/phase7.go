package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/annual"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/backup"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/serve"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/signing"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

// annualClose rolls the year's monthly evidence packages into one signed
// annual package for the tax adviser.
func annualClose(args []string, db *store.DB, log *audit.Log, dataDir string, now time.Time) error {
	fs := flag.NewFlagSet("annual", flag.ContinueOnError)
	year := fs.String("year", "", "tax year, e.g. 2026 (required)")
	backupTarget := fs.String("backup", "", "optional backup target directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *year == "" {
		return fmt.Errorf("annual: --year is required")
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

	dir := filepath.Join(dataDir, "annual", *year)
	m, bundle, err := annual.Build(db.SQL(), dir, key, annual.Meta{
		Year: *year, SoftwareVersion: version, GitCommit: gitRev, SchemaVersion: schemaVersion,
	}, now)
	if err != nil {
		return err
	}
	if err := annual.Record(db.SQL(), m, dir, bundle, key, now); err != nil {
		return err
	}
	if _, err := log.Append(audit.Event{
		EventUID: "annual-" + *year + "-" + now.UTC().Format(time.RFC3339Nano),
		TsUTC:    now, Actor: "operator", Type: "annual.generated", Entity: "annual", EntityID: *year,
		NewValueHash: bundle,
	}); err != nil {
		return err
	}

	allVerified := true
	for _, p := range m.Periods {
		if !p.Verified {
			allVerified = false
		}
	}
	fmt.Printf("annual package %s\n  months included: %d (all verified: %t)\n  annual-bundle hash: %s\n  signed by: %s\n  directory: %s\n",
		*year, len(m.Periods), allVerified, bundle, key.KeyID(), dir)
	for _, p := range m.Periods {
		mark := "OK"
		if !p.Verified {
			mark = "FAILED"
		}
		fmt.Printf("    %s  %s (rev %d)  %s\n", p.Period, p.ReportID, p.Revision, mark)
	}
	if !allVerified {
		return fmt.Errorf("annual: one or more included packages failed verification")
	}

	if *backupTarget != "" {
		dst := filepath.Join(*backupTarget, "annual", *year)
		r, err := backup.Copy(dir, dst)
		if err != nil {
			return err
		}
		if err := backup.Record(db.SQL(), "ANNUAL-"+*year, dst, r, now); err != nil {
			return err
		}
		if !r.Verified {
			return fmt.Errorf("backup FAILED verification at %s", r.Bad)
		}
		fmt.Printf("  backup: %d files copied and verified to %s\n", r.FilesCopied, dst)
	}
	fmt.Println(disclaimer)
	return nil
}

// verifyAnnual re-verifies a signed annual package on disk.
func verifyAnnual(args []string, db *store.DB) error {
	fs := flag.NewFlagSet("verify-annual", flag.ContinueOnError)
	dir := fs.String("dir", "", "annual package directory (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("verify-annual: --dir is required")
	}
	m, bundle, err := annual.ReadManifest(*dir)
	if err != nil {
		return err
	}
	pub, err := signing.NewStore(db.SQL()).PublicKey(m.SigningKeyID)
	if err != nil {
		return fmt.Errorf("verify-annual: unknown signing key %q: %w", m.SigningKeyID, err)
	}
	ok, bad, err := annual.Verify(*dir, pub)
	if err != nil {
		return err
	}
	if ok {
		fmt.Printf("annual package %s OK (signed by %s, %d months)\n  annual-bundle hash: %s\n",
			m.Year, m.SigningKeyID, len(m.Periods), bundle)
		return nil
	}
	return fmt.Errorf("annual package FAILED verification at %s", bad)
}

// serveStatus runs the read-only Tax & Evidence status server.
func serveStatus(args []string, db *store.DB) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8090", "listen address (bind to localhost only unless you know what you are doing)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	h := serve.Handler(db.SQL(), version)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Printf("Tax & Evidence status server (read-only) on http://%s\n", *addr)
	fmt.Println(disclaimer)
	return srv.ListenAndServe()
}
