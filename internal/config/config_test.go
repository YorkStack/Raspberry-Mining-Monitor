package config

import (
	"strings"
	"testing"
	"time"
)

const minimal = `
miners:
  - name: NerdOctaxe
    host: 192.168.1.51
    payout_address: bc1qexampleexampleexampleexampleexampleex
  - name: Gamma 602
    host: 192.168.1.52
    payout_address: bc1qanotheranotheranotheranotheranotherxx
`

func TestParseMinimalConfig(t *testing.T) {
	c, err := Parse([]byte(minimal))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(c.Miners) != 2 {
		t.Fatalf("got %d miners, want 2", len(c.Miners))
	}
	if c.Miners[0].Name != "NerdOctaxe" || c.Miners[0].Host != "192.168.1.51" {
		t.Errorf("first miner = %+v", c.Miners[0])
	}
}

func TestDefaultsAreApplied(t *testing.T) {
	c, err := Parse([]byte(minimal))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if c.Dashboard.Port != 8080 {
		t.Errorf("Port = %d, want 8080", c.Dashboard.Port)
	}
	if c.Dashboard.Bind != "0.0.0.0" {
		t.Errorf("Bind = %q, want 0.0.0.0", c.Dashboard.Bind)
	}
	if c.Miners[0].Type != "axeos" {
		t.Errorf("Type = %q, want axeos", c.Miners[0].Type)
	}
	if c.Miners[0].Interval != 2*time.Second {
		t.Errorf("miner Interval = %v, want 2s", c.Miners[0].Interval)
	}
	if c.Pool.Interval != 60*time.Second {
		t.Errorf("pool Interval = %v, want 60s", c.Pool.Interval)
	}
	if c.Bitcoin.Interval != 30*time.Second {
		t.Errorf("bitcoin Interval = %v, want 30s", c.Bitcoin.Interval)
	}
	if c.Bitcoin.BaseURL != "https://mempool.space" {
		t.Errorf("bitcoin BaseURL = %q", c.Bitcoin.BaseURL)
	}
	if c.Dashboard.ScreensaverMinutes != 15 {
		t.Errorf("ScreensaverMinutes = %d, want 15", c.Dashboard.ScreensaverMinutes)
	}
	if c.Pool.BaseURL != "https://public-pool.io:40557" {
		t.Errorf("pool BaseURL = %q", c.Pool.BaseURL)
	}
	// Anchored to the firmware: both AxeOS variants trigger thermal protection
	// at 70 C, so red sits exactly there and amber gives 6 C of warning first.
	// A NerdOctaxe idling at 62 to 63 C stays green.
	if c.Miners[0].WarnTempC != 64 || c.Miners[0].CritTempC != 70 {
		t.Errorf("temp thresholds = %v/%v, want 64/70", c.Miners[0].WarnTempC, c.Miners[0].CritTempC)
	}
}

func TestScreensaverCanBeDisabledWithZero(t *testing.T) {
	c, err := Parse([]byte(minimal + "dashboard:\n  screensaver_minutes: 0\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Dashboard.ScreensaverMinutes != 0 {
		t.Errorf("ScreensaverMinutes = %d, want 0 (explicit disable must survive)", c.Dashboard.ScreensaverMinutes)
	}
}

func TestExplicitValuesOverrideDefaults(t *testing.T) {
	src := minimal + `
dashboard:
  port: 9090
  bind: 127.0.0.1
pool:
  interval: 90s
bitcoin:
  base_url: https://mempool.emzy.de
`
	c, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Dashboard.Port != 9090 {
		t.Errorf("Port = %d, want 9090", c.Dashboard.Port)
	}
	if c.Dashboard.Bind != "127.0.0.1" {
		t.Errorf("Bind = %q", c.Dashboard.Bind)
	}
	if c.Pool.Interval != 90*time.Second {
		t.Errorf("pool Interval = %v, want 90s", c.Pool.Interval)
	}
	if c.Bitcoin.BaseURL != "https://mempool.emzy.de" {
		t.Errorf("BaseURL = %q", c.Bitcoin.BaseURL)
	}
}

func TestRejectsMinerWithoutName(t *testing.T) {
	_, err := Parse([]byte("miners:\n  - host: 192.168.1.51\n"))
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("err = %v, want a complaint about the missing name", err)
	}
}

func TestRejectsMinerWithoutHost(t *testing.T) {
	_, err := Parse([]byte("miners:\n  - name: NerdOctaxe\n"))
	if err == nil || !strings.Contains(err.Error(), "host") {
		t.Errorf("err = %v, want a complaint about the missing host", err)
	}
}

func TestRejectsDuplicateMinerNames(t *testing.T) {
	src := `
miners:
  - name: Axe
    host: 192.168.1.51
  - name: Axe
    host: 192.168.1.52
`
	_, err := Parse([]byte(src))
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("err = %v, want a complaint about duplicate names", err)
	}
}

func TestRejectsUnknownMinerType(t *testing.T) {
	src := "miners:\n  - name: Axe\n    host: 1.2.3.4\n    type: antminer\n"
	_, err := Parse([]byte(src))
	if err == nil || !strings.Contains(err.Error(), "antminer") {
		t.Errorf("err = %v, want a complaint about the unknown type", err)
	}
}

func TestRejectsUnknownPoolProvider(t *testing.T) {
	src := minimal + "pool:\n  provider: slushpool\n"
	_, err := Parse([]byte(src))
	if err == nil || !strings.Contains(err.Error(), "slushpool") {
		t.Errorf("err = %v, want a complaint about the unknown pool provider", err)
	}
}

func TestRejectsInvalidPort(t *testing.T) {
	for _, port := range []int{0, -1, 70000} {
		src := minimal + "dashboard:\n  port: " + itoa(port) + "\n"
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("port %d was accepted, want an error", port)
		}
	}
}

func TestRejectsIntervalBelowOneSecond(t *testing.T) {
	src := `
miners:
  - name: Axe
    host: 1.2.3.4
    interval: 100ms
`
	_, err := Parse([]byte(src))
	if err == nil || !strings.Contains(err.Error(), "interval") {
		t.Errorf("err = %v, want a complaint about the interval", err)
	}
}

func TestRejectsMalformedYAML(t *testing.T) {
	if _, err := Parse([]byte("miners: [oh dear\n")); err == nil {
		t.Error("malformed YAML was accepted")
	}
}

// Demo mode has no physical miners, so an empty list is valid there and only
// there.
func TestEmptyMinerListIsRejectedOutsideDemoMode(t *testing.T) {
	_, err := Parse([]byte("dashboard:\n  port: 8080\n"))
	if err == nil || !strings.Contains(err.Error(), "at least one miner") {
		t.Errorf("err = %v, want a complaint about having no miners", err)
	}
}

func TestDemoConfigNeedsNoMiners(t *testing.T) {
	c := Demo()
	if err := c.Validate(); err != nil {
		t.Fatalf("the demo config should validate: %v", err)
	}
	if len(c.Miners) == 0 {
		t.Error("the demo config should still describe simulated miners")
	}
	if !c.Demo {
		t.Error("Demo = false on the demo config")
	}
}

// The config file is the only place an IP may appear.
func TestConfigCarriesNoBuiltInMinerAddresses(t *testing.T) {
	c := Demo()
	for _, m := range c.Miners {
		if m.Host != "" {
			t.Errorf("demo miner %q has host %q; demo mode must not imply a real address", m.Name, m.Host)
		}
	}
}

func itoa(v int) string {
	if v < 0 {
		return "-" + itoa(-v)
	}
	if v < 10 {
		return string(rune('0' + v))
	}
	return itoa(v/10) + string(rune('0'+v%10))
}
