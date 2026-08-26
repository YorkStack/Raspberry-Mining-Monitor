package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/report"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

func setup(t *testing.T) *store.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "e.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(time.Now()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := "2026-06-15T12:00:00Z"
	db.SQL().Exec("INSERT INTO miners (internal_id, created_at) VALUES ('M1', ?)", now)
	db.SQL().Exec(`INSERT INTO miner_versions (miner_id, version, valid_from, serial_number, invoice_number, created_at)
		VALUES (1, 1, ?, 'SN1', 'INV-1', ?)`, now, now)
	db.SQL().Exec("INSERT INTO watched_addresses (address, added_at, added_by) VALUES ('bc1qexample', ?, 'york')", now)
	log := audit.New(db.SQL(), time.UTC)
	rs := report.New(db.SQL(), log, filepath.Join(dir, "reports"), "0.8.0")
	rs.Close("2026-03", true, "york", time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC))
	return db
}

func TestStatusJSON(t *testing.T) {
	db := setup(t)
	defer db.Close()
	h := Handler(db.SQL(), "0.8.0")

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var st Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("json: %v (%s)", err, rec.Body.String())
	}
	if st.SoftwareVersion != "0.8.0" {
		t.Errorf("version = %q", st.SoftwareVersion)
	}
	if st.Disclaimer == "" {
		t.Error("disclaimer missing from status")
	}
	if !st.AuditIntact {
		t.Error("audit chain should be intact")
	}
	if len(st.Reports) != 1 || st.Reports[0].Period != "2026-03" {
		t.Errorf("reports = %+v", st.Reports)
	}
	if st.WatchedAddresses != 1 {
		t.Errorf("watched = %d", st.WatchedAddresses)
	}
}

func TestIndexIsSelfContainedHTML(t *testing.T) {
	db := setup(t)
	defer db.Close()
	h := Handler(db.SQL(), "0.8.0")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Tax & Evidence") {
		t.Error("index missing heading")
	}
	if !strings.Contains(body, "does not determine") {
		t.Error("index missing disclaimer")
	}
	// Self-contained: no external asset references.
	for _, bad := range []string{"http://", "https://", "//cdn", "src=\"//"} {
		if strings.Contains(body, bad) {
			t.Errorf("index references external asset %q", bad)
		}
	}
}

func TestReadOnlyRejectsMutations(t *testing.T) {
	db := setup(t)
	defer db.Close()
	h := Handler(db.SQL(), "0.8.0")

	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/api/status", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /api/status = %d, want 405", m, rec.Code)
		}
	}
}
