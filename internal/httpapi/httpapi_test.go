package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/bitcoin"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/dashboard"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/state"
)

var now = time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

const payoutAddress = "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq"

func testStore() *state.Store {
	s := state.New([]string{"NerdOctaxe", "Gamma 602"})

	m := miner.Snapshot{Name: "NerdOctaxe", HashrateTHs: 12.10, PoolUser: payoutAddress}
	m.Succeed(now)
	s.SetMiner("NerdOctaxe", m)

	n := bitcoin.Snapshot{Kind: bitcoin.SourcePublic, Height: 963692, Difficulty: 125_807_076_547_197.5}
	n.Succeed(now)
	s.SetNetwork(n)

	return s
}

func testHandler(s *state.Store) http.Handler {
	return NewHandler(Options{
		Store: s,
		Now:   func() time.Time { return now },
		Intervals: dashboard.Input{
			MinerInterval:   2 * time.Second,
			PoolInterval:    60 * time.Second,
			NetworkInterval: 30 * time.Second,
			Thresholds:      dashboard.Thresholds{ASICWarnC: 70, ASICCritC: 80, VRMWarnC: 80, VRMCritC: 90},
		},
		Static:  fstest.MapFS{"index.html": {Data: []byte("<h1>dashboard</h1>")}},
		Version: "test",
	})
}

func TestSnapshotEndpointReturnsTheDashboardDocument(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler(testStore()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/snapshot", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var v dashboard.View
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(v.Miners) != 2 {
		t.Errorf("got %d miners, want 2", len(v.Miners))
	}
	if v.Network.Height != 963692 {
		t.Errorf("Height = %d, want 963692", v.Network.Height)
	}
}

func TestSnapshotNeverLeaksThePayoutAddress(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler(testStore()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/snapshot", nil))

	if strings.Contains(rec.Body.String(), payoutAddress) {
		t.Fatalf("response leaks the full payout address:\n%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "bc1q…5mdq") {
		t.Error("response is missing the masked address")
	}
}

func TestSecurityHeadersArePresent(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler(testStore()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "frame-ancestors 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q is missing %q", csp, want)
		}
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", got)
	}
}

func TestIndexIsServedAtRoot(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler(testStore()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "dashboard") {
		t.Errorf("body = %q, want the index page", rec.Body.String())
	}
}

// A deployed frontend must not be served stale from the kiosk's cache, so the
// static assets carry a no-cache header.
func TestStaticAssetsAreNotCached(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler(testStore()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
}

func TestUnknownPathIsNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler(testStore()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope.js", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestWriteMethodsAreRejected(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		rec := httptest.NewRecorder()
		testHandler(testStore()).ServeHTTP(rec, httptest.NewRequest(method, "/api/v1/snapshot", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s gave status %d, want 405", method, rec.Code)
		}
	}
}

func TestHealthReportsPerSourceFreshness(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	testHandler(testStore()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var h struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Sources []struct {
			Name  string `json:"name"`
			OK    bool   `json:"ok"`
			Stale bool   `json:"stale"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &h); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if h.Version != "test" {
		t.Errorf("Version = %q, want test", h.Version)
	}
	// Two miners, one pool, one network.
	if len(h.Sources) != 4 {
		t.Fatalf("got %d sources, want 4", len(h.Sources))
	}

	byName := map[string]bool{}
	for _, s := range h.Sources {
		byName[s.Name] = s.OK
	}
	if !byName["miner:NerdOctaxe"] {
		t.Error("NerdOctaxe should be reported healthy")
	}
	if byName["miner:Gamma 602"] {
		t.Error("Gamma 602 never answered and must not be reported healthy")
	}
}

// The dashboard must keep serving while sources are down, so a degraded
// system still answers 200 rather than failing the healthcheck and getting
// restarted in a loop.
func TestHealthIsDegradedButStillTwoHundredWhenASourceIsDown(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	testHandler(testStore()).ServeHTTP(rec, req)

	var h struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &h)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 even when degraded", rec.Code)
	}
	if h.Status != "degraded" {
		t.Errorf("status field = %q, want degraded", h.Status)
	}
}

func TestStreamSendsAnInitialSnapshotImmediately(t *testing.T) {
	srv := httptest.NewServer(testHandler(testStore()))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	r := bufio.NewReader(resp.Body)

	eventLine, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read event line: %v", err)
	}
	if strings.TrimSpace(eventLine) != "event: snapshot" {
		t.Errorf("first line = %q, want %q", strings.TrimSpace(eventLine), "event: snapshot")
	}

	dataLine, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read data line: %v", err)
	}
	payload := strings.TrimPrefix(strings.TrimSpace(dataLine), "data: ")

	var v dashboard.View
	if err := json.Unmarshal([]byte(payload), &v); err != nil {
		t.Fatalf("decode SSE payload: %v\npayload: %s", err, payload)
	}
	if len(v.Miners) != 2 {
		t.Errorf("got %d miners in the streamed document, want 2", len(v.Miners))
	}
	if strings.Contains(payload, payoutAddress) {
		t.Error("the stream leaks the full payout address")
	}
}

func TestStreamPushesAnUpdateWhenTheStoreChanges(t *testing.T) {
	store := testStore()
	srv := httptest.NewServer(testHandler(store))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	r := bufio.NewReader(resp.Body)
	// Drain the initial snapshot: event line, data line, blank line.
	for i := 0; i < 3; i++ {
		if _, err := r.ReadString('\n'); err != nil {
			t.Fatalf("drain initial event: %v", err)
		}
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		m := miner.Snapshot{Name: "Gamma 602", HashrateTHs: 1.27}
		m.Succeed(now)
		store.SetMiner("Gamma 602", m)
	}()

	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read pushed event: %v", err)
	}
	if strings.TrimSpace(line) != "event: snapshot" {
		t.Errorf("pushed line = %q, want an event line", strings.TrimSpace(line))
	}
}
