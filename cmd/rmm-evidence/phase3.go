package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/reward"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/valuation"
)

func watchAdd(args []string, db *store.DB, log *audit.Log, actor string) error {
	fs := flag.NewFlagSet("watch-add", flag.ContinueOnError)
	addr := fs.String("address", "", "watch-only Bitcoin address (required)")
	label := fs.String("label", "", "label")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *addr == "" {
		return fmt.Errorf("watch-add: --address is required")
	}
	if err := reward.New(db.SQL(), log).AddAddress(*addr, *label, actor, time.Now()); err != nil {
		return err
	}
	fmt.Printf("watching %s\n", *addr)
	return nil
}

func watchList(db *store.DB, log *audit.Log) error {
	addrs, err := reward.New(db.SQL(), log).ActiveAddresses()
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		fmt.Println("(no watched addresses)")
		return nil
	}
	for _, a := range addrs {
		fmt.Println(a)
	}
	return nil
}

func policySet(args []string, db *store.DB, log *audit.Log, actor string) error {
	fs := flag.NewFlagSet("policy-set", flag.ContinueOnError)
	year := fs.Int("year", 0, "tax year (required)")
	method := fs.String("method", "CLOSE", "SPOT|OPEN|CLOSE|AVERAGE|MANUAL")
	primary := fs.String("primary", "KRAKEN", "primary rate source")
	fallback := fs.String("fallback", "COINBASE", "fallback rate source")
	tz := fs.String("timezone", "Europe/Berlin", "valuation timezone")
	rounding := fs.String("rounding", "HALF_UP", "rounding")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *year == 0 {
		return fmt.Errorf("policy-set: --year is required")
	}
	v, err := valuation.New(db.SQL(), log).SetPolicy(valuation.Policy{
		TaxYear: *year, Currency: "EUR", Method: *method, PrimarySource: *primary,
		FallbackSource: *fallback, Timezone: *tz, Rounding: *rounding, DecimalPrecision: 2,
	}, actor, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("valuation policy for %d set (version %d)\n", *year, v)
	return nil
}
