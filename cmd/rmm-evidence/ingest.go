package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/expected"
	evnet "github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/network"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/telemetry"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/subsidy"
)

// snapshotDoc is the subset of the monitor's /api/v1/snapshot we ingest.
type snapshotDoc struct {
	Miners []struct {
		Name           string   `json:"name"`
		Online         bool     `json:"online"`
		HashrateThs    float64  `json:"hashrateThs"`
		PowerW         *float64 `json:"powerW"`
		ASICTempC      *float64 `json:"asicTempC"`
		VRMTempC       *float64 `json:"vrmTempC"`
		SharesAccepted *int64   `json:"sharesAccepted"`
		SharesRejected *int64   `json:"sharesRejected"`
	} `json:"miners"`
	Totals struct {
		HashrateThs float64 `json:"hashrateThs"`
	} `json:"totals"`
	Network struct {
		Height            int64   `json:"height"`
		Difficulty        float64 `json:"difficulty"`
		NetworkHashrateHs float64 `json:"networkHashrateHs"`
		PriceEUR          float64 `json:"priceEur"`
	} `json:"network"`
}

func thsToHs(ths float64) int64 { return int64(ths * 1e12) }

// ingest reads one monitor snapshot (from a URL or a file) and records per-miner
// telemetry, a daily network snapshot (append-only) and the contemporaneous
// expected value for the fleet.
func ingest(args []string, db *store.DB, loc *time.Location) error {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	from := fs.String("from", "http://127.0.0.1:8080/api/v1/snapshot", "monitor snapshot URL")
	file := fs.String("file", "", "read the snapshot JSON from a file instead of a URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var raw []byte
	var err error
	if *file != "" {
		raw, err = os.ReadFile(*file)
	} else {
		raw, err = httpGet(*from)
	}
	if err != nil {
		return err
	}

	var doc snapshotDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("ingest: parse snapshot: %w", err)
	}

	now := time.Now().UTC()

	// 1. Per-miner telemetry.
	tel := telemetry.New(db.SQL(), time.Minute)
	samples := make([]telemetry.Sample, 0, len(doc.Miners))
	for _, m := range doc.Miners {
		s := telemetry.Sample{
			MinerInternalID: m.Name, TsUTC: now, Online: m.Online, APIAvailable: true,
			ASICTempC: m.ASICTempC, VRMTempC: m.VRMTempC,
			AcceptedShares: m.SharesAccepted, RejectedShares: m.SharesRejected,
		}
		if m.Online {
			hs := thsToHs(m.HashrateThs)
			s.HashrateHs = &hs
		}
		s.PowerW = m.PowerW
		samples = append(samples, s)
	}
	if err := tel.RecordRaw(samples...); err != nil {
		return err
	}

	// 2. Daily network snapshot (append-only: first of the day wins).
	uid := "daily-" + now.Format("2006-01-02")
	subsidySat := int64(subsidy.Sats(uint32(doc.Network.Height)))
	netStore := evnet.New(db.SQL(), loc)
	nsInserted, err := netStore.Record(evnet.Snapshot{
		UID: uid, TsUTC: now, BlockHeight: doc.Network.Height, Difficulty: doc.Network.Difficulty,
		NetworkHashrateHs: doc.Network.NetworkHashrateHs, SubsidySat: subsidySat,
		RewardPerBlockSat: subsidySat, Source: "rmm-monitor", SourceEndpoint: *from, APIRetrievedAt: now,
		DataQuality: "ok",
	}, raw, now)
	if err != nil {
		return err
	}

	// 3. Contemporaneous expected value for the fleet, tied to the day's snapshot.
	exp := expected.New(db.SQL())
	in := expected.Inputs{
		MinerHashrateHs: doc.Totals.HashrateThs * 1e12, NetworkHashrateHs: doc.Network.NetworkHashrateHs,
		Difficulty: doc.Network.Difficulty, RewardPerBlockSat: subsidySat,
	}
	if doc.Network.PriceEUR > 0 {
		cents := int64(doc.Network.PriceEUR * 100)
		in.BTCPriceCents = &cents
	}
	evInserted, r, err := exp.Record(uid, now, in, now)
	if err != nil {
		return err
	}

	fmt.Printf("ingested: %d miners, network snapshot %s (%s), expected %d sat/day (%s)\n",
		len(samples), uid, boolNote(nsInserted, "new", "existing"),
		r.ExpectedSatDay, boolNote(evInserted, "recorded", "already frozen"))
	return nil
}

func boolNote(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

func httpGet(url string) ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("ingest: GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ingest: GET %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}
