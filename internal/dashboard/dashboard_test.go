package dashboard

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/bitcoin"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/model"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/pool"
)

var now = time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

const fullAddress = "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq"

func f(v float64) *float64 { return &v }
func u(v uint64) *uint64   { return &v }

func onlineMiner(name string, ths, watts float64, temp float64) miner.Snapshot {
	return miner.Snapshot{
		Source:         model.Source{FetchedAt: now, OK: true},
		Name:           name,
		Model:          "BM1370",
		HashrateTHs:    ths,
		PowerW:         f(watts),
		ASICTempC:      f(temp),
		VRMTempC:       f(temp + 8),
		SharesAccepted: u(1284),
		SharesRejected: u(2),
		BestDiff:       f(18.7e9),
		PoolUser:       fullAddress,
	}
}

func reference() Input {
	return Input{
		Miners: []miner.Snapshot{
			onlineMiner("NerdOctaxe", 12.10, 158, 62),
			onlineMiner("Gamma 602", 1.27, 18, 55),
		},
		MinerInterval:   2 * time.Second,
		PoolInterval:    60 * time.Second,
		NetworkInterval: 30 * time.Second,
		Pool: pool.Snapshot{
			Source:         model.Source{FetchedAt: now, OK: true},
			Provider:       "publicpool",
			BestDifficulty: f(18.7e9),
			WorkersCount:   2,
		},
		Network: bitcoin.Snapshot{
			Source:            model.Source{FetchedAt: now, OK: true},
			Kind:              bitcoin.SourcePublic,
			Height:            963692,
			Difficulty:        125_807_076_547_197.5,
			NetworkHashrateHs: 907_782_986_431_433_900_000,
			PriceEUR:          65_515,
			LastBlockTime:     now.Add(-222 * time.Second),
		},
		Thresholds: Thresholds{ASICWarnC: 70, ASICCritC: 80, VRMWarnC: 80, VRMCritC: 90},
	}
}

func TestBuildTotals(t *testing.T) {
	v := Build(reference(), now)
	if math.Abs(v.Totals.HashrateTHs-13.37) > 1e-9 {
		t.Errorf("HashrateTHs = %v, want 13.37", v.Totals.HashrateTHs)
	}
	if math.Abs(v.Totals.PowerW-176) > 1e-9 {
		t.Errorf("PowerW = %v, want 176", v.Totals.PowerW)
	}
	if v.Totals.EfficiencyJTH == nil {
		t.Fatal("EfficiencyJTH = nil, want a value")
	}
	if math.Abs(*v.Totals.EfficiencyJTH-13.164) > 1e-3 {
		t.Errorf("EfficiencyJTH = %v, want ~13.164", *v.Totals.EfficiencyJTH)
	}
	if v.Totals.MinersOnline != 2 || v.Totals.MinersTotal != 2 {
		t.Errorf("online/total = %d/%d, want 2/2", v.Totals.MinersOnline, v.Totals.MinersTotal)
	}
}

// The payout address must never reach the browser in full.
func TestBuiltViewNeverContainsTheFullPayoutAddress(t *testing.T) {
	v := Build(reference(), now)

	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), fullAddress) {
		t.Fatalf("serialised dashboard leaks the full payout address:\n%s", encoded)
	}
	if v.Miners[0].PoolUserMasked != "bc1q…5mdq" {
		t.Errorf("PoolUserMasked = %q, want %q", v.Miners[0].PoolUserMasked, "bc1q…5mdq")
	}
}

func TestMaskAddress(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{fullAddress, "bc1q…5mdq"},
		{"", ""},
		{"short", "…"},
		{"bc1qabcdef", "bc1q…cdef"},
	}
	for _, c := range cases {
		if got := MaskAddress(c.in); got != c.want {
			t.Errorf("MaskAddress(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestProbabilityUsesTotalHashrateAndNetworkDifficulty(t *testing.T) {
	v := Build(reference(), now)
	if v.Probability == nil {
		t.Fatal("Probability = nil, want a value")
	}
	if math.Abs(v.Probability.Day-2.13786e-6)/2.13786e-6 > 1e-4 {
		t.Errorf("Day = %v, want ~2.13786e-6", v.Probability.Day)
	}
	if math.Abs(v.Probability.MedianYears-887.6) > 1 {
		t.Errorf("MedianYears = %v, want ~887.6", v.Probability.MedianYears)
	}
	if math.Abs(v.Probability.ShareOfNetwork-1.4728e-8)/1.4728e-8 > 1e-3 {
		t.Errorf("ShareOfNetwork = %v, want ~1.4728e-8", v.Probability.ShareOfNetwork)
	}
}

// Without network data there is no difficulty, so there is no probability to
// state. It must be absent rather than zero, which would read as certainty of
// never finding a block.
func TestProbabilityAbsentWithoutNetworkData(t *testing.T) {
	in := reference()
	in.Network = bitcoin.Snapshot{Kind: bitcoin.SourcePublic}

	if v := Build(in, now); v.Probability != nil {
		t.Errorf("Probability = %+v, want nil when the network source has no data", v.Probability)
	}
}

func TestProbabilityAbsentWhenAllMinersAreOffline(t *testing.T) {
	in := reference()
	for i := range in.Miners {
		in.Miners[i].OK = false
	}

	if v := Build(in, now); v.Probability != nil {
		t.Error("Probability should be nil when there is no hashrate")
	}
}

func TestPriceEURPassedThrough(t *testing.T) {
	v := Build(reference(), now)
	if v.Network.PriceEUR != 65_515 {
		t.Errorf("PriceEUR = %v, want 65515", v.Network.PriceEUR)
	}
}

func TestSubsidyComesFromHeight(t *testing.T) {
	v := Build(reference(), now)
	if v.Network.SubsidyBTC != 3.125 {
		t.Errorf("SubsidyBTC = %v, want 3.125", v.Network.SubsidyBTC)
	}
	if v.Network.NextHalvingHeight != 1_050_000 {
		t.Errorf("NextHalvingHeight = %d, want 1050000", v.Network.NextHalvingHeight)
	}
}

func TestSecondsSinceLastBlock(t *testing.T) {
	v := Build(reference(), now)
	if math.Abs(v.Network.SecondsSinceBlock-222) > 0.5 {
		t.Errorf("SecondsSinceBlock = %v, want 222", v.Network.SecondsSinceBlock)
	}
}

func TestTemperatureStatusThresholds(t *testing.T) {
	cases := []struct {
		temp float64
		want string
	}{
		{62, "ok"},
		{69.9, "ok"},
		{70, "warn"},
		{79.9, "warn"},
		{80, "crit"},
		{95, "crit"},
	}
	for _, c := range cases {
		in := reference()
		in.Miners[0].ASICTempC = f(c.temp)
		v := Build(in, now)
		if got := v.Miners[0].ASICTempStatus; got != c.want {
			t.Errorf("temp %v gave status %q, want %q", c.temp, got, c.want)
		}
	}
}

func TestTemperatureStatusUnknownWhenNotReported(t *testing.T) {
	in := reference()
	in.Miners[0].ASICTempC = nil
	v := Build(in, now)
	if v.Miners[0].ASICTempStatus != "unknown" {
		t.Errorf("ASICTempStatus = %q, want %q", v.Miners[0].ASICTempStatus, "unknown")
	}
}

func TestStaleSourceIsFlagged(t *testing.T) {
	in := reference()
	// Miner interval is 2s, so 7s without a successful fetch is stale.
	later := now.Add(7 * time.Second)

	v := Build(in, later)
	if !v.Miners[0].Stale {
		t.Error("miner should be stale after more than three poll intervals")
	}
	if v.Pool.Stale {
		t.Error("pool should still be fresh 7s into a 60s interval")
	}
	if math.Abs(v.Miners[0].AgeSeconds-7) > 0.001 {
		t.Errorf("AgeSeconds = %v, want 7", v.Miners[0].AgeSeconds)
	}
}

func TestOfflineMinerKeepsItsLastKnownValues(t *testing.T) {
	in := reference()
	in.Miners[0].OK = false
	in.Miners[0].Err = "dial tcp: connection refused"

	v := Build(in, now)
	if v.Miners[0].Online {
		t.Error("Online = true, want false")
	}
	if v.Miners[0].HashrateTHs != 12.10 {
		t.Errorf("HashrateTHs = %v; the last known value must stay on screen", v.Miners[0].HashrateTHs)
	}
	if v.Miners[0].Err == "" {
		t.Error("Err is empty, want the failure reason")
	}
}

func TestPerMinerEfficiency(t *testing.T) {
	v := Build(reference(), now)
	if v.Miners[0].EfficiencyJTH == nil {
		t.Fatal("EfficiencyJTH = nil, want a value")
	}
	if math.Abs(*v.Miners[0].EfficiencyJTH-13.058) > 1e-3 {
		t.Errorf("EfficiencyJTH = %v, want ~13.058", *v.Miners[0].EfficiencyJTH)
	}
}

func TestAcceptanceRatio(t *testing.T) {
	v := Build(reference(), now)
	if v.Miners[0].AcceptanceRatio == nil {
		t.Fatal("AcceptanceRatio = nil, want a value")
	}
	if math.Abs(*v.Miners[0].AcceptanceRatio-1284.0/1286.0) > 1e-9 {
		t.Errorf("AcceptanceRatio = %v", *v.Miners[0].AcceptanceRatio)
	}
}

func TestNetworkSourceLabel(t *testing.T) {
	v := Build(reference(), now)
	if v.Network.SourceLabel != "PUBLIC" {
		t.Errorf("SourceLabel = %q, want PUBLIC", v.Network.SourceLabel)
	}
}

// Rejected shares come from the miners when the pool cannot report them.
func TestRejectedSharesFallBackToMinerReportedCounts(t *testing.T) {
	v := Build(reference(), now)
	if !v.Pool.RejectedFromMiners {
		t.Error("RejectedFromMiners = false; Public Pool cannot report rejects, so the label must say the miners did")
	}
	if v.Pool.SharesRejected != 4 {
		t.Errorf("SharesRejected = %d, want 4 (2 per miner)", v.Pool.SharesRejected)
	}
	if v.Pool.SharesAccepted != 2568 {
		t.Errorf("SharesAccepted = %d, want 2568", v.Pool.SharesAccepted)
	}
}

func TestDisabledMinerIsHiddenFromTilesAndTotals(t *testing.T) {
	in := reference()
	in.DisabledMiners = map[string]bool{"Gamma 602": true}

	v := Build(in, now)
	if len(v.Miners) != 1 {
		t.Fatalf("got %d miners, want 1 (disabled one hidden)", len(v.Miners))
	}
	if v.Miners[0].Name != "NerdOctaxe" {
		t.Errorf("visible miner = %q, want NerdOctaxe", v.Miners[0].Name)
	}
	// Totals must exclude the disabled miner.
	if v.Totals.MinersTotal != 1 {
		t.Errorf("MinersTotal = %d, want 1", v.Totals.MinersTotal)
	}
	if v.Totals.HashrateTHs != 12.10 {
		t.Errorf("HashrateTHs = %v, want only the enabled miner's 12.10", v.Totals.HashrateTHs)
	}
}

func TestScreensaverSecondsPassedThrough(t *testing.T) {
	in := reference()
	in.ScreensaverSeconds = 900
	if v := Build(in, now); v.ScreensaverSeconds != 900 {
		t.Errorf("ScreensaverSeconds = %d, want 900", v.ScreensaverSeconds)
	}
}
