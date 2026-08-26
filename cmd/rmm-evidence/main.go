// Command rmm-evidence is the Mining Evidence & Tax Documentation service.
//
// It documents objective technical facts about mining activity. It does not
// determine the legal or tax classification of that activity.
//
// This is the foundation phase: it opens the evidence database, runs
// migrations, and offers minimal commands for the versioned miner inventory,
// document storage, and audit-log verification. Later phases add telemetry,
// rewards, valuation, reporting and PDF/A export.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/config"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/document"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/miner"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

var version = "0.5.0"

const disclaimer = "Technical factual documentation only. This report does not " +
	"determine the legal or tax classification of the mining activity."

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "rmm-evidence:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("rmm-evidence", flag.ContinueOnError)
	configPath := fs.String("config", "evidence.yaml", "path to the evidence configuration")
	actor := fs.String("actor", "operator", "who is performing the action, for the audit log")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cmd := fs.Arg(0)
	if cmd == "" || cmd == "help" {
		usage()
		return nil
	}
	if cmd == "version" {
		fmt.Println("rmm-evidence", version)
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	loc, _ := cfg.Location()

	if err := os.MkdirAll(cfg.DataDirectory, 0o750); err != nil {
		return err
	}
	db, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(time.Now()); err != nil {
		return err
	}
	log := audit.New(db.SQL(), loc)

	switch cmd {
	case "init":
		v, _ := db.SchemaVersion()
		fmt.Printf("evidence database ready at %s (schema v%d)\n", cfg.DBPath(), v)
		fmt.Println(disclaimer)
		return nil

	case "miner-add":
		return minerAdd(fs.Args()[1:], miner.New(db.SQL(), log), *actor)

	case "miner-list":
		return minerList(db, miner.New(db.SQL(), log))

	case "doc-add":
		return docAdd(fs.Args()[1:], document.New(db.SQL(), log, cfg.DataDirectory), *actor)

	case "ingest":
		return ingest(fs.Args()[1:], db, loc)

	case "watch-add":
		return watchAdd(fs.Args()[1:], db, log, *actor)
	case "watch-list":
		return watchList(db, log)
	case "policy-set":
		return policySet(fs.Args()[1:], db, log, *actor)
	case "cost-add":
		return costAdd(fs.Args()[1:], db, log, *actor)
	case "cost-summary":
		return costSummary(fs.Args()[1:], db, log)
	case "period-validate":
		return periodValidate(fs.Args()[1:], db, log, cfg.DataDirectory)
	case "period-close":
		return periodClose(fs.Args()[1:], db, log, cfg.DataDirectory, *actor)
	case "period-revise":
		return periodRevise(fs.Args()[1:], db, log, cfg.DataDirectory, *actor)
	case "verify":
		return verifyPackage(fs.Args()[1:])

	case "audit-verify":
		ok, brokenID, err := log.Verify()
		if err != nil {
			return err
		}
		n, _ := log.Count()
		if ok {
			fmt.Printf("audit log OK: %d entries, hash chain intact\n", n)
			return nil
		}
		return fmt.Errorf("audit log BROKEN at entry id %d (of %d)", brokenID, n)

	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func minerAdd(args []string, s *miner.Store, actor string) error {
	fs := flag.NewFlagSet("miner-add", flag.ContinueOnError)
	id := fs.String("id", "", "internal miner id (required)")
	manu := fs.String("manufacturer", "", "manufacturer")
	model := fs.String("model", "", "model")
	serial := fs.String("serial", "", "serial number")
	priceCents := fs.Int64("price-cents", -1, "purchase price in euro cents")
	hashrate := fs.Int64("hashrate-hs", -1, "nominal hashrate in H/s")
	powerW := fs.Int64("power-w", -1, "nominal power in watts")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return fmt.Errorf("miner-add: --id is required")
	}
	m := miner.Master{Manufacturer: *manu, Model: *model, SerialNumber: *serial}
	if *priceCents >= 0 {
		m.PurchasePriceCents = priceCents
	}
	if *hashrate >= 0 {
		m.NominalHashrateHs = hashrate
	}
	if *powerW >= 0 {
		m.NominalPowerW = powerW
	}
	if _, err := s.Create(*id, m, actor, time.Now()); err != nil {
		return err
	}
	fmt.Printf("miner %q added (version 1)\n", *id)
	return nil
}

func minerList(db *store.DB, _ *miner.Store) error {
	rows, err := db.SQL().Query(`SELECT m.internal_id, mv.version, mv.model, mv.serial_number
		FROM miner_versions mv JOIN miners m ON m.id = mv.miner_id
		WHERE mv.superseded_at IS NULL ORDER BY m.internal_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	fmt.Printf("%-16s %-8s %-20s %s\n", "INTERNAL_ID", "VERSION", "MODEL", "SERIAL")
	for rows.Next() {
		var id, model, serial string
		var v int
		if err := rows.Scan(&id, &v, &model, &serial); err != nil {
			return err
		}
		fmt.Printf("%-16s v%-7d %-20s %s\n", id, v, model, serial)
	}
	return rows.Err()
}

func docAdd(args []string, s *document.Store, actor string) error {
	fs := flag.NewFlagSet("doc-add", flag.ContinueOnError)
	docType := fs.String("type", "other", "document type")
	path := fs.String("file", "", "path to the file to store (required)")
	desc := fs.String("description", "", "description")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return fmt.Errorf("doc-add: --file is required")
	}
	content, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	name := *path
	if i := lastSep(name); i >= 0 {
		name = name[i+1:]
	}
	d, err := s.Add(*docType, name, content, document.Meta{Description: *desc}, actor, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("document %q stored as version %d (sha256 %s…)\n", d.OriginalFilename, d.Version, d.SHA256[:12])
	return nil
}

func lastSep(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' || s[i] == '\\' {
			return i
		}
	}
	return -1
}

func usage() {
	fmt.Fprintln(os.Stderr, `rmm-evidence — Mining Evidence & Tax Documentation (foundation)

Usage:
  rmm-evidence [--config evidence.yaml] [--actor NAME] <command>

Commands:
  init           open the database and run migrations
  miner-add      add a miner to the versioned inventory (--id required)
  miner-list     list current miner versions
  doc-add        store a document with a content hash (--file required)
  ingest         record a monitor snapshot: telemetry + daily network snapshot + expected value
  watch-add      watch a payout address (--address); watch-list lists them
  policy-set     set the EUR valuation policy for a tax year (--year)
  cost-add       record a cost (--date --description --category --gross-cents)
  cost-summary   preliminary factual cost summary for a period (--period)
  period-validate  list pre-close warnings for a period (--period)
  period-close     close a period and write the evidence package (--period [--acknowledge])
  period-revise    create a revision of a closed period (--period --reason)
  verify           verify an evidence package against its manifest (--dir)
  audit-verify   verify the audit-log hash chain
  version        print the version

Technical factual documentation only. Does not determine tax classification.`)
}
