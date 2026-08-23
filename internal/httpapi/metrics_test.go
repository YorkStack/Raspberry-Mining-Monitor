package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/dashboard"
)

func metricsHandler(enabled bool) http.Handler {
	return NewHandler(Options{
		Store: testStore(),
		Now:   func() time.Time { return now },
		Intervals: dashboard.Input{
			MinerInterval: 2 * time.Second, PoolInterval: 60 * time.Second, NetworkInterval: 30 * time.Second,
			Thresholds: dashboard.Thresholds{ASICWarnC: 70, ASICCritC: 80, VRMWarnC: 80, VRMCritC: 90},
		},
		Static:         fstest.MapFS{"index.html": {Data: []byte("x")}},
		Version:        "test",
		MetricsEnabled: enabled,
	})
}

func TestMetricsExposesFleetAndMiners(t *testing.T) {
	rec := httptest.NewRecorder()
	metricsHandler(true).ServeHTTP(rec, local(httptest.NewRequest(http.MethodGet, "/metrics", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`rmm_build_info{version="test"} 1`,
		"rmm_fleet_hashrate_ths 12.1",
		`rmm_miner_hashrate_ths{miner="NerdOctaxe"} 12.1`,
		"rmm_network_difficulty ",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q\n---\n%s", want, body)
		}
	}
}

func TestMetricsGatedToLocalNetwork(t *testing.T) {
	rec := httptest.NewRecorder()
	metricsHandler(true).ServeHTTP(rec, remote(httptest.NewRequest(http.MethodGet, "/metrics", nil)))
	if rec.Code != http.StatusNotFound {
		t.Errorf("public status = %d, want 404 (hidden from the LAN)", rec.Code)
	}
}

func TestMetricsCanBeDisabled(t *testing.T) {
	rec := httptest.NewRecorder()
	metricsHandler(false).ServeHTTP(rec, local(httptest.NewRequest(http.MethodGet, "/metrics", nil)))
	if rec.Code != http.StatusNotFound {
		t.Errorf("disabled status = %d, want 404", rec.Code)
	}
}

func TestMetricsRejectsNonGet(t *testing.T) {
	rec := httptest.NewRecorder()
	metricsHandler(true).ServeHTTP(rec, local(httptest.NewRequest(http.MethodPost, "/metrics", nil)))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", rec.Code)
	}
}

func TestMetricsLabelEscaping(t *testing.T) {
	if got := escapeLabel(`a"b\c`); got != `a\"b\\c` {
		t.Errorf("escapeLabel = %q", got)
	}
}
