// Package serve exposes a read-only status view of the evidence archive over
// HTTP: a JSON endpoint and a self-contained "Tax & Evidence" page.
//
// It is deliberately read-only — no endpoint mutates the archive — so the page
// can be shown to an operator or adviser without any risk to the records. It
// lives in the evidence binary; the monitor never links it and so keeps zero
// SQLite/evidence dependencies. The page fetches no external assets.
package serve

import (
	"database/sql"
	"encoding/json"
	"html/template"
	"net/http"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
)

// Disclaimer is shown on every view.
const Disclaimer = "Technical factual documentation only. This service does not " +
	"determine the legal or tax classification of the mining activity."

// ReportRow summarises one report for the status view.
type ReportRow struct {
	ReportID   string `json:"reportId"`
	Period     string `json:"period"`
	Revision   int    `json:"revision"`
	Status     string `json:"status"`
	BundleHash string `json:"evidenceBundleHash"`
	CreatedAt  string `json:"createdAt"`
}

// Status is the read-only status document.
type Status struct {
	SoftwareVersion  string      `json:"softwareVersion"`
	SchemaVersion    int         `json:"schemaVersion"`
	GeneratedAt      string      `json:"generatedAt"`
	AuditIntact      bool        `json:"auditIntact"`
	AuditEntries     int         `json:"auditEntries"`
	WatchedAddresses int         `json:"watchedAddresses"`
	Miners           int         `json:"miners"`
	Reports          []ReportRow `json:"reports"`
	AnnualPackages   int         `json:"annualPackages"`
	Disclaimer       string      `json:"disclaimer"`
}

func schemaVersion(db *sql.DB) int {
	var v sql.NullInt64
	_ = db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&v)
	return int(v.Int64)
}

func collect(db *sql.DB, softwareVersion string, now time.Time) (Status, error) {
	st := Status{
		SoftwareVersion: softwareVersion, SchemaVersion: schemaVersion(db),
		GeneratedAt: now.UTC().Format(time.RFC3339), Disclaimer: Disclaimer,
	}
	log := audit.New(db, time.UTC)
	intact, _, err := log.Verify()
	if err != nil {
		return Status{}, err
	}
	st.AuditIntact = intact
	st.AuditEntries, _ = log.Count()

	db.QueryRow("SELECT COUNT(*) FROM watched_addresses WHERE removed_at IS NULL").Scan(&st.WatchedAddresses)
	db.QueryRow("SELECT COUNT(*) FROM miner_versions WHERE superseded_at IS NULL").Scan(&st.Miners)
	db.QueryRow("SELECT COUNT(*) FROM annual_packages").Scan(&st.AnnualPackages)

	rows, err := db.Query(`SELECT report_id, period, revision, status, evidence_bundle_hash, created_at
		FROM reports ORDER BY period DESC, revision DESC`)
	if err != nil {
		return Status{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var r ReportRow
		if err := rows.Scan(&r.ReportID, &r.Period, &r.Revision, &r.Status, &r.BundleHash, &r.CreatedAt); err != nil {
			return Status{}, err
		}
		st.Reports = append(st.Reports, r)
	}
	return st, rows.Err()
}

// Handler returns the read-only HTTP handler. now defaults to time.Now.
func Handler(db *sql.DB, softwareVersion string) http.Handler {
	mux := http.NewServeMux()

	readOnly := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, "read-only service", http.StatusMethodNotAllowed)
				return
			}
			next(w, r)
		}
	}

	mux.HandleFunc("/api/status", readOnly(func(w http.ResponseWriter, r *http.Request) {
		st, err := collect(db, softwareVersion, time.Now())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.Encode(st)
	}))

	mux.HandleFunc("/healthz", readOnly(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok\n"))
	}))

	mux.HandleFunc("/", readOnly(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		st, err := collect(db, softwareVersion, time.Now())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := indexTmpl.Execute(w, st); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))

	return mux
}

// indexTmpl is a self-contained page: inline CSS, no external assets.
var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Tax & Evidence — Mining Evidence Archive</title>
<style>
:root{color-scheme:light dark}
body{font:15px/1.5 system-ui,-apple-system,Segoe UI,Roboto,sans-serif;margin:0;padding:2rem;max-width:60rem;margin-inline:auto}
h1{font-size:1.5rem;margin:0 0 .25rem}
.sub{color:#888;margin:0 0 1.5rem}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(9rem,1fr));gap:.75rem;margin:1rem 0}
.card{border:1px solid #8884;border-radius:.5rem;padding:.75rem 1rem}
.card .n{font-size:1.6rem;font-weight:600}
.card .l{color:#888;font-size:.8rem}
table{border-collapse:collapse;width:100%;margin:1rem 0;font-size:.9rem}
th,td{text-align:left;padding:.4rem .6rem;border-bottom:1px solid #8883}
th{color:#888;font-weight:600}
code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.85em}
.ok{color:#16a34a;font-weight:600}.bad{color:#dc2626;font-weight:600}
.disc{border-left:3px solid #f59e0b;padding:.5rem .9rem;margin:1.5rem 0;color:#888;font-style:italic}
footer{color:#888;font-size:.8rem;margin-top:2rem}
</style></head><body>
<h1>Tax &amp; Evidence</h1>
<p class="sub">Read-only view of the Mining Evidence Archive · software {{.SoftwareVersion}} · schema v{{.SchemaVersion}} · generated {{.GeneratedAt}}</p>
<div class="grid">
  <div class="card"><div class="n">{{if .AuditIntact}}<span class="ok">intact</span>{{else}}<span class="bad">BROKEN</span>{{end}}</div><div class="l">Audit chain ({{.AuditEntries}} entries)</div></div>
  <div class="card"><div class="n">{{len .Reports}}</div><div class="l">Reports</div></div>
  <div class="card"><div class="n">{{.AnnualPackages}}</div><div class="l">Annual packages</div></div>
  <div class="card"><div class="n">{{.Miners}}</div><div class="l">Active miners</div></div>
  <div class="card"><div class="n">{{.WatchedAddresses}}</div><div class="l">Watched addresses</div></div>
</div>
<h2>Reports</h2>
{{if .Reports}}
<table><thead><tr><th>Report</th><th>Period</th><th>Rev</th><th>Status</th><th>Evidence-bundle hash</th></tr></thead><tbody>
{{range .Reports}}<tr><td><code>{{.ReportID}}</code></td><td>{{.Period}}</td><td>{{.Revision}}</td><td>{{.Status}}</td><td><code>{{slice .BundleHash 0 16}}…</code></td></tr>
{{end}}</tbody></table>
{{else}}<p class="sub">No periods have been closed yet.</p>{{end}}
<p class="disc">{{.Disclaimer}}</p>
<footer>The digitally signed final manifests and evidence packages in the archive constitute the authoritative record. This page is a read-only status view and stores nothing.</footer>
</body></html>`))
