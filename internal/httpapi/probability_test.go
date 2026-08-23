package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func decodeProb(t *testing.T, url string, mut func(*http.Request) *http.Request) probabilityResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if mut != nil {
		req = mut(req)
	}
	testHandler(testStore()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, url)
	}
	var r probabilityResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return r
}

func TestProbabilityEndpointUsesCurrentHashrate(t *testing.T) {
	r := decodeProb(t, "/api/v1/probability", nil)
	if !r.UsingCurrent {
		t.Error("usingCurrent = false, want true without ?ths")
	}
	// testStore has one online miner at 12.10 TH/s.
	if r.Ths < 12.09 || r.Ths > 12.11 {
		t.Errorf("ths = %v, want ~12.10", r.Ths)
	}
	if r.Year.Probability <= 0 || r.Year.OddsAgainst <= 0 {
		t.Errorf("year window empty: %+v", r.Year)
	}
}

func TestProbabilityEndpointHypotheticalHashrate(t *testing.T) {
	cur := decodeProb(t, "/api/v1/probability", nil)
	hi := decodeProb(t, "/api/v1/probability?ths=100", nil)
	if hi.UsingCurrent {
		t.Error("usingCurrent = true, want false with ?ths")
	}
	if hi.Ths != 100 {
		t.Errorf("ths = %v, want 100", hi.Ths)
	}
	// More hashrate -> higher probability, better (smaller) odds.
	if !(hi.Year.Probability > cur.Year.Probability) {
		t.Errorf("hypothetical year prob %v not greater than current %v", hi.Year.Probability, cur.Year.Probability)
	}
	if !(hi.Year.OddsAgainst < cur.Year.OddsAgainst) {
		t.Errorf("hypothetical odds %v not better than current %v", hi.Year.OddsAgainst, cur.Year.OddsAgainst)
	}
}

func TestProbabilityEndpointIsPublic(t *testing.T) {
	r := decodeProb(t, "/api/v1/probability", remote)
	if r.Difficulty <= 0 {
		t.Error("expected difficulty for a public client")
	}
}

func TestProbabilityEndpointRejectsPost(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler(testStore()).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/probability", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
