package finalmanifest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/export"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/signing"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

func pkg(t *testing.T) (string, string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(time.Now()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	dir := t.TempDir()
	bundle, _, err := export.WriteEvidencePackage(db.SQL(), dir)
	if err != nil {
		t.Fatalf("WriteEvidencePackage: %v", err)
	}
	return dir, bundle
}

func TestBuildSignAndVerify(t *testing.T) {
	dir, bundle := pkg(t)
	key, _ := signing.Generate()
	meta := Meta{ReportID: "MINING-2026-08-ORIGINAL", Period: "2026-08", EvidenceBundleHash: bundle,
		SoftwareVersion: "0.6.0", SchemaVersion: 6, SigningKeyID: key.KeyID(), GeneratedAt: time.Now()}

	fm, canonical, err := Build(dir, meta)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if fm.EvidenceManifestSHA256 == "" || len(fm.Files) == 0 {
		t.Fatalf("manifest incomplete: %+v", fm)
	}
	if err := Sign(dir, canonical, key); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	pub, _ := signing.ParsePublicBase64(key.PublicBase64())
	ok, bad, err := Verify(dir, pub)
	if err != nil || !ok {
		t.Fatalf("Verify = ok:%v bad:%q err:%v", ok, bad, err)
	}
}

func TestVerifyFailsOnTamper(t *testing.T) {
	dir, bundle := pkg(t)
	key, _ := signing.Generate()
	_, canonical, _ := Build(dir, Meta{ReportID: "R", EvidenceBundleHash: bundle, SigningKeyID: key.KeyID(), GeneratedAt: time.Now()})
	Sign(dir, canonical, key)
	pub, _ := signing.ParsePublicBase64(key.PublicBase64())

	// Tamper an export file.
	p := filepath.Join(dir, "data", "costs.csv")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, append(b, 'x'), 0o640)
	if ok, bad, _ := Verify(dir, pub); ok || bad != "data/costs.csv" {
		t.Errorf("tamper not detected: ok=%v bad=%q", ok, bad)
	}
}
