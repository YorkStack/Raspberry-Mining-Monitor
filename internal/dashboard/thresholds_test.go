package dashboard

import (
	"testing"
	"time"
)

// Thresholds are per miner: a six-phase NerdOctaxe and a single-ASIC Gamma do
// not share a sensible warning band.
func TestPerMinerThresholdsOverrideTheDefault(t *testing.T) {
	in := reference()
	in.Thresholds = Thresholds{ASICWarnC: 64, ASICCritC: 70, VRMWarnC: 80, VRMCritC: 90}
	in.MinerThresholds = map[string]Thresholds{
		"Gamma 602": {ASICWarnC: 50, ASICCritC: 55, VRMWarnC: 80, VRMCritC: 90},
	}
	in.Miners[0].ASICTempC = f(62) // NerdOctaxe, under the default warn of 64
	in.Miners[1].ASICTempC = f(52) // Gamma, over its own warn of 50

	v := Build(in, now)
	if v.Miners[0].ASICTempStatus != "ok" {
		t.Errorf("NerdOctaxe at 62 with warn 64 = %q, want ok", v.Miners[0].ASICTempStatus)
	}
	if v.Miners[1].ASICTempStatus != "warn" {
		t.Errorf("Gamma at 52 with its own warn of 50 = %q, want warn", v.Miners[1].ASICTempStatus)
	}
}

func TestMinerWithoutAnOverrideUsesTheDefault(t *testing.T) {
	in := reference()
	in.Thresholds = Thresholds{ASICWarnC: 64, ASICCritC: 70, VRMWarnC: 80, VRMCritC: 90}
	in.MinerThresholds = map[string]Thresholds{"Somebody Else": {ASICWarnC: 10, ASICCritC: 20}}
	in.Miners[0].ASICTempC = f(66)

	if v := Build(in, now); v.Miners[0].ASICTempStatus != "warn" {
		t.Errorf("status = %q, want warn from the default band", v.Miners[0].ASICTempStatus)
	}
}

// The band the firmware itself defines: both AxeOS variants trigger thermal
// protection at 70 C, so red must not sit above that.
func TestFirmwareBandKeepsSixtyTwoGreenAndSeventyRed(t *testing.T) {
	in := reference()
	in.Thresholds = Thresholds{ASICWarnC: 64, ASICCritC: 70, VRMWarnC: 80, VRMCritC: 90}
	in.MinerThresholds = nil

	cases := []struct {
		temp float64
		want string
	}{
		{55, "ok"}, {62, "ok"}, {63, "ok"}, {63.9, "ok"},
		{64, "warn"}, {69.9, "warn"},
		{70, "crit"}, {75, "crit"},
	}
	for _, c := range cases {
		in.Miners[0].ASICTempC = f(c.temp)
		if got := Build(in, now).Miners[0].ASICTempStatus; got != c.want {
			t.Errorf("%.1f C gave %q, want %q", c.temp, got, c.want)
		}
	}
}

func TestZeroThresholdsNeverReportAFalseAlarm(t *testing.T) {
	in := reference()
	in.Thresholds = Thresholds{}
	in.MinerThresholds = nil
	in.Miners[0].ASICTempC = f(45)

	if got := Build(in, now).Miners[0].ASICTempStatus; got != "ok" {
		t.Errorf("status = %q with unset thresholds, want ok rather than a false crit", got)
	}
}

var _ = time.Second
