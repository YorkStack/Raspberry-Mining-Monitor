// Package alert raises operator notifications for miner conditions and delivers
// them to an opt-in webhook. It is disabled unless a webhook URL is configured.
//
// The engine is edge-triggered with a cooldown: an alert fires when a condition
// becomes true, then not again until the cooldown passes while it stays true.
// When the condition clears, its state resets so a later recurrence fires at
// once. This keeps a flapping miner from flooding the webhook without going
// silent on a genuinely new problem.
package alert

import (
	"context"
	"fmt"
	"time"
)

// Kind identifies an alert condition.
type Kind string

const (
	KindOffline Kind = "offline"
	KindTemp    Kind = "temp"
)

// Alert is one notification.
type Alert struct {
	Miner   string  `json:"miner"`
	Kind    Kind    `json:"kind"`
	Level   string  `json:"level"`
	Message string  `json:"message"`
	Value   float64 `json:"value"`
}

// MinerStatus is the per-miner input the engine evaluates.
type MinerStatus struct {
	Name       string
	Online     bool
	OfflineFor time.Duration // time since the last successful poll, when offline
	ASICTempC  *float64      // nil when the miner does not report it
	CritTempC  float64       // the miner's critical threshold, 0 to skip
}

// Config controls which conditions raise alerts and how often they repeat.
type Config struct {
	// OfflineAfter is how long a miner may be offline before alerting. 0 disables
	// offline alerts.
	OfflineAfter time.Duration
	// TempAlerts enables ASIC-over-critical alerts.
	TempAlerts bool
	// Cooldown is the minimum gap between repeats of the same alert.
	Cooldown time.Duration
}

// Engine holds the alert configuration and the per-condition firing state.
type Engine struct {
	cfg       Config
	lastFired map[string]time.Time
}

// New creates an engine.
func New(cfg Config) *Engine {
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = 30 * time.Minute
	}
	return &Engine{cfg: cfg, lastFired: map[string]time.Time{}}
}

// Evaluate returns the alerts to send right now for the given miner states,
// honouring the cooldown and resetting conditions that have cleared.
func (e *Engine) Evaluate(now time.Time, miners []MinerStatus) []Alert {
	active := map[string]bool{}
	var out []Alert

	consider := func(a Alert, key string) {
		active[key] = true
		if t, ok := e.lastFired[key]; ok && now.Sub(t) < e.cfg.Cooldown {
			return
		}
		e.lastFired[key] = now
		out = append(out, a)
	}

	for _, m := range miners {
		if e.cfg.OfflineAfter > 0 && !m.Online && m.OfflineFor >= e.cfg.OfflineAfter {
			mins := int(m.OfflineFor.Minutes())
			consider(Alert{
				Miner: m.Name, Kind: KindOffline, Level: "critical", Value: float64(mins),
				Message: fmt.Sprintf("%s has been offline for %d min", m.Name, mins),
			}, m.Name+"/offline")
		}
		if e.cfg.TempAlerts && m.ASICTempC != nil && m.CritTempC > 0 && *m.ASICTempC >= m.CritTempC {
			consider(Alert{
				Miner: m.Name, Kind: KindTemp, Level: "critical", Value: *m.ASICTempC,
				Message: fmt.Sprintf("%s ASIC temperature %.0f°C at or above critical %.0f°C", m.Name, *m.ASICTempC, m.CritTempC),
			}, m.Name+"/temp")
		}
	}

	// A condition that is no longer active resets, so its next occurrence fires
	// immediately rather than waiting out a stale cooldown.
	for key := range e.lastFired {
		if !active[key] {
			delete(e.lastFired, key)
		}
	}
	return out
}

// Notifier delivers an alert.
type Notifier interface {
	Notify(ctx context.Context, a Alert) error
}
