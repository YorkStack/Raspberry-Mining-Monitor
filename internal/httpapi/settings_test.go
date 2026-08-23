package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/dashboard"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/settings"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/state"
)

func bandDefaults() settings.Thresholds {
	return settings.Thresholds{ASICWarnC: 64, ASICCritC: 70, VRMWarnC: 80, VRMCritC: 90}
}

func adminHandler(t *testing.T, s *state.Store, enabled bool) (http.Handler, *settings.Store) {
	t.Helper()
	set := settings.New(filepath.Join(t.TempDir(), "thresholds.json"), bandDefaults())

	h := NewHandler(Options{
		Store: s,
		Now:   func() time.Time { return now },
		Intervals: dashboard.Input{
			MinerInterval:   2 * time.Second,
			PoolInterval:    60 * time.Second,
			NetworkInterval: 30 * time.Second,
		},
		Static: fstest.MapFS{
			"index.html":    {Data: []byte("<h1>dashboard</h1>")},
			"settings.html": {Data: []byte("<h1>thresholds</h1>")},
		},
		Version:         "test",
		Settings:        set,
		SettingsEnabled: enabled,
		MinerNames:      []string{"NerdOctaxe", "Gamma 602"},
	})
	return h, set
}

// httptest requests default to a non-loopback RemoteAddr, so both sides of the
// gate are reachable from a test.
func local(r *http.Request) *http.Request  { r.RemoteAddr = "127.0.0.1:54321"; return r }
func remote(r *http.Request) *http.Request { r.RemoteAddr = "203.0.113.7:54321"; return r } // public, untrusted

func TestSettingsApiIsServedToLoopback(t *testing.T) {
	h, _ := adminHandler(t, testStore(), true)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Default   settings.Thresholds            `json:"default"`
		Miners    []string                       `json:"miners"`
		Overrides map[string]settings.Thresholds `json:"overrides"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Default != bandDefaults() {
		t.Errorf("Default = %+v, want %+v", body.Default, bandDefaults())
	}
	if len(body.Miners) != 2 {
		t.Errorf("Miners = %v, want the two configured names", body.Miners)
	}
}

// The LAN dashboard stays strictly read-only. The settings surface is not
// merely refused there, it is invisible.
func TestSettingsApiIsHiddenFromTheLan(t *testing.T) {
	h, _ := adminHandler(t, testStore(), true)

	for _, path := range []string{"/api/v1/settings", "/settings"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, remote(httptest.NewRequest(http.MethodGet, path, nil)))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s from the LAN gave %d, want 404", path, rec.Code)
		}
	}
}

// X-Forwarded-For is deliberately not honoured: trusting it would let any LAN
// client claim to be local.
func TestForwardedHeaderCannotForgeLoopback(t *testing.T) {
	h, _ := adminHandler(t, testStore(), true)

	req := remote(httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil))
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("X-Real-IP", "127.0.0.1")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; a forwarded header must not grant access", rec.Code)
	}
}

func TestSettingsCanBeDisabledEntirely(t *testing.T) {
	h, _ := adminHandler(t, testStore(), false)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when the settings surface is switched off", rec.Code)
	}
}

func TestPutStoresAndPersistsAThreshold(t *testing.T) {
	h, set := adminHandler(t, testStore(), true)

	body := `{"asicWarnC":50,"asicCritC":58,"vrmWarnC":75,"vrmCritC":85}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodPut, "/api/v1/settings/Gamma%20602", strings.NewReader(body))))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}

	want := settings.Thresholds{ASICWarnC: 50, ASICCritC: 58, VRMWarnC: 75, VRMCritC: 85}
	if got := set.For("Gamma 602"); got != want {
		t.Errorf("For = %+v, want %+v", got, want)
	}

	// It must survive a restart, so it has to be on disk already.
	reloaded := settings.New(set.Path(), bandDefaults())
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := reloaded.For("Gamma 602"); got != want {
		t.Errorf("after reload For = %+v, want %+v", got, want)
	}
}

func TestPutRejectsAnInvalidBandAndChangesNothing(t *testing.T) {
	h, set := adminHandler(t, testStore(), true)

	body := `{"asicWarnC":70,"asicCritC":60,"vrmWarnC":80,"vrmCritC":90}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodPut, "/api/v1/settings/Gamma%20602", strings.NewReader(body))))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if got := set.For("Gamma 602"); got != bandDefaults() {
		t.Errorf("For = %+v, want the untouched defaults", got)
	}
}

func TestPutRejectsAnUnknownMiner(t *testing.T) {
	h, _ := adminHandler(t, testStore(), true)

	body := `{"asicWarnC":50,"asicCritC":58,"vrmWarnC":75,"vrmCritC":85}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodPut, "/api/v1/settings/Ghost", strings.NewReader(body))))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a miner that is not configured", rec.Code)
	}
}

func TestDeleteResetsToTheDefault(t *testing.T) {
	h, set := adminHandler(t, testStore(), true)
	_ = set.Set("Gamma 602", settings.Thresholds{ASICWarnC: 50, ASICCritC: 58, VRMWarnC: 75, VRMCritC: 85})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodDelete, "/api/v1/settings/Gamma%20602", nil)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := set.For("Gamma 602"); got != bandDefaults() {
		t.Errorf("For = %+v, want the defaults back", got)
	}
}

func TestSettingsPageIsServedToLoopback(t *testing.T) {
	h, _ := adminHandler(t, testStore(), true)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodGet, "/settings", nil)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "thresholds") {
		t.Errorf("body = %q, want the settings page", rec.Body.String())
	}
}

// Health is an operator surface, so it follows the same loopback rule.
func TestHealthIsLoopbackOnly(t *testing.T) {
	h, _ := adminHandler(t, testStore(), true)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, remote(httptest.NewRequest(http.MethodGet, "/healthz", nil)))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 from the LAN", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodGet, "/healthz", nil)))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 from loopback", rec.Code)
	}
}

// Changing a display threshold must never reach the hardware.
func TestSettingsApiRefusesAnythingButGetPutDelete(t *testing.T) {
	h, _ := adminHandler(t, testStore(), true)

	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, local(httptest.NewRequest(method, "/api/v1/settings/Gamma%20602", strings.NewReader("{}"))))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s gave %d, want 405", method, rec.Code)
		}
	}
}

// A threshold changed through the settings API has to show up in the very next
// snapshot, otherwise the operator adjusts a number and nothing happens.
func TestChangedThresholdIsVisibleInTheNextSnapshot(t *testing.T) {
	store := testStore()
	h, _ := adminHandler(t, store, true)

	hot := miner.Snapshot{Name: "NerdOctaxe", HashrateTHs: 12.10, ASICTempC: ptr(62)}
	hot.Succeed(now)
	store.SetMiner("NerdOctaxe", hot)

	// 62 C sits below the default warn of 64.
	if got := statusOf(t, h, "NerdOctaxe"); got != "ok" {
		t.Fatalf("status = %q before the change, want ok", got)
	}

	body := `{"asicWarnC":58,"asicCritC":70,"vrmWarnC":80,"vrmCritC":90}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodPut, "/api/v1/settings/NerdOctaxe", strings.NewReader(body))))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", rec.Code, rec.Body.String())
	}

	if got := statusOf(t, h, "NerdOctaxe"); got != "warn" {
		t.Errorf("status = %q after lowering warn to 58, want warn", got)
	}
}

func ptr(v float64) *float64 { return &v }

func statusOf(t *testing.T, h http.Handler, minerName string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/snapshot", nil))

	var v dashboard.View
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	for _, m := range v.Miners {
		if m.Name == minerName {
			return m.ASICTempStatus
		}
	}
	t.Fatalf("miner %q not in snapshot", minerName)
	return ""
}

func TestSettingsGetReportsEnabledState(t *testing.T) {
	h, set := adminHandler(t, testStore(), true)
	set.SetEnabled("Gamma 602", false)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Disabled map[string]bool `json:"disabled"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !body.Disabled["Gamma 602"] {
		t.Errorf("Disabled = %v, want Gamma 602 true", body.Disabled)
	}
}

func TestToggleMinerMonitoring(t *testing.T) {
	h, set := adminHandler(t, testStore(), true)

	body := `{"enabled":false}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodPut, "/api/v1/settings/Gamma%20602/enabled", strings.NewReader(body))))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if set.Enabled("Gamma 602") {
		t.Error("Gamma should be disabled after the toggle")
	}

	// It must persist.
	reloaded := settings.New(set.Path(), bandDefaults())
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Enabled("Gamma 602") {
		t.Error("disabled state did not persist")
	}
}

func TestToggleRejectsUnknownMiner(t *testing.T) {
	h, _ := adminHandler(t, testStore(), true)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodPut, "/api/v1/settings/Ghost/enabled", strings.NewReader(`{"enabled":false}`))))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestToggleIsLoopbackOnly(t *testing.T) {
	h, _ := adminHandler(t, testStore(), true)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, remote(httptest.NewRequest(http.MethodPut, "/api/v1/settings/Gamma%20602/enabled", strings.NewReader(`{"enabled":false}`))))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 from the LAN", rec.Code)
	}
}

func TestDisabledMinerVanishesFromSnapshot(t *testing.T) {
	store := testStore()
	// Add Gamma so there are two miners.
	g := miner.Snapshot{Name: "Gamma 602", HashrateTHs: 1.27}
	g.Succeed(now)
	store.SetMiner("Gamma 602", g)

	h, set := adminHandler(t, store, true)
	set.SetEnabled("Gamma 602", false)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/snapshot", nil))
	var v dashboard.View
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	for _, m := range v.Miners {
		if m.Name == "Gamma 602" {
			t.Error("disabled Gamma 602 should not appear in the snapshot")
		}
	}
}
