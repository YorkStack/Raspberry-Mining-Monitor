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
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/minercfg"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/settings"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/state"
)

func minersHandler(t *testing.T) (http.Handler, *minercfg.Store, *int) {
	t.Helper()
	dir := t.TempDir()
	mc := minercfg.New(filepath.Join(dir, "miners.json"), minercfg.Providers{
		BitcoinBaseURL: "https://mempool.space", PoolBaseURL: "https://public-pool.io:40557",
	})
	_ = mc.Replace([]minercfg.Spec{{Name: "NerdOctaxe", Type: "axeos", Host: "192.168.1.51", PayoutAddress: "bc1qnerd"}})

	set := settings.New(filepath.Join(dir, "thresholds.json"),
		settings.Thresholds{ASICWarnC: 64, ASICCritC: 70, VRMWarnC: 80, VRMCritC: 90})

	reloads := 0
	h := NewHandler(Options{
		Store:           state.New([]string{"NerdOctaxe"}),
		Now:             func() time.Time { return now },
		Intervals:       dashboard.Input{MinerInterval: 2 * time.Second, PoolInterval: 60 * time.Second, NetworkInterval: 30 * time.Second},
		Static:          fstest.MapFS{"index.html": {Data: []byte("x")}},
		Version:         "test",
		Settings:        set,
		SettingsEnabled: true,
		MinerCfg:        mc,
		OnConfigChange:  func() { reloads++ },
	})
	return h, mc, &reloads
}

func TestGetMinersReturnsSpecsAndProviders(t *testing.T) {
	h, _, _ := minersHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodGet, "/api/v1/miners", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Miners []struct {
			Name            string `json:"name"`
			Host            string `json:"host"`
			IntervalSeconds int    `json:"intervalSeconds"`
		} `json:"miners"`
		Providers minercfg.Providers `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Miners) != 1 || body.Miners[0].Host != "192.168.1.51" {
		t.Errorf("miners = %+v", body.Miners)
	}
	if body.Providers.BitcoinBaseURL != "https://mempool.space" {
		t.Errorf("providers = %+v", body.Providers)
	}
}

func TestPutMinersReplacesAndReloads(t *testing.T) {
	h, mc, reloads := minersHandler(t)
	body := `{"miners":[
		{"name":"NerdOctaxe","type":"axeos","host":"10.0.0.5","payoutAddress":"bc1qa","intervalSeconds":3},
		{"name":"Gamma 602","type":"axeos","host":"10.0.0.6","payoutAddress":"bc1qb","intervalSeconds":2}
	]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodPut, "/api/v1/miners", strings.NewReader(body))))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if len(mc.Miners()) != 2 {
		t.Errorf("store has %d miners, want 2", len(mc.Miners()))
	}
	if mc.Miners()[0].Host != "10.0.0.5" || mc.Miners()[0].Interval != 3*time.Second {
		t.Errorf("first miner = %+v", mc.Miners()[0])
	}
	if *reloads != 1 {
		t.Errorf("reloads = %d, want 1", *reloads)
	}
}

func TestPutMinersRejectsInvalid(t *testing.T) {
	h, _, reloads := minersHandler(t)
	body := `{"miners":[{"name":"NoHost","type":"axeos"}]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodPut, "/api/v1/miners", strings.NewReader(body))))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if *reloads != 0 {
		t.Error("invalid config must not trigger a reload")
	}
}

func TestPutProvidersReloads(t *testing.T) {
	h, mc, reloads := minersHandler(t)
	body := `{"bitcoinBaseUrl":"https://mempool.emzy.de","poolBaseUrl":"https://public-pool.io:40557"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodPut, "/api/v1/providers", strings.NewReader(body))))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if mc.Providers().BitcoinBaseURL != "https://mempool.emzy.de" {
		t.Errorf("provider not stored: %+v", mc.Providers())
	}
	if *reloads != 1 {
		t.Errorf("reloads = %d, want 1", *reloads)
	}
}

func TestMinersApiIsTrustedOnly(t *testing.T) {
	h, _, _ := minersHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, remote(httptest.NewRequest(http.MethodGet, "/api/v1/miners", nil)))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 from a public IP", rec.Code)
	}
}

// The per-miner monitoring token must never reach the browser, and editing a
// miner through the admin UI (which never receives the token) must not wipe it.
func TestMinerTokenNeverExposedButPreservedOnPut(t *testing.T) {
	h, mc, _ := minersHandler(t)
	const token = "supersecrettoken1234567890ABCD"
	if err := mc.Replace([]minercfg.Spec{{
		Name: "Mac M2", Type: "axeos", Host: "192.168.1.60",
		PayoutAddress: "bc1qmac", Token: token,
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodGet, "/api/v1/miners", nil)))
	if strings.Contains(rec.Body.String(), token) || strings.Contains(rec.Body.String(), "token") {
		t.Fatalf("GET /api/v1/miners leaked token material: %s", rec.Body.String())
	}

	body := `{"miners":[{"name":"Mac M2","type":"axeos","host":"192.168.1.99","payoutAddress":"bc1qmac","intervalSeconds":5}]}`
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, local(httptest.NewRequest(http.MethodPut, "/api/v1/miners", strings.NewReader(body))))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", rec.Code, rec.Body.String())
	}
	got := mc.Miners()
	if len(got) != 1 {
		t.Fatalf("miners = %d, want 1", len(got))
	}
	if got[0].Host != "192.168.1.99" {
		t.Errorf("host = %q, want 192.168.1.99", got[0].Host)
	}
	if got[0].Token != token {
		t.Errorf("token not preserved on PUT: got %q", got[0].Token)
	}
}
