package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/export"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/report"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

func reportStore(db *store.DB, log *audit.Log, dataDir string) *report.Store {
	return report.New(db.SQL(), log, filepath.Join(dataDir, "reports"), version)
}

func periodValidate(args []string, db *store.DB, log *audit.Log, dataDir string) error {
	fs := flag.NewFlagSet("period-validate", flag.ContinueOnError)
	period := fs.String("period", "", "reporting period, e.g. 2026-08 (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *period == "" {
		return fmt.Errorf("period-validate: --period is required")
	}
	warnings, err := reportStore(db, log, dataDir).Validate(*period)
	if err != nil {
		return err
	}
	if len(warnings) == 0 {
		fmt.Printf("%s: no warnings; ready to close\n", *period)
		return nil
	}
	fmt.Printf("%s: %d warning(s):\n", *period, len(warnings))
	for _, w := range warnings {
		fmt.Printf("  [%s] %s\n", w.Code, w.Message)
	}
	fmt.Println("Close with --acknowledge to proceed; the warnings are recorded in the report.")
	return nil
}

func periodClose(args []string, db *store.DB, log *audit.Log, dataDir, actor string) error {
	fs := flag.NewFlagSet("period-close", flag.ContinueOnError)
	period := fs.String("period", "", "reporting period, e.g. 2026-08 (required)")
	ack := fs.Bool("acknowledge", false, "acknowledge warnings and close anyway")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *period == "" {
		return fmt.Errorf("period-close: --period is required")
	}
	id, bundle, warnings, err := reportStore(db, log, dataDir).Close(*period, *ack, actor, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("closed %s\n  evidence-bundle hash: %s\n", id, bundle)
	if len(warnings) > 0 {
		fmt.Printf("  closed with %d acknowledged warning(s)\n", len(warnings))
	}
	fmt.Println("Technical factual documentation only. Does not determine tax classification.")
	return nil
}

func periodRevise(args []string, db *store.DB, log *audit.Log, dataDir, actor string) error {
	fs := flag.NewFlagSet("period-revise", flag.ContinueOnError)
	period := fs.String("period", "", "reporting period (required)")
	reason := fs.String("reason", "", "reason for the revision (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *period == "" || *reason == "" {
		return fmt.Errorf("period-revise: --period and --reason are required")
	}
	id, bundle, err := reportStore(db, log, dataDir).Revise(*period, *reason, actor, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("created %s\n  evidence-bundle hash: %s\n", id, bundle)
	return nil
}

func verifyPackage(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	dir := fs.String("dir", "", "evidence package directory (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("verify: --dir is required")
	}
	ok, bad, bundle, err := export.VerifyEvidencePackage(*dir)
	if err != nil {
		return err
	}
	if ok {
		fmt.Printf("package OK\n  evidence-bundle hash: %s\n", bundle)
		return nil
	}
	return fmt.Errorf("package FAILED verification at %s", bad)
}
