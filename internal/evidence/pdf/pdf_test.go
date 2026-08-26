package pdf

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateProducesPDF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.pdf")
	d := Data{
		ReportID: "MINING-2026-08-ORIGINAL", Period: "2026-08", Revision: 0,
		EvidenceBundleHash: "abcdef0123456789abcdef0123456789", GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		SoftwareVersion: "0.7.0", SchemaVersion: 6, PDFAStatus: "not validated",
		Sections: []Section{{Title: "Active miner inventory", Rows: []KV{{"NERD-01", "NerdOctaxe serial SN1"}}}},
	}
	if err := Generate(d, path); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(b) < 1000 || string(b[:5]) != "%PDF-" {
		t.Errorf("not a PDF: %d bytes, header %q", len(b), string(b[:min(5, len(b))]))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
