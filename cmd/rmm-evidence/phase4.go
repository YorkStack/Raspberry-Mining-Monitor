package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/cost"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

func costAdd(args []string, db *store.DB, log *audit.Log, actor string) error {
	fs := flag.NewFlagSet("cost-add", flag.ContinueOnError)
	date := fs.String("date", "", "cost date YYYY-MM-DD (required)")
	desc := fs.String("description", "", "description (required)")
	category := fs.String("category", "", "category, e.g. hardware/electricity/repairs (required)")
	gross := fs.Int64("gross-cents", -1, "gross amount in euro cents (required)")
	period := fs.String("period", "", "reporting period, e.g. 2026-08")
	miner := fs.String("miner", "", "associated miner internal id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *date == "" || *desc == "" || *category == "" || *gross < 0 {
		return fmt.Errorf("cost-add: --date, --description, --category and --gross-cents are required")
	}
	id, err := cost.New(db.SQL(), log).Add(cost.Cost{
		Date: *date, Description: *desc, Category: *category, GrossCents: *gross,
		ReportingPeriod: *period, MinerInternalID: *miner,
	}, actor, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("cost #%d added: %s %.2f EUR (%s)\n", id, *category, float64(*gross)/100, *desc)
	return nil
}

func costSummary(args []string, db *store.DB, log *audit.Log) error {
	fs := flag.NewFlagSet("cost-summary", flag.ContinueOnError)
	period := fs.String("period", "", "reporting period, e.g. 2026-08 (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *period == "" {
		return fmt.Errorf("cost-summary: --period is required")
	}
	sums, err := cost.New(db.SQL(), log).Summary(*period)
	if err != nil {
		return err
	}
	fmt.Printf("Preliminary factual summary for %s (not a tax computation):\n", *period)
	var total int64
	for _, s := range sums {
		fmt.Printf("  %-16s %10.2f EUR  (%d)\n", s.Category, float64(s.TotalGrossCents)/100, s.Count)
		total += s.TotalGrossCents
	}
	fmt.Printf("  %-16s %10.2f EUR\n", "TOTAL", float64(total)/100)
	fmt.Println("Tax treatment requires professional review.")
	return nil
}
