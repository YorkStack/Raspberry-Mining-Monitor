package alert

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var t0 = time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

func f(v float64) *float64 { return &v }

func TestOfflineAlertFiresOnlyPastThreshold(t *testing.T) {
	e := New(Config{OfflineAfter: 10 * time.Minute, Cooldown: time.Hour})

	// 5 minutes offline: below threshold, nothing.
	if got := e.Evaluate(t0, []MinerStatus{{Name: "A", Online: false, OfflineFor: 5 * time.Minute}}); len(got) != 0 {
		t.Fatalf("early offline fired: %+v", got)
	}
	// 12 minutes offline: fires.
	got := e.Evaluate(t0.Add(time.Minute), []MinerStatus{{Name: "A", Online: false, OfflineFor: 12 * time.Minute}})
	if len(got) != 1 || got[0].Kind != KindOffline {
		t.Fatalf("offline did not fire: %+v", got)
	}
}

func TestTempAlertFiresAtOrAboveCritical(t *testing.T) {
	e := New(Config{TempAlerts: true, Cooldown: time.Hour})
	got := e.Evaluate(t0, []MinerStatus{{Name: "A", Online: true, ASICTempC: f(72), CritTempC: 70}})
	if len(got) != 1 || got[0].Kind != KindTemp {
		t.Fatalf("temp did not fire: %+v", got)
	}
	// Below critical: nothing.
	if got := e.Evaluate(t0.Add(time.Hour+time.Minute), []MinerStatus{{Name: "A", Online: true, ASICTempC: f(65), CritTempC: 70}}); len(got) != 0 {
		t.Fatalf("temp fired below critical: %+v", got)
	}
}

func TestCooldownSuppressesRepeatThenReleases(t *testing.T) {
	e := New(Config{TempAlerts: true, Cooldown: 30 * time.Minute})
	hot := []MinerStatus{{Name: "A", Online: true, ASICTempC: f(75), CritTempC: 70}}

	if len(e.Evaluate(t0, hot)) != 1 {
		t.Fatal("first alert should fire")
	}
	if len(e.Evaluate(t0.Add(10*time.Minute), hot)) != 0 {
		t.Fatal("repeat within cooldown should be suppressed")
	}
	if len(e.Evaluate(t0.Add(31*time.Minute), hot)) != 1 {
		t.Fatal("alert should fire again after the cooldown")
	}
}

func TestClearingResetsSoRecurrenceFiresImmediately(t *testing.T) {
	e := New(Config{TempAlerts: true, Cooldown: time.Hour})
	hot := []MinerStatus{{Name: "A", Online: true, ASICTempC: f(75), CritTempC: 70}}
	cool := []MinerStatus{{Name: "A", Online: true, ASICTempC: f(60), CritTempC: 70}}

	if len(e.Evaluate(t0, hot)) != 1 {
		t.Fatal("first alert should fire")
	}
	// Cools down (condition clears), then heats again 5 min later — well inside
	// the 1h cooldown, but a cleared condition must fire immediately.
	e.Evaluate(t0.Add(2*time.Minute), cool)
	if len(e.Evaluate(t0.Add(5*time.Minute), hot)) != 1 {
		t.Fatal("recurrence after clearing should fire despite the cooldown")
	}
}

func TestDisabledConditionsNeverFire(t *testing.T) {
	e := New(Config{OfflineAfter: 0, TempAlerts: false, Cooldown: time.Hour})
	got := e.Evaluate(t0, []MinerStatus{{Name: "A", Online: false, OfflineFor: time.Hour, ASICTempC: f(99), CritTempC: 70}})
	if len(got) != 0 {
		t.Fatalf("disabled conditions fired: %+v", got)
	}
}

func TestWebhookPostsJSON(t *testing.T) {
	var got payload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
	}))
	defer srv.Close()

	err := NewWebhook(srv.URL, time.Second).Notify(context.Background(),
		Alert{Miner: "A", Kind: KindTemp, Level: "critical", Message: "hot", Value: 75})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if got.Miner != "A" || got.Kind != "temp" || got.Message != "hot" || got.Source == "" {
		t.Errorf("payload = %+v", got)
	}
}
